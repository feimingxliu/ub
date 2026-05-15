package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/invopop/jsonschema"

	"github.com/feimingxliu/ub/internal/approval"
	"github.com/feimingxliu/ub/internal/execution"
	"github.com/feimingxliu/ub/internal/message"
	"github.com/feimingxliu/ub/internal/permission"
	"github.com/feimingxliu/ub/internal/provider"
	"github.com/feimingxliu/ub/internal/provider/fake"
	"github.com/feimingxliu/ub/internal/rollout"
	"github.com/feimingxliu/ub/internal/tool"
	"github.com/feimingxliu/ub/internal/tool/fs"
)

type scriptProvider struct {
	scripts  []fake.Script
	requests []provider.Request
}

func (p *scriptProvider) Name() string { return "script" }
func (p *scriptProvider) Caps() provider.Caps {
	return provider.Caps{SupportsTools: true, SupportsStreaming: true}
}

func (p *scriptProvider) Chat(_ context.Context, req provider.Request) (provider.Stream, error) {
	p.requests = append(p.requests, provider.Request{
		Model:    req.Model,
		Messages: cloneMessages(req.Messages),
		Tools:    append([]provider.ToolDefinition(nil), req.Tools...),
	})
	idx := len(p.requests) - 1
	if idx >= len(p.scripts) {
		return nil, errors.New("unexpected extra chat call")
	}
	return fake.New(p.scripts[idx]).Chat(context.Background(), req)
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
		EventDeltaText,
		EventDeltaText,
		EventToolCallStart,
		EventToolCallEnd,
		EventDeltaText,
		EventDone,
	}
	if !reflect.DeepEqual(gotTypes, wantTypes) {
		t.Fatalf("event types = %#v, want %#v", gotTypes, wantTypes)
	}
	if events[0].Text != "he" || events[1].Text != "llo" || events[4].Text != "done" {
		t.Fatalf("delta events = %#v", events)
	}
	if events[2].ToolName != "read" || events[3].ToolName != "read" || events[3].IsError {
		t.Fatalf("tool events = %#v / %#v", events[2], events[3])
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
		if events[i].Type == EventPermission {
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
		if event.Type == EventPermission {
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
		if event.Type == EventPermission {
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

type recordingRollout struct {
	events []rollout.Event
}

func (w *recordingRollout) Append(_ context.Context, event rollout.Event) error {
	w.events = append(w.events, event)
	return nil
}

func (w *recordingRollout) Close() error { return nil }

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
