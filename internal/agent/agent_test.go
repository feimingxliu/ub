package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/invopop/jsonschema"

	"github.com/feimingxliu/ub/internal/approval"
	"github.com/feimingxliu/ub/internal/config"
	contextmgr "github.com/feimingxliu/ub/internal/context"
	"github.com/feimingxliu/ub/internal/execution"
	"github.com/feimingxliu/ub/internal/message"
	"github.com/feimingxliu/ub/internal/permission"
	"github.com/feimingxliu/ub/internal/provider"
	"github.com/feimingxliu/ub/internal/provider/fake"
	"github.com/feimingxliu/ub/internal/reasoning"
	"github.com/feimingxliu/ub/internal/rollout"
	"github.com/feimingxliu/ub/internal/tool"
	"github.com/feimingxliu/ub/internal/tool/fs"
)

type scriptProvider struct {
	scripts  []fake.Script
	requests []provider.Request
	caps     provider.Caps
}

func (p *scriptProvider) Name() string { return "script" }
func (p *scriptProvider) Caps() provider.Caps {
	caps := p.caps
	caps.SupportsTools = true
	caps.SupportsStreaming = true
	return caps
}

func (p *scriptProvider) Chat(_ context.Context, req provider.Request) (provider.Stream, error) {
	p.requests = append(p.requests, provider.Request{
		Model:     req.Model,
		Messages:  cloneMessages(req.Messages),
		Tools:     append([]provider.ToolDefinition(nil), req.Tools...),
		Reasoning: cloneReasoning(req.Reasoning),
	})
	idx := len(p.requests) - 1
	if idx >= len(p.scripts) {
		return nil, errors.New("unexpected extra chat call")
	}
	return fake.New(p.scripts[idx]).Chat(context.Background(), req)
}

func TestAgentPassesReasoningConfig(t *testing.T) {
	reg := tool.New()
	p := &scriptProvider{scripts: []fake.Script{{fake.TextDelta("ok"), fake.Done()}}}
	a, err := New(Options{
		Provider:  p,
		Tools:     reg,
		Model:     "reasoner",
		Mode:      execution.ModeWork,
		Reasoning: &reasoning.Config{Effort: reasoning.EffortHigh},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := a.Run(context.Background(), Request{Prompt: "hi", Turn: 1}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(p.requests) != 1 || p.requests[0].Reasoning == nil || p.requests[0].Reasoning.Effort != reasoning.EffortHigh {
		t.Fatalf("request reasoning = %#v", p.requests)
	}
}

func TestAgentInjectsRuntimeContextWithoutPersistingIt(t *testing.T) {
	reg := tool.New()
	p := &scriptProvider{scripts: []fake.Script{{fake.TextDelta("ok"), fake.Done()}}}
	a, err := New(Options{
		Provider: p,
		Tools:    reg,
		Model:    "fake/model",
		Mode:     execution.ModeWork,
		Runtime: RuntimeContext{
			Workspace: "/tmp/workspace",
			Shell:     "/bin/sh",
			OS:        "linux",
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	res, err := a.Run(context.Background(), Request{Prompt: "hi", Turn: 1})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(p.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(p.requests))
	}
	got := p.requests[0].Messages
	if len(got) == 0 || got[0].Role != message.RoleSystem {
		t.Fatalf("first request message = %#v, want runtime system context", got)
	}
	runtimeText := got[0].Text()
	for _, want := range []string{
		"<cwd>/tmp/workspace</cwd>",
		"<shell>/bin/sh</shell>",
		"<os>linux</os>",
		"Do not invent alternate project paths such as /home/user",
		"use the cwd parameter",
	} {
		if !strings.Contains(runtimeText, want) {
			t.Fatalf("runtime context missing %q:\n%s", want, runtimeText)
		}
	}
	if containsText(res.Messages, "<environment_context>") {
		t.Fatalf("runtime context leaked into result history: %#v", res.Messages)
	}
}

func TestAgentRunsReadToolAndReturnsFinalAnswer(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	reg := tool.New()
	if err := fs.Register(reg, root); err != nil {
		t.Fatalf("register fs: %v", err)
	}
	perm := newPermissionManager(t, nil)
	p := &scriptProvider{scripts: []fake.Script{
		{fake.ToolCall("read", map[string]any{"path": "main.go"}), fake.Done()},
		{fake.TextDelta("main function found"), fake.Done()},
	}}
	a := newTestAgent(t, p, reg, perm, execution.ModeWork)

	res, err := a.Run(context.Background(), Request{Prompt: "read main.go", Turn: 1})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Text != "main function found" {
		t.Fatalf("text = %q", res.Text)
	}
	if len(p.requests) != 2 {
		t.Fatalf("chat calls = %d, want 2", len(p.requests))
	}
	if len(p.requests[0].Tools) == 0 || p.requests[0].Tools[0].Name == "" || len(p.requests[0].Tools[0].Schema) == 0 {
		t.Fatalf("first request tools = %#v", p.requests[0].Tools)
	}
	last := lastMessage(t, p.requests[1].Messages)
	if last.Role != message.RoleTool || len(last.Content) != 1 {
		t.Fatalf("second request last message = %#v", last)
	}
	block := last.Content[0]
	if block.Type != message.BlockToolResult || block.IsError || !strings.Contains(block.Output, "func main") {
		t.Fatalf("tool result block = %#v", block)
	}
}

func TestAgentLimitsToolResultAndSpillsFullOutput(t *testing.T) {
	root := t.TempDir()
	var content strings.Builder
	for i := 1; i <= 450; i++ {
		fmt.Fprintf(&content, "line-%03d\n", i)
	}
	if err := os.WriteFile(filepath.Join(root, "big.txt"), []byte(content.String()), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	reg := tool.New()
	if err := fs.Register(reg, root); err != nil {
		t.Fatalf("register fs: %v", err)
	}
	perm := newPermissionManager(t, nil)
	p := &scriptProvider{scripts: []fake.Script{
		{fake.ToolCall("read", map[string]any{"path": "big.txt"}), fake.Done()},
		{fake.TextDelta("done"), fake.Done()},
	}}
	writer := &recordingRollout{}
	stateRoot := t.TempDir()
	a, err := New(Options{
		Provider:        p,
		Tools:           reg,
		Permission:      perm,
		Rollout:         writer,
		Model:           "fake/model",
		Mode:            execution.ModeWork,
		ToolOutputState: stateRoot,
		Context: config.ContextConfig{
			ToolResults: config.ContextToolResultConfig{
				InlineMaxBytes: 2048,
				InlineMaxLines: 20,
			},
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := a.Run(context.Background(), Request{SessionID: "sess_spill", Prompt: "read big.txt", Turn: 1}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	last := lastMessage(t, p.requests[1].Messages)
	block := last.Content[0]
	if block.Type != message.BlockToolResult || !strings.Contains(block.Output, "full_output_path=") {
		t.Fatalf("tool result output missing spillover footer:\n%s", block.Output)
	}
	if strings.Contains(block.Output, "line-450") {
		t.Fatalf("model-visible output kept tail beyond cap:\n%s", block.Output)
	}
	var payload rollout.ToolResultPayload
	for _, event := range writer.events {
		if event.Type == rollout.TypeToolResult {
			if err := json.Unmarshal(event.Payload, &payload); err != nil {
				t.Fatalf("decode payload: %v", err)
			}
			break
		}
	}
	if !payload.Truncated || payload.FullOutputPath == "" || payload.OriginalBytes == 0 {
		t.Fatalf("payload metadata = %#v", payload)
	}
	raw, err := os.ReadFile(payload.FullOutputPath)
	if err != nil {
		t.Fatalf("read spillover: %v", err)
	}
	if !strings.Contains(string(raw), "line-450") {
		t.Fatalf("spillover missing tail: %q", tailString(string(raw), 80))
	}
}

func TestAgentFinalizesWithoutToolsAfterMaxTurns(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	reg := tool.New()
	if err := fs.Register(reg, root); err != nil {
		t.Fatalf("register fs: %v", err)
	}
	perm := newPermissionManager(t, nil)
	writer := &recordingRollout{}
	p := &scriptProvider{scripts: []fake.Script{
		{fake.ToolCall("read", map[string]any{"path": "main.go"}), fake.Done()},
		{fake.TextDelta("final from gathered file"), fake.Done()},
	}}
	a, err := New(Options{
		Provider:   p,
		Tools:      reg,
		Permission: perm,
		Rollout:    writer,
		Model:      "fake/model",
		Mode:       execution.ModeWork,
		MaxTurns:   1,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	res, err := a.Run(context.Background(), Request{SessionID: "sess_limit", Prompt: "inspect", Turn: 1})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Text != "final from gathered file" {
		t.Fatalf("text = %q, want final", res.Text)
	}
	if len(p.requests) != 2 {
		t.Fatalf("chat calls = %d, want 2", len(p.requests))
	}
	if len(p.requests[0].Tools) == 0 {
		t.Fatalf("first request missing tools")
	}
	if len(p.requests[1].Tools) != 0 {
		t.Fatalf("final request tools = %#v, want none", p.requests[1].Tools)
	}
	if !containsText(p.requests[1].Messages, "Do not call tools") {
		t.Fatalf("final request missing no-tool instruction: %#v", p.requests[1].Messages)
	}
	if containsText(res.Messages, "Do not call tools") {
		t.Fatalf("result history leaked internal no-tool instruction: %#v", res.Messages)
	}
	if !hasEventType(writer.events, rollout.TypeAssistantMessage) {
		t.Fatalf("events missing final assistant message: %#v", writer.events)
	}
}

func TestAgentEmitsRuntimeEvents(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	reg := tool.New()
	if err := fs.Register(reg, root); err != nil {
		t.Fatalf("register fs: %v", err)
	}
	perm := newPermissionManager(t, nil)
	p := &scriptProvider{scripts: []fake.Script{
		{fake.TextDelta("he"), fake.TextDelta("llo"), fake.ToolCall("read", map[string]any{"path": "main.go"}), fake.Done()},
		{fake.TextDelta("done"), fake.Done()},
	}}
	var events []Event
	a, err := New(Options{
		Provider:   p,
		Tools:      reg,
		Permission: perm,
		Model:      "fake/model",
		Mode:       execution.ModeWork,
		Events: func(event Event) {
			events = append(events, event)
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := a.Run(context.Background(), Request{Prompt: "read main.go", Turn: 1}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	gotTypes := make([]EventType, 0, len(events))
	for _, event := range events {
		gotTypes = append(gotTypes, event.Type)
	}
	wantTypes := []EventType{
		EventContext,
		EventDeltaText,
		EventDeltaText,
		EventActivity,
		EventActivity,
		EventActivity,
		EventContext,
		EventDeltaText,
		EventDone,
	}
	if !reflect.DeepEqual(gotTypes, wantTypes) {
		t.Fatalf("event types = %#v, want %#v", gotTypes, wantTypes)
	}
	if events[0].ContextUsedTokens <= 0 {
		t.Fatalf("context event = %#v, want used tokens", events[0])
	}
	if events[1].Text != "he" || events[2].Text != "llo" || events[7].Text != "done" {
		t.Fatalf("delta events = %#v", events)
	}
	if events[3].ActivityKind != ActivityTool || events[3].ToolName != "read" || events[3].Status != "queued" || !strings.Contains(events[3].Summary, "path=main.go") {
		t.Fatalf("queued event = %#v", events[3])
	}
	if events[4].ActivityKind != ActivityTool || events[4].Status != "running" {
		t.Fatalf("running event = %#v", events[4])
	}
	if events[5].ActivityKind != ActivityTool || events[5].Status != "done" || events[5].IsError {
		t.Fatalf("done event = %#v", events[5])
	}
}

func TestAgentReasoningActivityDoesNotEnterAssistantText(t *testing.T) {
	reg := tool.New()
	perm := newPermissionManager(t, nil)
	p := &scriptProvider{scripts: []fake.Script{
		{fake.ReasoningDelta("checking context"), fake.TextDelta("answer"), fake.Done()},
	}}
	var events []Event
	a, err := New(Options{
		Provider:   p,
		Tools:      reg,
		Permission: perm,
		Model:      "fake/model",
		Mode:       execution.ModeWork,
		Events: func(event Event) {
			events = append(events, event)
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	res, err := a.Run(context.Background(), Request{Prompt: "think", Turn: 1})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Text != "answer" {
		t.Fatalf("text = %q, want answer", res.Text)
	}
	if got := res.Messages[len(res.Messages)-1].Text(); got != "answer" {
		t.Fatalf("assistant text = %q, want answer", got)
	}
	if !hasActivity(events, ActivityThinking, "checking context") {
		t.Fatalf("events = %#v, want thinking activity", events)
	}
	if !hasActivityContent(events, ActivityThinking, "checking context") {
		t.Fatalf("events = %#v, want thinking content", events)
	}
}

func TestAgentPlanModeRejectsEditWithoutModifyingFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	if err := os.WriteFile(path, []byte("package main\nfunc main() { println(\"old\") }\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	reg := tool.New()
	if err := fs.Register(reg, root); err != nil {
		t.Fatalf("register fs: %v", err)
	}
	perm := newPermissionManager(t, nil)
	p := &scriptProvider{scripts: []fake.Script{
		{fake.ToolCall("edit", map[string]any{"path": "main.go", "old": "old", "new": "new"}), fake.Done()},
		{fake.TextDelta("edit denied"), fake.Done()},
	}}
	a := newTestAgent(t, p, reg, perm, execution.ModePlan)

	if _, err := a.Run(context.Background(), Request{Prompt: "edit main.go", Turn: 1}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if strings.Contains(string(raw), "new") || !strings.Contains(string(raw), "old") {
		t.Fatalf("file changed unexpectedly: %q", raw)
	}
	last := lastMessage(t, p.requests[1].Messages)
	block := last.Content[0]
	if !block.IsError || !strings.Contains(block.Output, "plan mode") {
		t.Fatalf("tool result block = %#v, want plan denial", block)
	}
}

func TestAgentPreviewPassesThroughPermissionAndExecuteAfterAllow(t *testing.T) {
	reg := tool.New()
	pt := &previewExecTool{}
	if err := reg.Register(pt); err != nil {
		t.Fatalf("register tool: %v", err)
	}
	asker := &recordingAsker{decision: permission.DecisionAllow}
	perm := newPermissionManager(t, asker)
	p := &scriptProvider{scripts: []fake.Script{
		{fake.ToolCall("preview_exec", map[string]any{"value": "x"}), fake.Done()},
		{fake.TextDelta("done"), fake.Done()},
	}}
	a := newTestAgent(t, p, reg, perm, execution.ModeWork)

	if _, err := a.Run(context.Background(), Request{Prompt: "call preview tool", Turn: 1}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if pt.previewCalls != 1 || pt.executeCalls != 1 {
		t.Fatalf("preview calls=%d execute calls=%d, want 1/1", pt.previewCalls, pt.executeCalls)
	}
	if asker.calls != 1 {
		t.Fatalf("asker calls = %d, want 1", asker.calls)
	}
	if asker.requests[0].Preview == nil || asker.requests[0].Preview.Summary != "preview x" {
		t.Fatalf("asker preview = %#v", asker.requests[0].Preview)
	}
}

func TestAgentEmitsPermissionDecisionEvent(t *testing.T) {
	reg := tool.New()
	if err := reg.Register(&previewExecTool{}); err != nil {
		t.Fatalf("register tool: %v", err)
	}
	asker := &recordingAsker{decision: permission.DecisionAllow}
	perm := newPermissionManager(t, asker)
	p := &scriptProvider{scripts: []fake.Script{
		{fake.ToolCall("preview_exec", map[string]any{"value": "x"}), fake.Done()},
		{fake.TextDelta("done"), fake.Done()},
	}}
	var events []Event
	a, err := New(Options{
		Provider:   p,
		Tools:      reg,
		Permission: perm,
		Model:      "fake/model",
		Mode:       execution.ModeWork,
		Events: func(event Event) {
			events = append(events, event)
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := a.Run(context.Background(), Request{Prompt: "call preview tool", Turn: 1}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	var got *Event
	for i := range events {
		if events[i].Type == EventActivity && events[i].ActivityKind == ActivityPermission {
			got = &events[i]
			break
		}
	}
	if got == nil {
		t.Fatalf("events = %#v, want permission event", events)
	}
	if got.ToolName != "preview_exec" || got.Source != string(permission.SourceHuman) || got.Decision != string(permission.DecisionAllow) || !got.Allowed {
		t.Fatalf("permission event = %#v", *got)
	}
}

func TestAgentEmitsApprovalAgentDecisionOnce(t *testing.T) {
	reg := tool.New()
	if err := reg.Register(&previewExecTool{}); err != nil {
		t.Fatalf("register tool: %v", err)
	}
	asker := &recordingAsker{decision: permission.DecisionDeny}
	perm, err := permission.NewManager(permission.Options{
		Asker:           asker,
		ApprovalAgent:   approvalAgent{result: approval.Result{Decision: approval.DecisionAllow, Reason: "safe read-only command"}},
		GlobalRulesPath: filepath.Join(t.TempDir(), "permissions.yaml"),
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	p := &scriptProvider{scripts: []fake.Script{
		{fake.ToolCall("preview_exec", map[string]any{"value": "x"}), fake.Done()},
		{fake.TextDelta("done"), fake.Done()},
	}}
	var events []Event
	a, err := New(Options{
		Provider:   p,
		Tools:      reg,
		Permission: perm,
		Model:      "fake/model",
		Mode:       execution.ModeAuto,
		Events: func(event Event) {
			events = append(events, event)
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := a.Run(context.Background(), Request{Prompt: "call preview tool", Turn: 1}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	var permissionEvents []Event
	for _, event := range events {
		if event.Type == EventActivity && event.ActivityKind == ActivityPermission {
			permissionEvents = append(permissionEvents, event)
		}
	}
	if len(permissionEvents) != 1 {
		t.Fatalf("permission events = %#v, want exactly one", permissionEvents)
	}
	got := permissionEvents[0]
	if got.Source != string(permission.SourceApprovalAgent) || got.Decision != string(approval.DecisionAllow) || !got.Allowed || got.Reason != "safe read-only command" {
		t.Fatalf("approval permission event = %#v", got)
	}
	if asker.calls != 0 {
		t.Fatalf("asker calls = %d, want 0", asker.calls)
	}
}

func TestAgentEmitsApprovalAgentFallbackAndHumanDecision(t *testing.T) {
	reg := tool.New()
	if err := reg.Register(&previewExecTool{}); err != nil {
		t.Fatalf("register tool: %v", err)
	}
	asker := &recordingAsker{decision: permission.DecisionAllow}
	perm, err := permission.NewManager(permission.Options{
		Asker:           asker,
		ApprovalAgent:   approvalAgent{result: approval.Result{Decision: approval.DecisionUnsure, Reason: "needs user context"}},
		GlobalRulesPath: filepath.Join(t.TempDir(), "permissions.yaml"),
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	p := &scriptProvider{scripts: []fake.Script{
		{fake.ToolCall("preview_exec", map[string]any{"value": "x"}), fake.Done()},
		{fake.TextDelta("done"), fake.Done()},
	}}
	var events []Event
	a, err := New(Options{
		Provider:   p,
		Tools:      reg,
		Permission: perm,
		Model:      "fake/model",
		Mode:       execution.ModeAuto,
		Events: func(event Event) {
			events = append(events, event)
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := a.Run(context.Background(), Request{Prompt: "call preview tool", Turn: 1}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	var permissionEvents []Event
	for _, event := range events {
		if event.Type == EventActivity && event.ActivityKind == ActivityPermission {
			permissionEvents = append(permissionEvents, event)
		}
	}
	if len(permissionEvents) != 2 {
		t.Fatalf("permission events = %#v, want approval + human", permissionEvents)
	}
	if permissionEvents[0].Source != string(permission.SourceApprovalAgent) || permissionEvents[0].Decision != string(approval.DecisionUnsure) || permissionEvents[0].Allowed {
		t.Fatalf("approval event = %#v", permissionEvents[0])
	}
	if permissionEvents[1].Source != string(permission.SourceHuman) || permissionEvents[1].Decision != string(permission.DecisionAllow) || !permissionEvents[1].Allowed {
		t.Fatalf("human event = %#v", permissionEvents[1])
	}
}

func TestAgentReadsModeAtToolPermissionTime(t *testing.T) {
	reg := tool.New()
	if err := reg.Register(&previewExecTool{}); err != nil {
		t.Fatalf("register tool: %v", err)
	}
	asker := &recordingAsker{decision: permission.DecisionAllow}
	perm := newPermissionManager(t, asker)
	p := &scriptProvider{scripts: []fake.Script{
		{fake.ToolCall("preview_exec", map[string]any{"value": "x"}), fake.Done()},
		{fake.TextDelta("done"), fake.Done()},
	}}
	mode := execution.ModePlan
	a, err := New(Options{
		Provider:   p,
		Tools:      reg,
		Permission: perm,
		Model:      "fake/model",
		Mode:       execution.ModeWork,
		ModeFunc: func() execution.Mode {
			return mode
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := a.Run(context.Background(), Request{Prompt: "call preview tool", Turn: 1}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if asker.calls != 1 {
		t.Fatalf("asker calls = %d, want 1", asker.calls)
	}
	if got := asker.requests[0].Mode; got != execution.ModePlan {
		t.Fatalf("permission mode = %q, want plan", got)
	}
}

func TestToolActivitySummaryRedactsSecretsAndTruncates(t *testing.T) {
	summary := summarizeToolInput("bash", json.RawMessage(`{"command":"curl -H 'Authorization: Bearer secret-token' https://example.test\nsecond line","cwd":"/tmp"}`))
	if strings.Contains(summary, "secret-token") || strings.Contains(summary, "Authorization") {
		t.Fatalf("summary leaked secret: %q", summary)
	}
	if !strings.Contains(summary, "cmd=[redacted]") || !strings.Contains(summary, "cwd=/tmp") {
		t.Fatalf("summary = %q, want redacted command and cwd", summary)
	}

	long := summarizeToolResult(tool.Result{Content: strings.Repeat("x", maxActivitySummaryRunes+20)})
	if len([]rune(long)) > maxActivitySummaryRunes {
		t.Fatalf("summary len = %d, want <= %d", len([]rune(long)), maxActivitySummaryRunes)
	}
	if !strings.HasSuffix(long, "...") {
		t.Fatalf("summary = %q, want ellipsis", long)
	}
}

func TestAgentWritesRolloutEvents(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	reg := tool.New()
	if err := fs.Register(reg, root); err != nil {
		t.Fatalf("register fs: %v", err)
	}
	perm := newPermissionManager(t, nil)
	writer := &recordingRollout{}
	p := &scriptProvider{scripts: []fake.Script{
		{fake.ToolCall("read", map[string]any{"path": "main.go"}), fake.Usage(1, 2), fake.Done()},
		{fake.TextDelta("done"), fake.Done()},
	}}
	a, err := New(Options{
		Provider:   p,
		Tools:      reg,
		Permission: perm,
		Rollout:    writer,
		Model:      "fake/model",
		Mode:       execution.ModeWork,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := a.Run(context.Background(), Request{SessionID: "sess_1", Prompt: "read", Turn: 3}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	gotTypes := make([]rollout.Type, 0, len(writer.events))
	for _, event := range writer.events {
		gotTypes = append(gotTypes, event.Type)
	}
	want := []rollout.Type{
		rollout.TypeUserMessage,
		rollout.TypeUsage,
		rollout.TypeAssistantMessage,
		rollout.TypeToolResult,
		rollout.TypeAssistantMessage,
	}
	if len(gotTypes) != len(want) {
		t.Fatalf("event types = %#v, want %#v", gotTypes, want)
	}
	for i := range want {
		if gotTypes[i] != want[i] {
			t.Fatalf("event types = %#v, want %#v", gotTypes, want)
		}
	}
}

func TestAgentSummarizesLongHistoryBeforeProviderRequest(t *testing.T) {
	reg := tool.New()
	perm := newPermissionManager(t, nil)
	main := &scriptProvider{
		caps: provider.Caps{MaxContextTokens: 20},
		scripts: []fake.Script{
			{fake.TextDelta("final"), fake.Done()},
		},
	}
	summary := &scriptProvider{scripts: []fake.Script{
		{fake.TextDelta("summary of early work"), fake.Done()},
	}}
	writer := &recordingRollout{}
	a, err := New(Options{
		Provider:        main,
		SummaryProvider: summary,
		SummaryModel:    "small",
		Tools:           reg,
		Permission:      perm,
		Rollout:         writer,
		Model:           "fake/model",
		Mode:            execution.ModeWork,
		Context: config.ContextConfig{
			TriggerRatio:    0.01,
			KeepRecentTurns: 3,
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	history := turnHistory(5)
	res, err := a.Run(context.Background(), Request{SessionID: "sess_sum", Prompt: "current prompt", Turn: 7, History: history})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Text != "final" {
		t.Fatalf("text = %q, want final", res.Text)
	}
	if len(summary.requests) != 1 {
		t.Fatalf("summary requests = %d, want 1", len(summary.requests))
	}
	if summary.requests[0].Model != "small" {
		t.Fatalf("summary model = %q, want small", summary.requests[0].Model)
	}
	if len(main.requests) != 1 {
		t.Fatalf("main requests = %d, want 1", len(main.requests))
	}
	got := main.requests[0].Messages
	if len(got) == 0 || got[0].Role != message.RoleSystem || !strings.Contains(got[0].Text(), "summary of early work") {
		t.Fatalf("main request first message = %#v", got)
	}
	if containsText(got, "user 1") || containsText(got, "user 2") || containsText(got, "user 3") {
		t.Fatalf("main request kept summarized messages: %#v", got)
	}
	for _, want := range []string{"user 4", "user 5", "current prompt"} {
		if !containsText(got, want) {
			t.Fatalf("main request missing %q: %#v", want, got)
		}
	}
	if !hasEventType(writer.events, rollout.TypeSummary) {
		t.Fatalf("events missing summary: %#v", writer.events)
	}
}

func TestAgentManualCompactSummarizesHistory(t *testing.T) {
	reg := tool.New()
	perm := newPermissionManager(t, nil)
	main := &scriptProvider{caps: provider.Caps{MaxContextTokens: 1000}}
	summary := &scriptProvider{scripts: []fake.Script{
		{fake.TextDelta("manual summary"), fake.Done()},
	}}
	writer := &recordingRollout{}
	var events []Event
	a, err := New(Options{
		Provider:        main,
		SummaryProvider: summary,
		SummaryModel:    "small",
		Tools:           reg,
		Permission:      perm,
		Rollout:         writer,
		Model:           "fake/model",
		Mode:            execution.ModeWork,
		Context:         config.ContextConfig{TriggerRatio: 0.99, KeepRecentTurns: 3},
		Events: func(event Event) {
			events = append(events, event)
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	result, err := a.Compact(context.Background(), CompactRequest{SessionID: "sess_manual", Turn: 9, History: turnHistory(5)})
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if result.Noop {
		t.Fatalf("Compact returned noop")
	}
	if len(summary.requests) != 1 || summary.requests[0].Model != "small" {
		t.Fatalf("summary requests = %#v", summary.requests)
	}
	if len(result.Messages) == 0 || result.Messages[0].Role != message.RoleSystem || !strings.Contains(result.Messages[0].Text(), "manual summary") {
		t.Fatalf("result first message = %#v", result.Messages)
	}
	if containsText(result.Messages, "user 1") || containsText(result.Messages, "user 2") {
		t.Fatalf("result kept compacted messages: %#v", result.Messages)
	}
	for _, want := range []string{"user 3", "user 4", "user 5"} {
		if !containsText(result.Messages, want) {
			t.Fatalf("result missing %q: %#v", want, result.Messages)
		}
	}
	if !hasEventType(writer.events, rollout.TypeSummary) {
		t.Fatalf("events missing summary: %#v", writer.events)
	}
	contextEvent, ok := firstContextEvent(events)
	if !ok || contextEvent.ContextMaxTokens != 1000 || contextEvent.ContextUsedTokens <= 0 || contextEvent.ContextRatio <= 0 || !contextEvent.ContextReset {
		t.Fatalf("context event = %#v, ok=%v", contextEvent, ok)
	}
	if events[len(events)-1].Type != EventDone {
		t.Fatalf("last event = %#v, want done", events[len(events)-1])
	}
}

func TestAgentManualCompactNoopsWithoutPrefix(t *testing.T) {
	reg := tool.New()
	perm := newPermissionManager(t, nil)
	main := &scriptProvider{}
	summary := &scriptProvider{scripts: []fake.Script{
		{fake.Error("summary should not run")},
	}}
	writer := &recordingRollout{}
	var events []Event
	a, err := New(Options{
		Provider:        main,
		SummaryProvider: summary,
		Tools:           reg,
		Permission:      perm,
		Rollout:         writer,
		Model:           "fake/model",
		Mode:            execution.ModeWork,
		Context:         config.ContextConfig{KeepRecentTurns: 3},
		Events: func(event Event) {
			events = append(events, event)
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	history := turnHistory(2)
	result, err := a.Compact(context.Background(), CompactRequest{SessionID: "sess_noop", Turn: 2, History: history})
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if !result.Noop || !strings.Contains(result.Reason, "nothing to compact") {
		t.Fatalf("result = %#v, want noop reason", result)
	}
	if len(summary.requests) != 0 {
		t.Fatalf("summary requests = %d, want 0", len(summary.requests))
	}
	if !reflect.DeepEqual(result.Messages, history) {
		t.Fatalf("messages changed: %#v", result.Messages)
	}
	if hasEventType(writer.events, rollout.TypeSummary) {
		t.Fatalf("unexpected summary event: %#v", writer.events)
	}
	if !hasActivity(events, ActivityNotice, "nothing to compact") {
		t.Fatalf("events missing noop notice: %#v", events)
	}
}

func TestSplitSummaryWindowKeepsRecentTurnsWithinBudget(t *testing.T) {
	history := []message.Message{
		message.Text(message.RoleUser, "user 1"),
		message.Text(message.RoleAssistant, "assistant 1"),
		message.Text(message.RoleUser, "user 2 "+strings.Repeat("x", 70000)),
		message.Text(message.RoleAssistant, "assistant 2"),
		message.Text(message.RoleUser, "user 3"),
		message.Text(message.RoleAssistant, "assistant 3"),
		message.Text(message.RoleUser, "current"),
	}
	prefix, suffix, ok := splitSummaryWindow(history, summaryWindowOptions{
		KeepRecentTurns: 3,
		MaxContext:      100000,
		Model:           "local/unknown",
	})
	if !ok {
		t.Fatal("splitSummaryWindow returned !ok")
	}
	if containsText(suffix, "user 2") {
		t.Fatalf("suffix kept oversized older turn: %#v", suffix)
	}
	for _, want := range []string{"user 3", "current"} {
		if !containsText(suffix, want) {
			t.Fatalf("suffix missing %q: %#v", want, suffix)
		}
	}
	if !containsText(prefix, "user 2") {
		t.Fatalf("prefix should contain compacted oversized turn: %#v", prefix)
	}
}

func TestAgentDoesNotSummarizeBelowThreshold(t *testing.T) {
	reg := tool.New()
	perm := newPermissionManager(t, nil)
	main := &scriptProvider{
		caps: provider.Caps{MaxContextTokens: 1_000_000},
		scripts: []fake.Script{
			{fake.TextDelta("ok"), fake.Done()},
		},
	}
	summary := &scriptProvider{scripts: []fake.Script{
		{fake.Error("summary should not run")},
	}}
	a, err := New(Options{
		Provider:        main,
		SummaryProvider: summary,
		Tools:           reg,
		Permission:      perm,
		Model:           "fake/model",
		Mode:            execution.ModeWork,
		Context:         config.ContextConfig{TriggerRatio: 0.8, KeepRecentTurns: 3},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := a.Run(context.Background(), Request{Prompt: "short", Turn: 1, History: turnHistory(5)}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(summary.requests) != 0 {
		t.Fatalf("summary requests = %d, want 0", len(summary.requests))
	}
	if got := main.requests[0].Messages; containsRole(got, message.RoleSystem) {
		t.Fatalf("main request unexpectedly summarized: %#v", got)
	}
}

func TestAgentUsesModelContextOverrideForSummaryThreshold(t *testing.T) {
	reg := tool.New()
	perm := newPermissionManager(t, nil)
	main := &scriptProvider{
		caps: provider.Caps{MaxContextTokens: 20},
		scripts: []fake.Script{
			{fake.TextDelta("ok"), fake.Done()},
		},
	}
	summary := &scriptProvider{scripts: []fake.Script{
		{fake.Error("summary should not run")},
	}}
	var events []Event
	a, err := New(Options{
		Provider:         main,
		SummaryProvider:  summary,
		Tools:            reg,
		Permission:       perm,
		Model:            "fake/model",
		Mode:             execution.ModeWork,
		MaxContextTokens: 1_000_000,
		Context:          config.ContextConfig{TriggerRatio: 0.99, KeepRecentTurns: 3},
		Events: func(event Event) {
			events = append(events, event)
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := a.Run(context.Background(), Request{Prompt: "current", Turn: 1, History: turnHistory(5)}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(summary.requests) != 0 {
		t.Fatalf("summary requests = %d, want 0", len(summary.requests))
	}
	contextEvent, ok := firstContextEvent(events)
	if !ok || contextEvent.ContextMaxTokens != 1_000_000 {
		t.Fatalf("context event = %#v ok=%v, want model override max", contextEvent, ok)
	}
	if got := main.requests[0].Messages; containsRole(got, message.RoleSystem) {
		t.Fatalf("main request unexpectedly summarized: %#v", got)
	}
}

func TestAgentSummaryFailureRecordsRolloutError(t *testing.T) {
	reg := tool.New()
	perm := newPermissionManager(t, nil)
	main := &scriptProvider{caps: provider.Caps{MaxContextTokens: 20}}
	summary := &scriptProvider{scripts: []fake.Script{
		{fake.Error("summary failed")},
	}}
	writer := &recordingRollout{}
	a, err := New(Options{
		Provider:        main,
		SummaryProvider: summary,
		Tools:           reg,
		Permission:      perm,
		Rollout:         writer,
		Model:           "fake/model",
		Mode:            execution.ModeWork,
		Context:         config.ContextConfig{TriggerRatio: 0.01, KeepRecentTurns: 3},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = a.Run(context.Background(), Request{SessionID: "sess_fail", Prompt: "current", Turn: 2, History: turnHistory(5)})
	if err == nil || !strings.Contains(err.Error(), "summary provider") {
		t.Fatalf("Run error = %v, want summary provider error", err)
	}
	if len(main.requests) != 0 {
		t.Fatalf("main provider was called after summary failure")
	}
	if !hasEventType(writer.events, rollout.TypeError) {
		t.Fatalf("events missing error: %#v", writer.events)
	}
}

func TestAgentUsageCalibratesTokenEstimate(t *testing.T) {
	reg := tool.New()
	perm := newPermissionManager(t, nil)
	model := "usage-calibration-test"
	msgs := []message.Message{message.Text(message.RoleUser, "calibrate")}
	before := contextmgr.Estimate(msgs, model)
	main := &scriptProvider{scripts: []fake.Script{
		{fake.Usage(before*2, 1), fake.TextDelta("ok"), fake.Done()},
	}}
	a, err := New(Options{
		Provider:   main,
		Tools:      reg,
		Permission: perm,
		Model:      model,
		Mode:       execution.ModeWork,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := a.Run(context.Background(), Request{Prompt: "calibrate", Turn: 1}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	after := contextmgr.Estimate(msgs, model)
	if after <= before {
		t.Fatalf("estimate after usage = %d, before = %d, want larger", after, before)
	}
}

type recordingRollout struct {
	events []rollout.Event
}

func (w *recordingRollout) Append(_ context.Context, event rollout.Event) error {
	w.events = append(w.events, event)
	return nil
}

func (w *recordingRollout) Close() error { return nil }

func turnHistory(turns int) []message.Message {
	out := make([]message.Message, 0, turns*2)
	for i := 1; i <= turns; i++ {
		out = append(out,
			message.Text(message.RoleUser, fmt.Sprintf("user %d", i)),
			message.Text(message.RoleAssistant, fmt.Sprintf("assistant %d", i)),
		)
	}
	return out
}

func containsText(messages []message.Message, text string) bool {
	for _, msg := range messages {
		if strings.Contains(msg.Text(), text) {
			return true
		}
	}
	return false
}

func containsRole(messages []message.Message, role message.Role) bool {
	for _, msg := range messages {
		if msg.Role == role {
			return true
		}
	}
	return false
}

func hasEventType(events []rollout.Event, typ rollout.Type) bool {
	for _, event := range events {
		if event.Type == typ {
			return true
		}
	}
	return false
}

func hasActivity(events []Event, kind ActivityKind, text string) bool {
	for _, event := range events {
		if event.Type == EventActivity && event.ActivityKind == kind && strings.Contains(event.Summary, text) {
			return true
		}
	}
	return false
}

func hasActivityContent(events []Event, kind ActivityKind, text string) bool {
	for _, event := range events {
		if event.Type == EventActivity && event.ActivityKind == kind && strings.Contains(event.Content, text) {
			return true
		}
	}
	return false
}

func firstContextEvent(events []Event) (Event, bool) {
	for _, event := range events {
		if event.Type == EventContext {
			return event, true
		}
	}
	return Event{}, false
}

func newTestAgent(t *testing.T, p provider.Provider, reg *tool.Registry, perm *permission.Manager, mode execution.Mode) *Agent {
	t.Helper()
	a, err := New(Options{
		Provider:   p,
		Tools:      reg,
		Permission: perm,
		Model:      "fake/model",
		Mode:       mode,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return a
}

func newPermissionManager(t *testing.T, asker permission.Asker) *permission.Manager {
	t.Helper()
	perm, err := permission.NewManager(permission.Options{
		Asker:           asker,
		GlobalRulesPath: filepath.Join(t.TempDir(), "permissions.yaml"),
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return perm
}

func lastMessage(t *testing.T, messages []message.Message) message.Message {
	t.Helper()
	if len(messages) == 0 {
		t.Fatal("empty messages")
	}
	return messages[len(messages)-1]
}

func tailString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

type recordingAsker struct {
	decision permission.Decision
	calls    int
	requests []permission.Request
}

func (a *recordingAsker) Ask(_ context.Context, req permission.Request) (permission.Decision, error) {
	a.calls++
	a.requests = append(a.requests, req)
	return a.decision, nil
}

type approvalAgent struct {
	result approval.Result
	err    error
}

func (a approvalAgent) ReviewCommand(context.Context, approval.Request) (approval.Result, error) {
	return a.result, a.err
}

type previewExecTool struct {
	previewCalls int
	executeCalls int
	schema       *jsonschema.Schema
}

func (t *previewExecTool) Name() string        { return "preview_exec" }
func (t *previewExecTool) Description() string { return "Preview exec tool." }
func (t *previewExecTool) Schema() *jsonschema.Schema {
	if t.schema == nil {
		t.schema = jsonschema.Reflect(&struct {
			Value string `json:"value"`
		}{})
	}
	return t.schema
}
func (t *previewExecTool) Risk() tool.Risk { return tool.RiskExec }

func (t *previewExecTool) Preview(_ context.Context, raw json.RawMessage) (tool.Preview, error) {
	t.previewCalls++
	var body struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return tool.Preview{}, err
	}
	return tool.Preview{Summary: "preview " + body.Value}, nil
}

func (t *previewExecTool) Execute(_ context.Context, _ json.RawMessage) (tool.Result, error) {
	t.executeCalls++
	return tool.Result{Content: "executed"}, nil
}
