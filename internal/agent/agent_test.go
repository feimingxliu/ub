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
	"sync"
	"testing"

	"github.com/invopop/jsonschema"

	"github.com/feimingxliu/ub/internal/approval"
	"github.com/feimingxliu/ub/internal/config"
	"github.com/feimingxliu/ub/internal/context"
	"github.com/feimingxliu/ub/internal/hook"
	"github.com/feimingxliu/ub/internal/message"
	execmode "github.com/feimingxliu/ub/internal/mode"
	"github.com/feimingxliu/ub/internal/permission"
	"github.com/feimingxliu/ub/internal/provider"
	"github.com/feimingxliu/ub/internal/provider/fake"
	"github.com/feimingxliu/ub/internal/reasoning"
	"github.com/feimingxliu/ub/internal/rollout"
	contextmgr "github.com/feimingxliu/ub/internal/tokenizer"
	"github.com/feimingxliu/ub/internal/tool"
	"github.com/feimingxliu/ub/internal/tool/fs"
	memorytool "github.com/feimingxliu/ub/internal/tool/memory"
	"github.com/feimingxliu/ub/internal/workspace/filehistory"
	"github.com/feimingxliu/ub/internal/workspace/memory"
)

type scriptProvider struct {
	scripts    []fake.Script
	chatErrors []error
	requests   []provider.Request
	caps       provider.Caps
	scriptIdx  int
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
	if idx < len(p.chatErrors) && p.chatErrors[idx] != nil {
		return nil, p.chatErrors[idx]
	}
	if p.scriptIdx >= len(p.scripts) {
		return nil, errors.New("unexpected extra chat call")
	}
	script := p.scripts[p.scriptIdx]
	p.scriptIdx++
	return fake.New(script).Chat(context.Background(), req)
}

func TestAgentPassesReasoningConfig(t *testing.T) {
	reg := tool.New()
	p := &scriptProvider{scripts: []fake.Script{{fake.TextDelta("ok"), fake.Done()}}}
	a, err := New(Options{
		Provider:  p,
		Tools:     reg,
		Model:     "reasoner",
		Mode:      execmode.ModeWork,
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

func TestAgentPersistsThinkingActivity(t *testing.T) {
	reg := tool.New()
	ro := &recordingRollout{}
	p := &scriptProvider{scripts: []fake.Script{{fake.ReasoningDelta("checking files"), fake.TextDelta("ok"), fake.Done()}}}
	a, err := New(Options{
		Provider: p,
		Tools:    reg,
		Rollout:  ro,
		Model:    "reasoner",
		Mode:     execmode.ModeWork,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := a.Run(context.Background(), Request{SessionID: "sess_1", Prompt: "hi", Turn: 1}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	var activity rollout.ActivityPayload
	for _, event := range ro.events {
		if event.Type != rollout.TypeActivity {
			continue
		}
		payload, ok, err := rollout.ActivityFromEvent(event)
		if err != nil {
			t.Fatalf("ActivityFromEvent: %v", err)
		}
		if ok {
			activity = payload
			break
		}
	}
	if activity.ActivityKind != string(ActivityThinking) || activity.Summary != "checking files" || activity.Content != "checking files" {
		t.Fatalf("activity = %#v, want persisted thinking", activity)
	}
}

func TestAgentFileHistoryTracksToolBeforeExecution(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	path := filepath.Join(root, "main.txt")
	if err := os.WriteFile(path, []byte("old\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	reg := tool.New()
	if err := fs.Register(reg, root); err != nil {
		t.Fatalf("register fs: %v", err)
	}
	ro := &recordingRollout{}
	fh, err := filehistory.New(filehistory.Options{
		Workspace: root,
		SessionID: "sess_1",
		Rollout:   ro,
	})
	if err != nil {
		t.Fatalf("filehistory.New: %v", err)
	}
	p := &scriptProvider{scripts: []fake.Script{
		{fake.ToolCall("edit", map[string]any{"path": "main.txt", "old": "old", "new": "new"}), fake.Done()},
		{fake.TextDelta("done"), fake.Done()},
	}}
	a, err := New(Options{
		Provider:    p,
		Tools:       reg,
		Rollout:     ro,
		Model:       "fake/model",
		Mode:        execmode.ModeWork,
		FileHistory: fh,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := a.Run(context.Background(), Request{SessionID: "sess_1", Prompt: "edit file", Turn: 1}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "new\n" {
		t.Fatalf("file after run = %q err=%v, want new", got, err)
	}
	ro.mu.Lock()
	events := append([]rollout.Event(nil), ro.events...)
	ro.mu.Unlock()
	restored, err := filehistory.New(filehistory.Options{
		Workspace: root,
		SessionID: "sess_1",
		Events:    events,
	})
	if err != nil {
		t.Fatalf("filehistory.New restored: %v", err)
	}
	changes := restored.ChangedFiles(1)
	if len(changes) != 1 || changes[0].Path != "main.txt" || changes[0].Kind != tool.KindModify {
		t.Fatalf("ChangedFiles = %#v, want main.txt modify", changes)
	}
	if _, skipped, err := restored.Rewind(1); err != nil || len(skipped) != 0 {
		t.Fatalf("Rewind err=%v skipped=%#v, want clean restore", err, skipped)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "old\n" {
		t.Fatalf("file after rewind = %q err=%v, want old", got, err)
	}
}

func TestAgentFileHistoryToolsOnlyUsesCurrentSnapshot(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	path := filepath.Join(root, "nested.txt")
	if err := os.WriteFile(path, []byte("old\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	reg := tool.New()
	if err := fs.Register(reg, root); err != nil {
		t.Fatalf("register fs: %v", err)
	}
	ro := &recordingRollout{}
	fh, err := filehistory.New(filehistory.Options{
		Workspace: root,
		SessionID: "parent_sess",
		Rollout:   ro,
	})
	if err != nil {
		t.Fatalf("filehistory.New: %v", err)
	}
	if err := fh.MakeSnapshot(context.Background(), 7); err != nil {
		t.Fatalf("MakeSnapshot: %v", err)
	}
	p := &scriptProvider{scripts: []fake.Script{
		{fake.ToolCall("edit", map[string]any{"path": "nested.txt", "old": "old", "new": "new"}), fake.Done()},
		{fake.TextDelta("done"), fake.Done()},
	}}
	a, err := New(Options{
		Provider:             p,
		Tools:                reg,
		Rollout:              ro,
		Model:                "fake/model",
		Mode:                 execmode.ModeWork,
		FileHistory:          fh,
		FileHistoryToolsOnly: true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := a.Run(context.Background(), Request{SessionID: "child_sess", Prompt: "edit file", Turn: 1}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	changes := fh.ChangedFiles(7)
	if len(changes) != 1 || changes[0].Path != "nested.txt" || changes[0].Kind != tool.KindModify {
		t.Fatalf("ChangedFiles(7) = %#v, want nested.txt modify", changes)
	}
	if changes := fh.ChangedFiles(1); len(changes) != 0 {
		t.Fatalf("ChangedFiles(1) = %#v, want no child snapshot", changes)
	}
}

func TestAgentRecoversOutputTokenLimitViaRecoveryMessage(t *testing.T) {
	reg := tool.New()
	ro := &recordingRollout{}
	p := &scriptProvider{scripts: []fake.Script{
		{fake.ReasoningDelta("thinking..."), fake.Done()},
		{fake.TextDelta("recovered"), fake.Done()},
	}}
	a, err := New(Options{
		Provider:  p,
		Tools:     reg,
		Rollout:   ro,
		Model:     "reasoner",
		Mode:      execmode.ModeWork,
		Reasoning: &reasoning.Config{Effort: reasoning.EffortHigh},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	res, err := a.Run(context.Background(), Request{SessionID: "sess_1", Prompt: "hi", Turn: 1})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Text != "recovered" {
		t.Fatalf("text = %q", res.Text)
	}
	if len(p.requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(p.requests))
	}
	// First request carries the original reasoning config.
	if p.requests[0].Reasoning == nil || p.requests[0].Reasoning.Effort != reasoning.EffortHigh {
		t.Fatalf("first request reasoning = %#v", p.requests[0].Reasoning)
	}
	// Recovery request also keeps reasoning (claude-code style: don't disable it).
	if p.requests[1].Reasoning == nil || p.requests[1].Reasoning.Effort != reasoning.EffortHigh {
		t.Fatalf("recovery reasoning = %#v, want preserved EffortHigh", p.requests[1].Reasoning)
	}
	// Recovery user message ("Output token limit hit...") must be the last
	// non-system message in the second request.
	last := lastMessage(t, p.requests[1].Messages)
	if last.Role != message.RoleUser || !strings.Contains(last.Text(), "Output token limit hit") {
		t.Fatalf("recovery last user message = %#v", last)
	}
	for _, event := range ro.events {
		if event.Type == rollout.TypeError {
			t.Fatalf("rollout should not record error after recovery; got %+v", ro.events)
		}
	}
}

func TestAgentReportsErrorAfterMaxOutputTokenRecoveries(t *testing.T) {
	reg := tool.New()
	ro := &recordingRollout{}
	// Every chat call returns reasoning-only — recovery will exhaust the limit.
	p := &scriptProvider{scripts: []fake.Script{
		{fake.ReasoningDelta("thinking 1"), fake.Done()},
		{fake.ReasoningDelta("thinking 2"), fake.Done()},
		{fake.ReasoningDelta("thinking 3"), fake.Done()},
		{fake.ReasoningDelta("thinking 4"), fake.Done()},
	}}
	a, err := New(Options{
		Provider:  p,
		Tools:     reg,
		Rollout:   ro,
		Model:     "reasoner",
		Mode:      execmode.ModeWork,
		Reasoning: &reasoning.Config{Effort: reasoning.EffortHigh},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, runErr := a.Run(context.Background(), Request{SessionID: "sess_1", Prompt: "hi", Turn: 1})
	if runErr == nil {
		t.Fatal("Run should fail after recovery limit exhausted")
	}
	if !strings.Contains(runErr.Error(), "max_output_tokens") {
		t.Fatalf("error = %v, want max_output_tokens hint", runErr)
	}
	// 1 initial + 3 recovery attempts = 4 chat calls.
	if len(p.requests) != 1+maxOutputTokensRecoveryLimit {
		t.Fatalf("requests = %d, want %d", len(p.requests), 1+maxOutputTokensRecoveryLimit)
	}
}

func TestAgentReportsFullyEmptyResponse(t *testing.T) {
	reg := tool.New()
	ro := &recordingRollout{}
	p := &scriptProvider{scripts: []fake.Script{{fake.Done()}}}
	a, err := New(Options{
		Provider: p,
		Tools:    reg,
		Rollout:  ro,
		Model:    "reasoner",
		Mode:     execmode.ModeWork,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, runErr := a.Run(context.Background(), Request{SessionID: "sess_1", Prompt: "hi", Turn: 1})
	if runErr == nil {
		t.Fatal("Run should fail when model returns nothing")
	}
	if !strings.Contains(runErr.Error(), "empty stream") {
		t.Fatalf("error = %v, want empty stream description", runErr)
	}
}

func TestAgentInjectsRuntimeContextWithoutPersistingIt(t *testing.T) {
	reg := tool.New()
	p := &scriptProvider{scripts: []fake.Script{{fake.TextDelta("ok"), fake.Done()}}}
	a, err := New(Options{
		Provider: p,
		Tools:    reg,
		Model:    "fake/model",
		Mode:     execmode.ModeWork,
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
	for _, want := range []string{
		"<cwd>/tmp/workspace</cwd>",
		"<shell>/bin/sh</shell>",
		"<os>linux</os>",
		"Do not invent alternate project paths such as /home/user",
		"Use read only for regular files",
		"use the cwd parameter",
	} {
		if !containsText(got, want) {
			t.Fatalf("runtime context missing %q:\n%#v", want, got)
		}
	}
	if containsText(res.Messages, "<environment_context>") {
		t.Fatalf("runtime context leaked into result history: %#v", res.Messages)
	}
}

func TestAgentInjectsCodingHarnessInstructionsWithoutPersistingThem(t *testing.T) {
	reg := tool.New()
	p := &scriptProvider{scripts: []fake.Script{{fake.TextDelta("ok"), fake.Done()}}}
	a, err := New(Options{
		Provider: p,
		Tools:    reg,
		Model:    "fake/model",
		Mode:     execmode.ModeWork,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	res, err := a.Run(context.Background(), Request{Prompt: "fix a bug", Turn: 1})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := p.requests[0].Messages
	for _, want := range []string{
		"<coding_agent_instructions>",
		"Read the relevant files before proposing or applying edits",
		"Prefer purpose-built tools",
		"Do not claim tests, builds, or checks passed unless they actually ran and passed",
	} {
		if !containsText(got, want) {
			t.Fatalf("coding harness missing %q:\n%#v", want, got)
		}
	}
	if containsText(res.Messages, "<coding_agent_instructions>") {
		t.Fatalf("coding harness leaked into result history: %#v", res.Messages)
	}
}

func TestPromptHarnessFakeProviderBehaviorRegressions(t *testing.T) {
	t.Run("directory prompt uses ls instead of read", func(t *testing.T) {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
		reg := tool.New()
		if err := fs.Register(reg, root); err != nil {
			t.Fatalf("register fs: %v", err)
		}
		p := &scriptProvider{scripts: []fake.Script{
			{fake.ToolCall("ls", map[string]any{"path": "."}), fake.Done()},
			{fake.TextDelta("used ls for directory"), fake.Done()},
		}}
		a := newTestAgent(t, p, reg, nil, execmode.ModeWork)
		res, err := a.Run(context.Background(), Request{Prompt: "inspect the current directory", Turn: 1})
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if res.Text != "used ls for directory" {
			t.Fatalf("text = %q", res.Text)
		}
		if !containsToolDescription(p.requests[0].Tools, "read", "Never use read for directories") {
			t.Fatalf("request missing directory tool-choice guidance: %#v", p.requests[0].Tools)
		}
	})

	t.Run("complex edit reads before writing", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "main.go")
		if err := os.WriteFile(path, []byte("package main\nfunc main() { println(\"old\") }\n"), 0o644); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
		reg := tool.New()
		if err := fs.Register(reg, root); err != nil {
			t.Fatalf("register fs: %v", err)
		}
		p := &scriptProvider{scripts: []fake.Script{
			{fake.ToolCall("read", map[string]any{"path": "main.go"}), fake.Done()},
			{fake.ToolCall("edit", map[string]any{"path": "main.go", "old": "old", "new": "new"}), fake.Done()},
			{fake.TextDelta("read first, then edited"), fake.Done()},
		}}
		a := newTestAgent(t, p, reg, nil, execmode.ModeWork)
		if _, err := a.Run(context.Background(), Request{Prompt: "change old to new and verify the file", Turn: 1}); err != nil {
			t.Fatalf("Run: %v", err)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read file: %v", err)
		}
		if !strings.Contains(string(raw), "new") {
			t.Fatalf("file was not edited after read-before-write flow: %q", raw)
		}
		if !containsText(p.requests[0].Messages, "Read the relevant files before proposing or applying edits") {
			t.Fatalf("request missing read-before-edit guidance: %#v", p.requests[0].Messages)
		}
	})

	t.Run("failed validation is not reported as passing", func(t *testing.T) {
		reg := tool.New()
		if err := reg.Register(&failingCheckTool{}); err != nil {
			t.Fatalf("register failing check: %v", err)
		}
		p := &scriptProvider{scripts: []fake.Script{
			{fake.ToolCall("test_check", map[string]any{}), fake.Done()},
			{fake.TextDelta("tests failed: exit_code=1"), fake.Done()},
		}}
		a := newTestAgent(t, p, reg, nil, execmode.ModeWork)
		res, err := a.Run(context.Background(), Request{Prompt: "run validation and summarize the result", Turn: 1})
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if strings.Contains(strings.ToLower(res.Text), "passed") {
			t.Fatalf("failed validation was reported as passing: %q", res.Text)
		}
		if !containsText(p.requests[0].Messages, "Do not claim tests, builds, or checks passed unless they actually ran and passed") {
			t.Fatalf("request missing honest-validation guidance: %#v", p.requests[0].Messages)
		}
	})
}

func TestAgentInjectsPlanModeInstructionsWithoutPersistingThem(t *testing.T) {
	reg := tool.New()
	p := &scriptProvider{scripts: []fake.Script{{fake.TextDelta("ok"), fake.Done()}}}
	a, err := New(Options{
		Provider: p,
		Tools:    reg,
		Model:    "fake/model",
		Mode:     execmode.ModePlan,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	res, err := a.Run(context.Background(), Request{Prompt: "add CI", Turn: 1})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(p.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(p.requests))
	}
	got := p.requests[0].Messages
	for _, want := range []string{
		"<execution_mode>",
		"mode=plan",
		"Inspect the workspace only with read, ls, glob, and grep",
		"create a plan with the plan_write tool before starting implementation",
		"update that same plan with plan_update instead of creating another plan",
		"Do not create, edit, delete, move, format, install, execute commands",
		"call exit_plan_mode with the plan_id",
	} {
		if !containsText(got, want) {
			t.Fatalf("plan mode instructions missing %q:\n%#v", want, got)
		}
	}
	if containsText(res.Messages, "<execution_mode>") {
		t.Fatalf("plan mode instructions leaked into result history: %#v", res.Messages)
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
	a := newTestAgent(t, p, reg, perm, execmode.ModeWork)

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
		Mode:            execmode.ModeWork,
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
		Mode:       execmode.ModeWork,
		MaxTurns:   1,
		Reasoning:  &reasoning.Config{Effort: reasoning.EffortHigh},
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
	if p.requests[0].Reasoning == nil || p.requests[0].Reasoning.Effort != reasoning.EffortHigh {
		t.Fatalf("first request reasoning = %#v, want high", p.requests[0].Reasoning)
	}
	if len(p.requests[1].Tools) != 0 {
		t.Fatalf("final request tools = %#v, want none", p.requests[1].Tools)
	}
	if p.requests[1].Reasoning != nil {
		t.Fatalf("final request reasoning = %#v, want omitted", p.requests[1].Reasoning)
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

type fakeLimitAsker struct {
	calls     int
	extension int
}

func (f *fakeLimitAsker) AskExtension(_ context.Context, _ LimitExtensionRequest) (LimitExtensionResponse, error) {
	f.calls++
	if f.calls == 1 {
		return LimitExtensionResponse{ExtraTurns: f.extension}, nil
	}
	return LimitExtensionResponse{}, nil
}

func TestAgentLimitAskerCanExtendLoop(t *testing.T) {
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
		// First turn: keep calling tools so we burn through the 1-turn cap.
		{fake.ToolCall("read", map[string]any{"path": "main.go"}), fake.Done()},
		// Extension granted: second turn answers without tools.
		{fake.TextDelta("done after extension"), fake.Done()},
	}}
	asker := &fakeLimitAsker{extension: 1}
	a, err := New(Options{
		Provider:   p,
		Tools:      reg,
		Permission: perm,
		Model:      "fake/model",
		Mode:       execmode.ModeWork,
		MaxTurns:   1,
		LimitAsker: asker,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	res, err := a.Run(context.Background(), Request{SessionID: "sess_ext", Prompt: "go", Turn: 1})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if asker.calls != 1 {
		t.Fatalf("asker called %d times, want 1", asker.calls)
	}
	if res.Text != "done after extension" {
		t.Fatalf("text = %q, want extension stream output", res.Text)
	}
	// Two chat requests both with tools — no finalize fallback.
	if len(p.requests) != 2 {
		t.Fatalf("chat calls = %d, want 2", len(p.requests))
	}
	if len(p.requests[1].Tools) == 0 {
		t.Fatalf("extended request should keep tools available")
	}
	if containsText(p.requests[1].Messages, "Do not call tools") {
		t.Fatalf("extension path must not inject the no-tool instruction")
	}
}

func TestAgentLimitAskerDecliningFallsThroughToFinalize(t *testing.T) {
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
		{fake.ToolCall("read", map[string]any{"path": "main.go"}), fake.Done()},
		{fake.TextDelta("finalize text"), fake.Done()},
	}}
	asker := &fakeLimitAsker{extension: 0}
	a, err := New(Options{
		Provider:   p,
		Tools:      reg,
		Permission: perm,
		Model:      "fake/model",
		Mode:       execmode.ModeWork,
		MaxTurns:   1,
		LimitAsker: asker,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	res, err := a.Run(context.Background(), Request{SessionID: "sess_no_ext", Prompt: "go", Turn: 1})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if asker.calls != 1 {
		t.Fatalf("asker called %d times, want 1", asker.calls)
	}
	if len(p.requests) != 2 || len(p.requests[1].Tools) != 0 {
		t.Fatalf("declined extension should still finalize without tools: tools=%v", p.requests[1].Tools)
	}
	if res.Text != "finalize text" {
		t.Fatalf("text = %q, want finalize output", res.Text)
	}
}

func TestAgentDefaultMaxTurnsIsUnbounded(t *testing.T) {
	a, err := New(Options{
		Provider: &scriptProvider{},
		Tools:    tool.New(),
		Model:    "fake/model",
		Mode:     execmode.ModeWork,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if a.maxTurns != 0 {
		t.Fatalf("maxTurns = %d, want 0 (unbounded default)", a.maxTurns)
	}
}

func TestAgentDetectsRepeatedToolLoop(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	reg := tool.New()
	if err := fs.Register(reg, root); err != nil {
		t.Fatalf("register fs: %v", err)
	}
	perm := newPermissionManager(t, nil)
	scripts := make([]fake.Script, 0, repeatedToolMaxRepeats+2)
	for i := 0; i < repeatedToolMaxRepeats+1; i++ {
		scripts = append(scripts, fake.Script{fake.ToolCall("read", map[string]any{"path": "main.go"}), fake.Done()})
	}
	scripts = append(scripts, fake.Script{fake.TextDelta("stopped repeated loop"), fake.Done()})
	p := &scriptProvider{scripts: scripts}
	var events []Event
	a, err := New(Options{
		Provider:   p,
		Tools:      reg,
		Permission: perm,
		Model:      "fake/model",
		Mode:       execmode.ModeWork,
		Events: func(event Event) {
			events = append(events, event)
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	res, err := a.Run(context.Background(), Request{SessionID: "sess_repeat", Prompt: "loop", Turn: 1})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Text != "stopped repeated loop" {
		t.Fatalf("text = %q, want repeated-loop final output", res.Text)
	}
	if got, want := len(p.requests), repeatedToolMaxRepeats+2; got != want {
		t.Fatalf("chat calls = %d, want %d", got, want)
	}
	finalReq := p.requests[len(p.requests)-1]
	if len(finalReq.Tools) != 0 {
		t.Fatalf("final request tools = %#v, want none", finalReq.Tools)
	}
	if !containsText(finalReq.Messages, "Do not call tools") {
		t.Fatalf("final request missing no-tool instruction: %#v", finalReq.Messages)
	}
	foundNotice := false
	for _, event := range events {
		if event.Type == EventActivity && strings.Contains(event.Summary, "repeated tool loop detected") {
			foundNotice = true
			break
		}
	}
	if !foundNotice {
		t.Fatalf("missing repeated-loop activity notice: %#v", events)
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
		Mode:       execmode.ModeWork,
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
		Mode:       execmode.ModeWork,
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

func TestAgentStoresSignedThinkingAsHiddenReasoningBlock(t *testing.T) {
	reg := tool.New()
	perm := newPermissionManager(t, nil)
	p := &scriptProvider{scripts: []fake.Script{
		{
			fake.ReasoningDelta("checking context"),
			{Type: provider.EventReasoningDelta, ReasoningSignature: "sig"},
			fake.TextDelta("answer"),
			fake.Done(),
		},
	}}
	a, err := New(Options{
		Provider:   p,
		Tools:      reg,
		Permission: perm,
		Model:      "fake/model",
		Mode:       execmode.ModeWork,
		Reasoning:  &reasoning.Config{Effort: reasoning.EffortHigh},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	res, err := a.Run(context.Background(), Request{Prompt: "think", Turn: 1})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	assistant := lastMessage(t, res.Messages)
	if got := assistant.Text(); got != "answer" {
		t.Fatalf("assistant text = %q, want answer", got)
	}
	if len(assistant.Content) < 2 {
		t.Fatalf("assistant content = %#v, want reasoning and text blocks", assistant.Content)
	}
	reasoningBlock := assistant.Content[0]
	if reasoningBlock.Type != message.BlockReasoning ||
		reasoningBlock.Reasoning != "checking context" ||
		reasoningBlock.ReasoningSignature != "sig" {
		t.Fatalf("reasoning block = %#v, want signed hidden reasoning", reasoningBlock)
	}
	if assistant.Content[1].Type != message.BlockText || assistant.Content[1].Text != "answer" {
		t.Fatalf("text block = %#v, want answer text", assistant.Content[1])
	}
}

func TestConsumeStreamStoresSignedThinkingForToolOnlyTurn(t *testing.T) {
	stream, err := fake.New(fake.Script{
		fake.ReasoningDelta("need a tool"),
		{Type: provider.EventReasoningDelta, ReasoningSignature: "sig-tool"},
		fake.ToolCall("read", map[string]string{"path": "main.go"}),
		fake.Done(),
	}).Chat(context.Background(), provider.Request{})
	if err != nil {
		t.Fatalf("fake Chat: %v", err)
	}

	var a Agent
	consumed, err := a.consumeStream(context.Background(), "", 1, stream, 0)
	if err != nil {
		t.Fatalf("consumeStream: %v", err)
	}
	if consumed.message.Text() != "" {
		t.Fatalf("assistant text = %q, want empty", consumed.message.Text())
	}
	if len(consumed.message.Content) < 2 {
		t.Fatalf("assistant content = %#v, want reasoning and tool_use blocks", consumed.message.Content)
	}
	reasoningBlock := consumed.message.Content[0]
	if reasoningBlock.Type != message.BlockReasoning ||
		reasoningBlock.Reasoning != "need a tool" ||
		reasoningBlock.ReasoningSignature != "sig-tool" {
		t.Fatalf("reasoning block = %#v, want signed hidden reasoning", reasoningBlock)
	}
	if consumed.message.Content[1].Type != message.BlockToolUse {
		t.Fatalf("second block = %#v, want tool_use", consumed.message.Content[1])
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
	a := newTestAgent(t, p, reg, perm, execmode.ModePlan)

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

func TestAgentPlanModeRejectsExecWithoutApproval(t *testing.T) {
	reg := tool.New()
	if err := reg.Register(&namedRiskTool{name: "bash", risk: tool.RiskExec}); err != nil {
		t.Fatalf("register bash: %v", err)
	}
	asker := &recordingAsker{decision: permission.DecisionAllow}
	perm := newPermissionManager(t, asker)
	p := &scriptProvider{scripts: []fake.Script{
		{fake.ToolCall("bash", map[string]any{"command": "git status"}), fake.Done()},
		{fake.TextDelta("blocked"), fake.Done()},
	}}
	a := newTestAgent(t, p, reg, perm, execmode.ModePlan)

	if _, err := a.Run(context.Background(), Request{Prompt: "inspect with bash", Turn: 1}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(asker.requests) != 0 {
		t.Fatalf("plan-mode bash should not reach permission asker: %#v", asker.requests)
	}
	last := lastMessage(t, p.requests[1].Messages)
	block := last.Content[0]
	if !block.IsError || !strings.Contains(block.Output, "not available in plan mode") {
		t.Fatalf("tool result block = %#v, want plan-mode denial", block)
	}
}

func TestAgentHidesAndRejectsPlanWriteOutsidePlanMode(t *testing.T) {
	reg := tool.New()
	planWrite := &namedSafeTool{name: "plan_write"}
	if err := reg.Register(planWrite); err != nil {
		t.Fatalf("register plan_write: %v", err)
	}
	if err := reg.Register(&namedSafeTool{name: "plan_update"}); err != nil {
		t.Fatalf("register plan_update: %v", err)
	}
	if err := reg.Register(&namedSafeTool{name: "plan_update_step"}); err != nil {
		t.Fatalf("register plan_update_step: %v", err)
	}
	if err := reg.Register(&namedSafeTool{name: "todo_write"}); err != nil {
		t.Fatalf("register todo_write: %v", err)
	}
	if err := reg.Register(&namedSafeTool{name: "todo_update"}); err != nil {
		t.Fatalf("register todo_update: %v", err)
	}
	if err := reg.Register(&namedSafeTool{name: "read"}); err != nil {
		t.Fatalf("register read: %v", err)
	}
	if err := reg.Register(&namedSafeTool{name: "grep"}); err != nil {
		t.Fatalf("register grep: %v", err)
	}
	if err := reg.Register(&namedSafeTool{name: "ask"}); err != nil {
		t.Fatalf("register ask: %v", err)
	}
	for _, tl := range NewPlanModeTools() {
		if err := reg.Register(tl); err != nil {
			t.Fatalf("register %s: %v", tl.Name(), err)
		}
	}
	if err := reg.Register(&namedSafeTool{name: "remember"}); err != nil {
		t.Fatalf("register remember: %v", err)
	}
	if err := reg.Register(&namedRiskTool{name: "edit", risk: tool.RiskWrite}); err != nil {
		t.Fatalf("register edit: %v", err)
	}
	if err := reg.Register(&namedRiskTool{name: "bash", risk: tool.RiskExec}); err != nil {
		t.Fatalf("register bash: %v", err)
	}
	if err := reg.Register(&namedRiskTool{name: "web_search", risk: tool.RiskNetwork}); err != nil {
		t.Fatalf("register web_search: %v", err)
	}
	if err := reg.Register(&namedRiskTool{name: "web_fetch", risk: tool.RiskNetwork}); err != nil {
		t.Fatalf("register web_fetch: %v", err)
	}
	tools, err := toolDefinitions(reg, execmode.ModeWork)
	if err != nil {
		t.Fatalf("toolDefinitions work: %v", err)
	}
	if !toolNamesContain(tools, "enter_plan_mode") {
		t.Fatalf("work mode should advertise enter_plan_mode: %#v", tools)
	}
	if toolNamesContain(tools, "exit_plan_mode") {
		t.Fatalf("work mode should not advertise exit_plan_mode: %#v", tools)
	}

	tools, err = toolDefinitions(reg, execmode.ModeAuto)
	if err != nil {
		t.Fatalf("toolDefinitions auto: %v", err)
	}
	if toolNamesContain(tools, "plan_write") {
		t.Fatalf("auto mode should not advertise plan_write: %#v", tools)
	}
	if toolNamesContain(tools, "plan_update") {
		t.Fatalf("auto mode should not advertise plan_update: %#v", tools)
	}
	if toolNamesContain(tools, "enter_plan_mode") || toolNamesContain(tools, "exit_plan_mode") {
		t.Fatalf("auto mode should not advertise plan-mode switch tools: %#v", tools)
	}
	if !toolNamesContain(tools, "plan_update_step") {
		t.Fatalf("auto mode should keep plan_update_step for execution progress: %#v", tools)
	}
	if !toolNamesContain(tools, "todo_write") || !toolNamesContain(tools, "todo_update") {
		t.Fatalf("auto mode should keep todo tools for execution progress: %#v", tools)
	}
	if !toolNamesContain(tools, "read") {
		t.Fatalf("auto mode should keep non-plan tools: %#v", tools)
	}
	if !toolNamesContain(tools, "edit") || !toolNamesContain(tools, "bash") || !toolNamesContain(tools, "web_search") || !toolNamesContain(tools, "web_fetch") {
		t.Fatalf("auto mode should keep write, exec, and network tools: %#v", tools)
	}
	tools, err = toolDefinitions(reg, execmode.ModeFullAccess)
	if err != nil {
		t.Fatalf("toolDefinitions full-access: %v", err)
	}
	if toolNamesContain(tools, "plan_write") || toolNamesContain(tools, "plan_update") {
		t.Fatalf("full-access mode should not advertise plan tools: %#v", tools)
	}
	if toolNamesContain(tools, "enter_plan_mode") || toolNamesContain(tools, "exit_plan_mode") {
		t.Fatalf("full-access mode should not advertise plan-mode switch tools: %#v", tools)
	}
	if !toolNamesContain(tools, "edit") || !toolNamesContain(tools, "bash") || !toolNamesContain(tools, "web_search") || !toolNamesContain(tools, "web_fetch") {
		t.Fatalf("full-access mode should keep write, exec, and network tools: %#v", tools)
	}
	tools, err = toolDefinitions(reg, execmode.ModePlan)
	if err != nil {
		t.Fatalf("toolDefinitions plan: %v", err)
	}
	if !toolNamesContain(tools, "plan_write") {
		t.Fatalf("plan mode should advertise plan_write: %#v", tools)
	}
	if !toolNamesContain(tools, "plan_update") {
		t.Fatalf("plan mode should advertise plan_update: %#v", tools)
	}
	if !toolNamesContain(tools, "exit_plan_mode") {
		t.Fatalf("plan mode should advertise exit_plan_mode: %#v", tools)
	}
	for _, hidden := range []string{"enter_plan_mode", "plan_update_step", "todo_write", "todo_update", "edit", "bash", "web_search", "web_fetch", "remember"} {
		if toolNamesContain(tools, hidden) {
			t.Fatalf("plan mode should not advertise %s: %#v", hidden, tools)
		}
	}
	for _, shown := range []string{"read", "grep", "ask"} {
		if !toolNamesContain(tools, shown) {
			t.Fatalf("plan mode should advertise %s: %#v", shown, tools)
		}
	}

	perm := newPermissionManager(t, nil)
	p := &scriptProvider{scripts: []fake.Script{
		{fake.ToolCall("plan_write", map[string]any{"title": "x", "steps": []string{"a"}}), fake.Done()},
		{fake.TextDelta("blocked"), fake.Done()},
	}}
	a := newTestAgent(t, p, reg, perm, execmode.ModeAuto)
	if _, err := a.Run(context.Background(), Request{Prompt: "make a plan", Turn: 1}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if planWrite.executeCalls != 0 {
		t.Fatalf("plan_write executed in auto mode")
	}
	last := lastMessage(t, p.requests[1].Messages)
	block := last.Content[0]
	if !block.IsError || !strings.Contains(block.Output, "only available in plan mode") {
		t.Fatalf("tool result block = %#v, want plan-mode denial", block)
	}
}

func TestAgentEnterPlanModeRefreshesToolsAndRecordsActivity(t *testing.T) {
	reg := tool.New()
	for _, tl := range NewPlanModeTools() {
		if err := reg.Register(tl); err != nil {
			t.Fatalf("register %s: %v", tl.Name(), err)
		}
	}
	if err := reg.Register(&namedSafeTool{name: "read"}); err != nil {
		t.Fatalf("register read: %v", err)
	}
	if err := reg.Register(&namedSafeTool{name: "plan_write"}); err != nil {
		t.Fatalf("register plan_write: %v", err)
	}
	mode := execmode.ModeWork
	controller := &recordingPlanModeController{
		confirm: func(req PlanModeRequest) PlanModeResponse {
			if req.Action != PlanModeEnter {
				t.Fatalf("action = %q, want enter", req.Action)
			}
			from := mode
			mode = execmode.ModePlan
			return PlanModeResponse{Approved: true, FromMode: from, ToMode: mode}
		},
	}
	ro := &recordingRollout{}
	p := &scriptProvider{scripts: []fake.Script{
		{fake.ToolCall("enter_plan_mode", map[string]any{"reason": "multi-file change"}), fake.Done()},
		{fake.TextDelta("planning"), fake.Done()},
	}}
	var events []Event
	a, err := New(Options{
		Provider: p,
		Tools:    reg,
		Rollout:  ro,
		Model:    "fake/model",
		Mode:     execmode.ModeWork,
		ModeFunc: func() execmode.Mode {
			return mode
		},
		PlanMode: controller,
		Events: func(event Event) {
			events = append(events, event)
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := a.Run(context.Background(), Request{SessionID: "sess_plan", Prompt: "implement", Turn: 1}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if controller.calls != 1 || controller.requests[0].Reason != "multi-file change" {
		t.Fatalf("controller = calls %d requests %#v", controller.calls, controller.requests)
	}
	if !toolNamesContain(p.requests[1].Tools, "exit_plan_mode") || !toolNamesContain(p.requests[1].Tools, "plan_write") {
		t.Fatalf("plan mode tools after switch = %#v", p.requests[1].Tools)
	}
	if toolNamesContain(p.requests[1].Tools, "enter_plan_mode") {
		t.Fatalf("enter_plan_mode should hide after switch: %#v", p.requests[1].Tools)
	}
	var sawModeActivity bool
	for _, event := range events {
		if event.Type == EventActivity && event.ActivityKind == ActivityMode {
			sawModeActivity = true
			if event.Source != "tool" || event.Decision != "approved" || !event.Allowed || !strings.Contains(event.Content, "from=work") || !strings.Contains(event.Content, "to=plan") {
				t.Fatalf("mode activity = %#v", event)
			}
		}
	}
	if !sawModeActivity {
		t.Fatalf("mode activity missing: %#v", events)
	}
	var persisted rollout.ActivityPayload
	for _, event := range ro.events {
		payload, ok, err := rollout.ActivityFromEvent(event)
		if err != nil {
			t.Fatalf("ActivityFromEvent: %v", err)
		}
		if ok && payload.ActivityKind == string(ActivityMode) {
			persisted = payload
			break
		}
	}
	if persisted.ActivityKind != string(ActivityMode) || persisted.Source != "tool" || persisted.Decision != "approved" {
		t.Fatalf("persisted mode activity = %#v", persisted)
	}
}

func TestExitPlanModeRequiresPlanIDBeforePrompt(t *testing.T) {
	var exitTool tool.Tool
	for _, tl := range NewPlanModeTools() {
		if tl.Name() == "exit_plan_mode" {
			exitTool = tl
			break
		}
	}
	if exitTool == nil {
		t.Fatal("exit_plan_mode tool missing")
	}
	controller := &recordingPlanModeController{
		response: PlanModeResponse{Approved: true, FromMode: execmode.ModePlan, ToMode: execmode.ModeWork},
	}
	ctx := contextWithPlanModeController(context.Background(), controller)
	result, err := exitTool.Execute(ctx, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if controller.calls != 0 {
		t.Fatalf("controller was called without plan_id: %#v", controller.requests)
	}
	if !result.IsError || !strings.Contains(result.Content, "requires a plan_id") {
		t.Fatalf("result = %#v, want missing plan_id error", result)
	}
	if result.Metadata["mode_action"] != string(PlanModeExit) || result.Metadata["mode_status"] != "missing_plan" || result.Metadata["mode_approved"] != "false" {
		t.Fatalf("metadata = %#v, want missing_plan denial", result.Metadata)
	}
}

func TestAgentRefreshesAdvertisedToolsAfterModeSwitch(t *testing.T) {
	mode := execmode.ModeWork
	reg := tool.New()
	if err := reg.Register(&namedSafeTool{name: "plan_write"}); err != nil {
		t.Fatalf("register plan_write: %v", err)
	}
	if err := reg.Register(&namedSafeTool{name: "plan_update"}); err != nil {
		t.Fatalf("register plan_update: %v", err)
	}
	if err := reg.Register(&namedRiskTool{name: "write", risk: tool.RiskWrite}); err != nil {
		t.Fatalf("register write: %v", err)
	}
	if err := reg.Register(&modeFlipTool{set: func() { mode = execmode.ModePlan }}); err != nil {
		t.Fatalf("register flip_mode: %v", err)
	}
	p := &scriptProvider{scripts: []fake.Script{
		{fake.ToolCall("flip_mode", map[string]any{}), fake.Done()},
		{fake.TextDelta("done"), fake.Done()},
	}}
	a, err := New(Options{
		Provider: p,
		Tools:    reg,
		Model:    "fake/model",
		Mode:     execmode.ModeWork,
		ModeFunc: func() execmode.Mode {
			return mode
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := a.Run(context.Background(), Request{Prompt: "create CI", Turn: 1}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(p.requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(p.requests))
	}
	if toolNamesContain(p.requests[0].Tools, "plan_write") {
		t.Fatalf("work mode should not advertise plan_write: %#v", p.requests[0].Tools)
	}
	if !toolNamesContain(p.requests[0].Tools, "write") {
		t.Fatalf("work mode should advertise write: %#v", p.requests[0].Tools)
	}
	if !toolNamesContain(p.requests[1].Tools, "plan_write") {
		t.Fatalf("plan mode should advertise plan_write after switch: %#v", p.requests[1].Tools)
	}
	if !toolNamesContain(p.requests[1].Tools, "plan_update") {
		t.Fatalf("plan mode should advertise plan_update after switch: %#v", p.requests[1].Tools)
	}
	if toolNamesContain(p.requests[1].Tools, "write") {
		t.Fatalf("plan mode should hide write after switch: %#v", p.requests[1].Tools)
	}
	if !containsText(p.requests[1].Messages, "mode=plan") {
		t.Fatalf("second request missing plan-mode instructions: %#v", p.requests[1].Messages)
	}
}

func TestAgentAskToolUsesStructuredAskerAndPersistsActivities(t *testing.T) {
	reg := tool.New()
	if err := reg.Register(NewAskTool()); err != nil {
		t.Fatalf("register ask: %v", err)
	}
	ro := &recordingRollout{}
	asker := &recordingUserAsker{
		response: AskResponse{Answers: []AskAnswer{{
			Header:   "Storage",
			Question: "Which backend?",
			Selected: []AskOption{{Label: "SQLite", Description: "local durable store"}},
		}}},
	}
	p := &scriptProvider{scripts: []fake.Script{
		{fake.ToolCall("ask", map[string]any{"questions": []map[string]any{{
			"header":   "Storage",
			"question": "Which backend?",
			"options": []map[string]any{
				{"label": "SQLite", "description": "local durable store"},
				{"label": "Postgres", "description": "shared server"},
			},
		}}}), fake.Done()},
		{fake.TextDelta("done"), fake.Done()},
	}}
	a, err := New(Options{
		Provider: p,
		Tools:    reg,
		Rollout:  ro,
		Model:    "fake/model",
		Mode:     execmode.ModePlan,
		Asker:    asker,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := a.Run(context.Background(), Request{SessionID: "sess_ask", Prompt: "choose", Turn: 2}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if asker.calls != 1 {
		t.Fatalf("asker calls = %d, want 1", asker.calls)
	}
	if got := asker.requests[0]; got.SessionID != "sess_ask" || got.UserTurn != 2 || got.ToolUseID == "" || len(got.Questions) != 1 {
		t.Fatalf("ask request = %#v", got)
	}
	last := lastMessage(t, p.requests[1].Messages)
	block := last.Content[0]
	if block.IsError || !strings.Contains(block.Output, "SQLite") || !strings.Contains(block.Output, "ask answered") {
		t.Fatalf("tool result = %#v, want ask answer", block)
	}
	var requested, answered bool
	for _, event := range ro.events {
		if event.Type != rollout.TypeActivity {
			continue
		}
		payload, ok, err := rollout.ActivityFromEvent(event)
		if err != nil {
			t.Fatalf("ActivityFromEvent: %v", err)
		}
		if !ok || payload.ActivityKind != string(ActivityAsk) {
			continue
		}
		requested = requested || payload.Status == "requested"
		answered = answered || payload.Status == "answered" && strings.Contains(payload.Content, "SQLite")
	}
	if !requested || !answered {
		t.Fatalf("ask activities requested=%v answered=%v events=%#v", requested, answered, ro.events)
	}
}

func TestAgentAskToolHeadlessFallsBackWithoutBlocking(t *testing.T) {
	reg := tool.New()
	if err := reg.Register(NewAskTool()); err != nil {
		t.Fatalf("register ask: %v", err)
	}
	p := &scriptProvider{scripts: []fake.Script{
		{fake.ToolCall("ask", map[string]any{"questions": []map[string]any{{
			"header":   "Choice",
			"question": "Pick one",
			"options":  []map[string]any{{"label": "A"}, {"label": "B"}},
		}}}), fake.Done()},
		{fake.TextDelta("done"), fake.Done()},
	}}
	a := newTestAgent(t, p, reg, nil, execmode.ModeWork)
	if _, err := a.Run(context.Background(), Request{Prompt: "choose", Turn: 1}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	last := lastMessage(t, p.requests[1].Messages)
	block := last.Content[0]
	if block.IsError || !strings.Contains(block.Output, "No interactive ask UI is available") {
		t.Fatalf("tool result = %#v, want non-blocking fallback", block)
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
	a := newTestAgent(t, p, reg, perm, execmode.ModeWork)

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
	writer := &recordingRollout{}
	p := &scriptProvider{scripts: []fake.Script{
		{fake.ToolCall("preview_exec", map[string]any{"value": "x"}), fake.Done()},
		{fake.TextDelta("done"), fake.Done()},
	}}
	var events []Event
	a, err := New(Options{
		Provider:   p,
		Tools:      reg,
		Permission: perm,
		Rollout:    writer,
		Model:      "fake/model",
		Mode:       execmode.ModeWork,
		Events: func(event Event) {
			events = append(events, event)
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := a.Run(context.Background(), Request{SessionID: "sess_perm", Prompt: "call preview tool", Turn: 1}); err != nil {
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
	writer.mu.Lock()
	defer writer.mu.Unlock()
	var persisted rollout.ActivityPayload
	var persistedTurn int
	for _, event := range writer.events {
		if event.Type != rollout.TypeActivity {
			continue
		}
		payload, ok, err := rollout.ActivityFromEvent(event)
		if err != nil {
			t.Fatalf("ActivityFromEvent: %v", err)
		}
		if !ok || payload.ActivityKind != string(ActivityPermission) {
			continue
		}
		persisted = payload
		persistedTurn = event.Turn
		break
	}
	if persisted.ActivityKind != string(ActivityPermission) || persistedTurn != 1 {
		t.Fatalf("persisted permission activity = %#v turn=%d, want turn 1", persisted, persistedTurn)
	}
	if persisted.ToolName != "preview_exec" || persisted.Source != string(permission.SourceHuman) || persisted.Decision != string(permission.DecisionAllow) || !persisted.Allowed {
		t.Fatalf("persisted permission activity = %#v", persisted)
	}
}

func TestAgentEmitsApprovalAgentDecisionOnce(t *testing.T) {
	reg := tool.New()
	if err := reg.Register(&previewExecTool{}); err != nil {
		t.Fatalf("register tool: %v", err)
	}
	asker := &recordingAsker{decision: permission.DecisionDeny}
	perm, err := permission.NewManager(permission.Options{
		Asker:         asker,
		ApprovalAgent: approvalAgent{result: approval.Result{Decision: approval.DecisionAllow, Reason: "safe read-only command"}},
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
		Mode:       execmode.ModeAuto,
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
		Asker:         asker,
		ApprovalAgent: approvalAgent{result: approval.Result{Decision: approval.DecisionUnsure, Reason: "needs user context"}},
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
		Mode:       execmode.ModeAuto,
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

func TestAgentReadsModeAtToolGateTime(t *testing.T) {
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
	mode := execmode.ModePlan
	a, err := New(Options{
		Provider:   p,
		Tools:      reg,
		Permission: perm,
		Model:      "fake/model",
		Mode:       execmode.ModeWork,
		ModeFunc: func() execmode.Mode {
			return mode
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := a.Run(context.Background(), Request{Prompt: "call preview tool", Turn: 1}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if asker.calls != 0 {
		t.Fatalf("asker calls = %d, want 0", asker.calls)
	}
	last := lastMessage(t, p.requests[1].Messages)
	block := last.Content[0]
	if !block.IsError || !strings.Contains(block.Output, "not available in plan mode") {
		t.Fatalf("tool result block = %#v, want mode denial", block)
	}
}

func TestToolActivitySummaryRedactsSecretsAndTruncates(t *testing.T) {
	summary := SummarizeToolInput("bash", json.RawMessage(`{"command":"curl -H 'Authorization: Bearer secret-token' https://example.test\nsecond line","cwd":"/tmp"}`))
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

func TestToolActivitySummaryTaskAndUnknownTools(t *testing.T) {
	summary := SummarizeToolInput("task", json.RawMessage(`{"prompt":"Research providers\nwith details","max_turns":20}`))
	if !strings.Contains(summary, "prompt=Research providers") || !strings.Contains(summary, "max_turns=20") {
		t.Fatalf("task summary = %q, want prompt and max_turns", summary)
	}
	if strings.Contains(summary, "\n") {
		t.Fatalf("task summary should be single-line: %q", summary)
	}

	unknown := SummarizeToolInput("mcp_custom", json.RawMessage(`{"query":"providers","limit":5}`))
	if !strings.Contains(unknown, "query=providers") || !strings.Contains(unknown, "limit=5") {
		t.Fatalf("unknown tool summary = %q, want useful fields", unknown)
	}
}

func TestToolInputDetailPreservesFullShellCommand(t *testing.T) {
	input, err := json.Marshal(map[string]any{
		"command":    "printf 'first line'\nprintf 'second line'\n",
		"cwd":        "internal/agent",
		"timeout_ms": 1500,
	})
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}

	summary := SummarizeToolInput("bash", input)
	if strings.Contains(summary, "second line") {
		t.Fatalf("summary should stay single-line and compact: %q", summary)
	}

	detail := ToolInputDetail("bash", input)
	for _, want := range []string{"command:\nprintf 'first line'\nprintf 'second line'", "cwd: internal/agent", "timeout_ms: 1500"} {
		if !strings.Contains(detail, want) {
			t.Fatalf("detail missing %q:\n%s", want, detail)
		}
	}
}

func TestToolInputDetailRedactsSensitiveCommand(t *testing.T) {
	detail := ToolInputDetail("bash", json.RawMessage(`{"command":"curl -H 'Authorization: Bearer secret-token' https://example.test"}`))
	if strings.Contains(detail, "secret-token") || strings.Contains(detail, "Authorization") {
		t.Fatalf("detail leaked secret:\n%s", detail)
	}
	if !strings.Contains(detail, "command:\n[redacted]") {
		t.Fatalf("detail = %q, want redacted command block", detail)
	}
}

func TestToolActivitySummaryMultiEdit(t *testing.T) {
	summary := SummarizeToolInput("multiedit", json.RawMessage(`{"edits":[{"path":"a.go","old":"x","new":"y"},{"path":"a.go","old":"y","new":"z"},{"path":"b.go","old":"x","new":"y"}]}`))
	if !strings.Contains(summary, "edits=3") || !strings.Contains(summary, "files=2") {
		t.Fatalf("multiedit summary = %q, want edits and files counts", summary)
	}
}

func TestToolActivitySummaryApplyPatch(t *testing.T) {
	input := json.RawMessage(`{"patch":"*** Begin Patch\n*** Update File: a.go\n@@\n-old\n+new\n*** End Patch"}`)
	summary := SummarizeToolInput("apply_patch", input)
	if !strings.Contains(summary, "patch=") || !strings.Contains(summary, "bytes") {
		t.Fatalf("apply_patch summary = %q", summary)
	}
	detail := ToolInputDetail("apply_patch", input)
	if !strings.Contains(detail, "patch:\n*** Begin Patch") || !strings.Contains(detail, "+new") {
		t.Fatalf("apply_patch detail = %q", detail)
	}
}

func TestToolActivitySummaryTodoUpdateOmitsZeroItemIndex(t *testing.T) {
	summary := SummarizeToolInput("todo_update", json.RawMessage(`{"id":"patch","item_index":0,"status":"in_progress"}`))
	if strings.Contains(summary, "item=0") {
		t.Fatalf("todo_update summary leaked zero item index: %q", summary)
	}
	for _, want := range []string{"id=patch", "status=in_progress"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("todo_update summary = %q, want %s", summary, want)
		}
	}

	byIndex := SummarizeToolInput("todo_update", json.RawMessage(`{"item_index":2,"status":"completed"}`))
	for _, want := range []string{"item=2", "status=completed"} {
		if !strings.Contains(byIndex, want) {
			t.Fatalf("todo_update by-index summary = %q, want %s", byIndex, want)
		}
	}
}

func TestToolResultDetailUsesUnifiedDiff(t *testing.T) {
	detail := toolResultDetail(tool.Result{
		Files: []tool.FileChange{{
			Path:        "write.md",
			Kind:        tool.KindCreate,
			UnifiedDiff: "--- write.md\n+++ write.md\n@@\n+hello\n",
		}},
	})
	if !strings.Contains(detail, "create write.md") || !strings.Contains(detail, "+hello") {
		t.Fatalf("detail = %q, want file summary and diff", detail)
	}
}

func TestToolActivityResultKeepsPlainToolDetail(t *testing.T) {
	summary, detail := ToolActivityResult("read", "path=README.md", tool.Result{Content: "1\tfirst\n2\tsecond"})
	if summary != "path=README.md" {
		t.Fatalf("summary = %q, want input summary", summary)
	}
	if detail != "1\tfirst\n2\tsecond" {
		t.Fatalf("detail = %q, want full tool content", detail)
	}
}

func TestToolActivityResultFormatsShellDetail(t *testing.T) {
	content := "<shell_metadata>\nexit_code=0\nduration_ms=12\n</shell_metadata>\n--- stdout ---\nok\n--- stderr ---\nwarn\n"
	_, detail := ToolActivityResult("bash", "cmd=test", tool.Result{Content: content})
	if strings.Contains(detail, "<shell_metadata>") || strings.Contains(detail, "</shell_metadata>") {
		t.Fatalf("detail still contains shell metadata tags: %q", detail)
	}
	for _, want := range []string{"exit_code=0", "duration_ms=12", "--- stdout ---", "ok", "--- stderr ---", "warn"} {
		if !strings.Contains(detail, want) {
			t.Fatalf("detail missing %q: %q", want, detail)
		}
	}
}

func TestToolActivityResultWithInputIncludesShellCommand(t *testing.T) {
	input := json.RawMessage(`{"command":"go test ./...\nprintf 'done'","cwd":"internal/agent"}`)
	content := "<shell_metadata>\nexit_code=0\nduration_ms=12\n</shell_metadata>\n--- stdout ---\nok\n--- stderr ---\n"
	summary, detail := ToolActivityResultWithInput("bash", input, tool.Result{Content: content})
	if summary != "cmd=go test ./..., cwd=internal/agent" {
		t.Fatalf("summary = %q, want compact first-line command", summary)
	}
	for _, want := range []string{"command:\ngo test ./...\nprintf 'done'", "cwd: internal/agent", "exit_code=0", "--- stdout ---", "ok"} {
		if !strings.Contains(detail, want) {
			t.Fatalf("detail missing %q:\n%s", want, detail)
		}
	}
}

func TestToolActivityResultUsesContentWhenFilesHaveNoDiff(t *testing.T) {
	content := "plan_id=plan-1\npath=/home/user/.local/state/ub/plans/abc123/plan-1.md\n\n# Plan\n\n- [ ] inspect"
	summary, detail := ToolActivityResult("plan_write", "title=Plan", tool.Result{
		Content: content,
		Files:   []tool.FileChange{{Path: "/home/user/.local/state/ub/plans/abc123/plan-1.md", Kind: tool.KindCreate}},
	})
	if summary != "create /home/user/.local/state/ub/plans/abc123/plan-1.md" {
		t.Fatalf("summary = %q, want file summary", summary)
	}
	if detail != content {
		t.Fatalf("detail = %q, want plan content", detail)
	}
}

func TestToolActivityDetailTruncationIsVisible(t *testing.T) {
	_, detail := ToolActivityResult("read", "path=large.log", tool.Result{Content: strings.Repeat("line\n", maxToolActivityDetailRunes)})
	if !strings.HasPrefix(detail, "[activity detail truncated:") {
		t.Fatalf("detail missing truncation notice:\n%s", detail)
	}
}

func TestToolActivityDetailTruncationPreservesToolResultFooter(t *testing.T) {
	footer := "... [tool result truncated: original_bytes=999999]\nfull_output_path=/tmp/ub-full-output.txt\nUse the read tool with this absolute path plus offset/limit to inspect more."
	_, detail := ToolActivityResult("task", "prompt=large research", tool.Result{Content: strings.Repeat("line\n", maxToolActivityDetailRunes) + footer})
	if !strings.HasPrefix(detail, "[activity detail truncated:") {
		t.Fatalf("detail should start with truncation notice:\n%s", detail)
	}
	for _, want := range []string{"activity detail truncated", "tool result footer preserved", "... [tool result truncated:", "full_output_path=/tmp/ub-full-output.txt"} {
		if !strings.Contains(detail, want) {
			t.Fatalf("detail missing %q:\n%s", want, detail)
		}
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
		Mode:       execmode.ModeWork,
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
	var events []Event
	a, err := New(Options{
		Provider:        main,
		SummaryProvider: summary,
		SummaryModel:    "small",
		Tools:           reg,
		Permission:      perm,
		Rollout:         writer,
		Model:           "fake/model",
		Mode:            execmode.ModeWork,
		Context: config.ContextConfig{
			TriggerRatio:    0.01,
			KeepRecentTurns: 3,
		},
		Events: func(event Event) {
			events = append(events, event)
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
	summaryPrompt := summary.requests[0].Messages[0].Text()
	for _, want := range []string{"## User Intent", "## Errors & Fixes", "## User Feedback", "## Next Steps"} {
		if !strings.Contains(summaryPrompt, want) {
			t.Fatalf("structured summary prompt missing %q:\n%s", want, summaryPrompt)
		}
	}
	if len(main.requests) != 1 {
		t.Fatalf("main requests = %d, want 1", len(main.requests))
	}
	got := main.requests[0].Messages
	if !containsText(got, "summary of early work") {
		t.Fatalf("main request missing summary: %#v", got)
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
	if !containsText(res.Messages, "user 1") || !containsText(res.Messages, "user 2") || containsText(res.Messages, "summary of early work") {
		t.Fatalf("result transcript = %#v, want original messages without summary injection", res.Messages)
	}
	if !containsText(res.ContextMessages, "summary of early work") || containsText(res.ContextMessages, "user 1") || containsText(res.ContextMessages, "user 2") {
		t.Fatalf("result context messages = %#v, want compacted provider context", res.ContextMessages)
	}
	// The compaction lifecycle emits a running notice when it starts and a
	// done notice when it finishes. Both must carry NoticeCompacting so the
	// TUI can key them together (otherwise the running "compacting" state
	// never clears). The done summary historically said "summarized ...",
	// which broke keying — pin the "compacted" wording here.
	if !hasNoticeActivity(events, NoticeCompacting, "running", "compacting context") {
		t.Fatalf("events missing compact running notice: %#v", events)
	}
	if !hasNoticeActivity(events, NoticeCompacting, "done", "compacted") {
		t.Fatalf("events missing compact done notice with compacted wording: %#v", events)
	}
}

func TestAgentUsesContextHistoryWithoutShrinkingTranscript(t *testing.T) {
	reg := tool.New()
	perm := newPermissionManager(t, nil)
	main := &scriptProvider{
		caps: provider.Caps{MaxContextTokens: 1_000_000},
		scripts: []fake.Script{
			{fake.TextDelta("resume answer"), fake.Done()},
		},
	}
	a, err := New(Options{
		Provider:   main,
		Tools:      reg,
		Permission: perm,
		Model:      "fake/model",
		Mode:       execmode.ModeWork,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	history := turnHistory(5)
	contextHistory := []message.Message{
		rollout.SummaryMessage("resume summary"),
		message.Text(message.RoleUser, "user 5"),
		message.Text(message.RoleAssistant, "assistant 5"),
	}
	res, err := a.Run(context.Background(), Request{
		SessionID:      "sess_resume_ctx",
		Turn:           6,
		History:        history,
		ContextHistory: contextHistory,
		Prompt:         "current prompt",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(main.requests) != 1 {
		t.Fatalf("main requests = %d, want 1", len(main.requests))
	}
	gotRequest := main.requests[0].Messages
	if !containsText(gotRequest, "resume summary") || containsText(gotRequest, "user 1") || !containsText(gotRequest, "current prompt") {
		t.Fatalf("provider request messages = %#v, want compacted context plus current prompt", gotRequest)
	}
	if !containsText(res.Messages, "user 1") || containsText(res.Messages, "resume summary") {
		t.Fatalf("result transcript = %#v, want full history without summary message", res.Messages)
	}
	if !containsText(res.ContextMessages, "resume summary") || containsText(res.ContextMessages, "user 1") {
		t.Fatalf("result context messages = %#v, want compacted context retained", res.ContextMessages)
	}
}

func TestAgentCompactsAndRetriesAfterContextOverflowChatError(t *testing.T) {
	reg := tool.New()
	perm := newPermissionManager(t, nil)
	main := &scriptProvider{
		caps:       provider.Caps{MaxContextTokens: 1_000_000},
		chatErrors: []error{errors.New("context_length_exceeded: maximum context length exceeded")},
		scripts: []fake.Script{
			{fake.TextDelta("final after retry"), fake.Done()},
		},
	}
	summary := &scriptProvider{scripts: []fake.Script{
		{fake.TextDelta("summary after overflow"), fake.Done()},
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
		Mode:            execmode.ModeWork,
		Context:         config.ContextConfig{TriggerRatio: 0.99, KeepRecentTurns: 3},
		Events: func(event Event) {
			events = append(events, event)
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	res, err := a.Run(context.Background(), Request{SessionID: "sess_overflow", Prompt: "current prompt", Turn: 7, History: turnHistory(5)})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Text != "final after retry" {
		t.Fatalf("text = %q, want final after retry", res.Text)
	}
	if len(main.requests) != 2 {
		t.Fatalf("main requests = %d, want initial + retry", len(main.requests))
	}
	if len(summary.requests) != 1 {
		t.Fatalf("summary requests = %d, want 1", len(summary.requests))
	}
	retryMessages := main.requests[1].Messages
	if !containsText(retryMessages, "summary after overflow") {
		t.Fatalf("retry request missing summary: %#v", retryMessages)
	}
	if containsText(retryMessages, "user 1") || containsText(retryMessages, "user 2") {
		t.Fatalf("retry request kept compacted messages: %#v", retryMessages)
	}
	if !hasEventType(writer.events, rollout.TypeSummary) || hasEventType(writer.events, rollout.TypeError) {
		t.Fatalf("rollout events = %#v, want summary and no error", writer.events)
	}
	if !hasActivity(events, ActivityNotice, "compacted") {
		t.Fatalf("events missing compact retry notice: %#v", events)
	}
	if !hasContextResetEvent(events) {
		t.Fatalf("events missing context reset after recovery: %#v", events)
	}
}

func TestAgentCompactsAndRetriesAfterContextOverflowStreamError(t *testing.T) {
	reg := tool.New()
	perm := newPermissionManager(t, nil)
	main := &scriptProvider{
		caps: provider.Caps{MaxContextTokens: 1_000_000},
		scripts: []fake.Script{
			{fake.Usage(9000, 0), fake.Error("This model's maximum context length is 8192 tokens. Reduce the prompt.")},
			{fake.TextDelta("final after stream retry"), fake.Done()},
		},
	}
	summary := &scriptProvider{scripts: []fake.Script{
		{fake.TextDelta("stream overflow summary"), fake.Done()},
		{fake.TextDelta("stream overflow summary after learned limit"), fake.Done()},
	}}
	window, err := contextwindow.New(contextwindow.Options{ProviderTokens: 1_000_000})
	if err != nil {
		t.Fatalf("new context window resolver: %v", err)
	}
	var events []Event
	a, err := New(Options{
		Provider:        main,
		SummaryProvider: summary,
		Tools:           reg,
		Permission:      perm,
		Model:           "fake/model",
		Mode:            execmode.ModeWork,
		ContextWindow:   window,
		Context:         config.ContextConfig{TriggerRatio: 0.99, KeepRecentTurns: 3},
		Events: func(event Event) {
			events = append(events, event)
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	res, err := a.Run(context.Background(), Request{SessionID: "sess_stream_overflow", Prompt: "current prompt", Turn: 7, History: turnHistory(5)})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Text != "final after stream retry" {
		t.Fatalf("text = %q, want final after stream retry", res.Text)
	}
	if len(main.requests) != 2 {
		t.Fatalf("main requests = %d, want initial + retry", len(main.requests))
	}
	if len(summary.requests) != 2 {
		t.Fatalf("summary requests = %d, want recovery compact plus learned-limit preflight compact", len(summary.requests))
	}
	resolved := window.Resolve()
	if resolved.MaxTokens != 8192 || resolved.Source != contextwindow.SourceLearnedOverflow || resolved.Confidence != contextwindow.ConfidenceHigh {
		t.Fatalf("resolved context window = %#v", resolved)
	}
	contextEvent, ok := firstContextEvent(events)
	if !ok || contextEvent.ContextMaxTokens != 1_000_000 || contextEvent.ContextMaxSource != string(contextwindow.SourceProviderCaps) {
		t.Fatalf("initial context event = %#v, ok=%v", contextEvent, ok)
	}
	foundLearned := false
	for _, event := range events {
		if event.Type == EventContext && event.ContextMaxTokens == 8192 && event.ContextMaxSource == string(contextwindow.SourceLearnedOverflow) && event.ContextConfidence == string(contextwindow.ConfidenceHigh) {
			foundLearned = true
			break
		}
	}
	if !foundLearned {
		t.Fatalf("events missing learned context window metadata: %#v", events)
	}
}

func TestAgentContextOverflowRecoveryRetriesOnlyOnce(t *testing.T) {
	reg := tool.New()
	perm := newPermissionManager(t, nil)
	overflowErr := errors.New("context_length_exceeded: maximum context length exceeded")
	main := &scriptProvider{
		caps:       provider.Caps{MaxContextTokens: 1_000_000},
		chatErrors: []error{overflowErr, overflowErr},
	}
	summary := &scriptProvider{scripts: []fake.Script{
		{fake.TextDelta("one retry summary"), fake.Done()},
	}}
	writer := &recordingRollout{}
	a, err := New(Options{
		Provider:        main,
		SummaryProvider: summary,
		Tools:           reg,
		Permission:      perm,
		Rollout:         writer,
		Model:           "fake/model",
		Mode:            execmode.ModeWork,
		Context:         config.ContextConfig{TriggerRatio: 0.99, KeepRecentTurns: 3},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = a.Run(context.Background(), Request{SessionID: "sess_overflow_twice", Prompt: "current prompt", Turn: 7, History: turnHistory(5)})
	if err == nil || !strings.Contains(err.Error(), "context_length_exceeded") {
		t.Fatalf("Run error = %v, want second provider overflow error", err)
	}
	if len(main.requests) != 2 {
		t.Fatalf("main requests = %d, want one retry", len(main.requests))
	}
	if len(summary.requests) != 1 {
		t.Fatalf("summary requests = %d, want 1", len(summary.requests))
	}
	if !hasEventType(writer.events, rollout.TypeSummary) || !hasEventType(writer.events, rollout.TypeError) {
		t.Fatalf("rollout events = %#v, want summary then final error", writer.events)
	}
}

func TestAgentContextOverflowRecoveryChunksSummaryInput(t *testing.T) {
	reg := tool.New()
	perm := newPermissionManager(t, nil)
	main := &scriptProvider{
		caps:       provider.Caps{MaxContextTokens: 1_000_000},
		chatErrors: []error{errors.New("context_length_exceeded: maximum context length exceeded")},
		scripts: []fake.Script{
			{fake.TextDelta("final after bounded summary"), fake.Done()},
		},
	}
	summaryScripts := make([]fake.Script, 40)
	for i := range summaryScripts {
		summaryScripts[i] = fake.Script{fake.TextDelta("bounded summary"), fake.Done()}
	}
	summary := &scriptProvider{
		caps:    provider.Caps{MaxContextTokens: 2500},
		scripts: summaryScripts,
	}
	a, err := New(Options{
		Provider:        main,
		SummaryProvider: summary,
		Tools:           reg,
		Permission:      perm,
		Model:           "fake/model",
		SummaryModel:    "fake/model",
		Mode:            execmode.ModeWork,
		Context:         config.ContextConfig{TriggerRatio: 0.99, KeepRecentTurns: 3},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	res, err := a.Run(context.Background(), Request{SessionID: "sess_overflow_chunked", Prompt: "current prompt", Turn: 7, History: hugeTurnHistory(40, 300)})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Text != "final after bounded summary" {
		t.Fatalf("text = %q, want final after bounded summary", res.Text)
	}
	if len(summary.requests) <= 1 {
		t.Fatalf("summary requests = %d, want chunked summary requests", len(summary.requests))
	}
	budget := a.summaryInputBudget(summary, "fake/model")
	for i, req := range summary.requests {
		estimated := contextmgr.Estimate(req.Messages, "fake/model")
		if estimated > budget {
			t.Fatalf("summary request %d estimate = %d, budget = %d", i, estimated, budget)
		}
	}
}

func TestSplitSummaryMessageChunksRejectsOversizedTurn(t *testing.T) {
	budget := summaryPromptEstimate(summaryPromptTemplate, "", "fake/model") + 1200
	_, err := splitSummaryMessageChunks(summaryPromptTemplate, hugeTurnHistory(1, 10000), "fake/model", budget)
	if err == nil || !strings.Contains(err.Error(), "single user turn exceeds summary input budget") {
		t.Fatalf("splitSummaryMessageChunks error = %v, want oversized turn error", err)
	}
}

func TestSummaryInputBudgetDoesNotExceedModelContext(t *testing.T) {
	a := &Agent{}
	p := &scriptProvider{caps: provider.Caps{MaxContextTokens: 1000}}

	budget := a.summaryInputBudget(p, "fake/model")
	if budget <= 0 || budget > 1000 {
		t.Fatalf("summaryInputBudget = %d, want within model context 1..1000", budget)
	}
}

func TestSummaryChunkingRejectsBudgetBelowPromptOverhead(t *testing.T) {
	emptyPrompt := summaryPromptEstimate(summaryPromptTemplate, "", "fake/model")

	_, err := splitSummaryMessageChunks(summaryPromptTemplate, turnHistory(1), "fake/model", emptyPrompt)
	if err == nil || !strings.Contains(err.Error(), "cannot fit summary prompt overhead") {
		t.Fatalf("splitSummaryMessageChunks error = %v, want prompt overhead error", err)
	}

	_, err = splitSummaryTextUnits(summaryPromptTemplate, []string{"summary"}, "fake/model", emptyPrompt)
	if err == nil || !strings.Contains(err.Error(), "cannot fit summary prompt overhead") {
		t.Fatalf("splitSummaryTextUnits error = %v, want prompt overhead error", err)
	}
}

func TestIsContextOverflowError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "openai code", err: errors.New("context_length_exceeded"), want: true},
		{name: "maximum context", err: errors.New("maximum context length is 8192 tokens"), want: true},
		{name: "prompt too long", err: errors.New("prompt is too long: 202000 tokens > 200000 maximum"), want: true},
		{name: "input tokens", err: errors.New("too many input tokens for this model"), want: true},
		{name: "output limit", err: errors.New("tool call arguments truncated mid-stream (likely hit max_output_tokens before tool call completed)"), want: false},
		{name: "rate limit", err: errors.New("rate limit exceeded"), want: false},
		{name: "deadline", err: context.DeadlineExceeded, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isContextOverflowError(tt.err); got != tt.want {
				t.Fatalf("isContextOverflowError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestAgentSummaryPromptCanUseShortStyle(t *testing.T) {
	reg := tool.New()
	perm := newPermissionManager(t, nil)
	main := &scriptProvider{
		caps: provider.Caps{MaxContextTokens: 20},
		scripts: []fake.Script{
			{fake.TextDelta("final"), fake.Done()},
		},
	}
	summary := &scriptProvider{scripts: []fake.Script{
		{fake.TextDelta("short summary"), fake.Done()},
	}}
	a, err := New(Options{
		Provider:        main,
		SummaryProvider: summary,
		Tools:           reg,
		Permission:      perm,
		Model:           "fake/model",
		Mode:            execmode.ModeWork,
		Context: config.ContextConfig{
			TriggerRatio:    0.01,
			KeepRecentTurns: 3,
		},
		Prompt: config.PromptConfig{CompactStyle: config.CompactStyleShort},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := a.Run(context.Background(), Request{SessionID: "sess_sum_short", Prompt: "current prompt", Turn: 7, History: turnHistory(5)}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(summary.requests) != 1 {
		t.Fatalf("summary requests = %d, want 1", len(summary.requests))
	}
	prompt := summary.requests[0].Messages[0].Text()
	if !strings.Contains(prompt, "## Goal") || strings.Contains(prompt, "## User Intent") {
		t.Fatalf("short summary prompt =\n%s", prompt)
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
		Mode:            execmode.ModeWork,
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
		t.Fatalf("auto memory requests = %#v", summary.requests)
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
	if !ok || contextEvent.ContextMaxTokens != 1000 || contextEvent.ContextUsedTokens <= 0 || contextEvent.ContextRatio <= 0 || !contextEvent.ContextReset || contextEvent.ContextMaxSource != string(contextwindow.SourceProviderCaps) || contextEvent.ContextConfidence != string(contextwindow.ConfidenceMedium) {
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
		Provider:           main,
		AutoMemoryProvider: summary,
		Tools:              reg,
		Permission:         perm,
		Rollout:            writer,
		Model:              "fake/model",
		Mode:               execmode.ModeWork,
		Context:            config.ContextConfig{KeepRecentTurns: 3},
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
		t.Fatalf("auto memory requests = %d, want 0", len(summary.requests))
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
		Mode:            execmode.ModeWork,
		Context:         config.ContextConfig{TriggerRatio: 0.8, KeepRecentTurns: 3},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := a.Run(context.Background(), Request{Prompt: "short", Turn: 1, History: turnHistory(5)}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(summary.requests) != 0 {
		t.Fatalf("auto memory requests = %d, want 0", len(summary.requests))
	}
	if got := main.requests[0].Messages; containsText(got, "summary should not run") {
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
		Mode:             execmode.ModeWork,
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
	if got := main.requests[0].Messages; containsText(got, "summary should not run") {
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
		Mode:            execmode.ModeWork,
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
		{fake.Usage(before*1000, 1), fake.TextDelta("ok"), fake.Done()},
	}}
	window, err := contextwindow.New(contextwindow.Options{ProviderTokens: 8192})
	if err != nil {
		t.Fatalf("new context window resolver: %v", err)
	}
	a, err := New(Options{
		Provider:      main,
		Tools:         reg,
		Permission:    perm,
		Model:         model,
		Mode:          execmode.ModeWork,
		ContextWindow: window,
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
	resolved := window.Resolve()
	if resolved.MaxTokens < before*1000 || resolved.Source != contextwindow.SourceLearnedUsage || resolved.Confidence != contextwindow.ConfidenceLow {
		t.Fatalf("resolved context window after usage = %#v", resolved)
	}
}

type recordingRollout struct {
	mu     sync.Mutex
	events []rollout.Event
}

func (w *recordingRollout) Append(_ context.Context, event rollout.Event) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.events = append(w.events, event)
	return nil
}

func (w *recordingRollout) Close() error { return nil }

func turnHistory(turns int) []message.Message {
	out := make([]message.Message, 0, turns*2)
	for i := 1; i <= turns; i++ {
		out = append(
			out,
			message.Text(message.RoleUser, fmt.Sprintf("user %d", i)),
			message.Text(message.RoleAssistant, fmt.Sprintf("assistant %d", i)),
		)
	}
	return out
}

func hugeTurnHistory(turns int, charsPerMessage int) []message.Message {
	out := make([]message.Message, 0, turns*2)
	body := strings.Repeat("x", charsPerMessage)
	for i := 1; i <= turns; i++ {
		out = append(
			out,
			message.Text(message.RoleUser, fmt.Sprintf("user %d %s", i, body)),
			message.Text(message.RoleAssistant, fmt.Sprintf("assistant %d %s", i, body)),
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

// hasNoticeActivity reports whether events contain an ActivityNotice with the
// given Notice kind, Status, and a Summary containing text. Used to pin the
// compaction lifecycle notices that the TUI keys on.
func hasNoticeActivity(events []Event, notice NoticeKind, status, text string) bool {
	for _, event := range events {
		if event.Type != EventActivity || event.ActivityKind != ActivityNotice {
			continue
		}
		if event.Notice != notice {
			continue
		}
		if status != "" && event.Status != status {
			continue
		}
		if text != "" && !strings.Contains(event.Summary, text) {
			continue
		}
		return true
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

func hasContextResetEvent(events []Event) bool {
	for _, event := range events {
		if event.Type == EventContext && event.ContextReset {
			return true
		}
	}
	return false
}

func newTestAgent(t *testing.T, p provider.Provider, reg *tool.Registry, perm *permission.Manager, mode execmode.Mode) *Agent {
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
		Asker: asker,
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

func toolNamesContain(tools []provider.ToolDefinition, name string) bool {
	for _, tl := range tools {
		if tl.Name == name {
			return true
		}
	}
	return false
}

func containsToolDescription(tools []provider.ToolDefinition, name, text string) bool {
	for _, tl := range tools {
		if tl.Name == name && strings.Contains(tl.Description, text) {
			return true
		}
	}
	return false
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

type recordingUserAsker struct {
	response AskResponse
	calls    int
	requests []AskRequest
}

func (a *recordingUserAsker) AskUser(_ context.Context, req AskRequest) (AskResponse, error) {
	a.calls++
	a.requests = append(a.requests, req)
	return a.response, nil
}

type recordingPlanModeController struct {
	response PlanModeResponse
	confirm  func(PlanModeRequest) PlanModeResponse
	calls    int
	requests []PlanModeRequest
}

func (c *recordingPlanModeController) ConfirmPlanMode(_ context.Context, req PlanModeRequest) (PlanModeResponse, error) {
	c.calls++
	c.requests = append(c.requests, req)
	if c.confirm != nil {
		return c.confirm(req), nil
	}
	return c.response, nil
}

type approvalAgent struct {
	result approval.Result
	err    error
}

func (a approvalAgent) ReviewCommand(context.Context, approval.Request) (approval.Result, error) {
	return a.result, a.err
}

type namedSafeTool struct {
	name         string
	executeCalls int
	schema       *jsonschema.Schema
}

func (t *namedSafeTool) Name() string        { return t.name }
func (t *namedSafeTool) Description() string { return "named safe test tool." }
func (t *namedSafeTool) Schema() *jsonschema.Schema {
	if t.schema == nil {
		t.schema = jsonschema.Reflect(&struct{}{})
	}
	return t.schema
}
func (t *namedSafeTool) Risk() tool.Risk { return tool.RiskSafe }
func (t *namedSafeTool) Execute(context.Context, json.RawMessage) (tool.Result, error) {
	t.executeCalls++
	return tool.Result{Content: "ok"}, nil
}

type namedRiskTool struct {
	name   string
	risk   tool.Risk
	schema *jsonschema.Schema
}

func (t *namedRiskTool) Name() string        { return t.name }
func (t *namedRiskTool) Description() string { return "named risk test tool." }
func (t *namedRiskTool) Schema() *jsonschema.Schema {
	if t.schema == nil {
		t.schema = jsonschema.Reflect(&struct{}{})
	}
	return t.schema
}
func (t *namedRiskTool) Risk() tool.Risk { return t.risk }
func (t *namedRiskTool) Execute(context.Context, json.RawMessage) (tool.Result, error) {
	return tool.Result{Content: "ok"}, nil
}

type failingCheckTool struct {
	schema *jsonschema.Schema
}

func (t *failingCheckTool) Name() string { return "test_check" }
func (t *failingCheckTool) Description() string {
	return "Run a deterministic failing validation check."
}

func (t *failingCheckTool) Schema() *jsonschema.Schema {
	if t.schema == nil {
		t.schema = jsonschema.Reflect(&struct{}{})
	}
	return t.schema
}
func (t *failingCheckTool) Risk() tool.Risk { return tool.RiskSafe }
func (t *failingCheckTool) Execute(context.Context, json.RawMessage) (tool.Result, error) {
	return tool.Result{
		Content: "<shell_metadata>\nexit_code=1\n</shell_metadata>\n--- stdout ---\n\n--- stderr ---\nfailed\n",
		IsError: true,
	}, nil
}

type modeFlipTool struct {
	set    func()
	schema *jsonschema.Schema
}

func (t *modeFlipTool) Name() string        { return "flip_mode" }
func (t *modeFlipTool) Description() string { return "flip test execmode." }
func (t *modeFlipTool) Schema() *jsonschema.Schema {
	if t.schema == nil {
		t.schema = jsonschema.Reflect(&struct{}{})
	}
	return t.schema
}
func (t *modeFlipTool) Risk() tool.Risk { return tool.RiskSafe }
func (t *modeFlipTool) Execute(context.Context, json.RawMessage) (tool.Result, error) {
	if t.set != nil {
		t.set()
	}
	return tool.Result{Content: "ok"}, nil
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

// recordSessionTool captures the session id present in the context that the
// agent passes into Execute, so a test can verify runTool injects it.
type recordSessionTool struct {
	got    string
	schema *jsonschema.Schema
}

func (t *recordSessionTool) Name() string        { return "recordsess" }
func (t *recordSessionTool) Description() string { return "Capture session id from ctx." }
func (t *recordSessionTool) Schema() *jsonschema.Schema {
	if t.schema == nil {
		t.schema = jsonschema.Reflect(&struct{}{})
	}
	return t.schema
}
func (t *recordSessionTool) Risk() tool.Risk { return tool.RiskSafe }
func (t *recordSessionTool) Execute(ctx context.Context, _ json.RawMessage) (tool.Result, error) {
	t.got = tool.SessionIDFromContext(ctx)
	return tool.Result{Content: "ok"}, nil
}

// streamingFakeTool exists only in tests to exercise the agent's
// StreamingTool dispatch path. It emits two stdout StreamEvents and a
// final Result that mirrors the concatenated stream.
type streamingFakeTool struct {
	schema *jsonschema.Schema
}

func (t *streamingFakeTool) Name() string        { return "streamer" }
func (t *streamingFakeTool) Description() string { return "emit two partial events." }
func (t *streamingFakeTool) Schema() *jsonschema.Schema {
	if t.schema == nil {
		t.schema = jsonschema.Reflect(&struct{}{})
	}
	return t.schema
}
func (t *streamingFakeTool) Risk() tool.Risk { return tool.RiskSafe }
func (t *streamingFakeTool) Execute(context.Context, json.RawMessage) (tool.Result, error) {
	return tool.Result{Content: "AB"}, nil
}

func (t *streamingFakeTool) ExecuteStream(_ context.Context, _ json.RawMessage, events chan<- tool.StreamEvent) (tool.Result, error) {
	events <- tool.StreamEvent{Kind: tool.StreamStdout, Data: "A"}
	events <- tool.StreamEvent{Kind: tool.StreamStdout, Data: "B"}
	return tool.Result{Content: "AB"}, nil
}

type streamingPanicTool struct {
	streamingFakeTool
}

func (t *streamingPanicTool) Name() string { return "panicstream" }
func (t *streamingPanicTool) ExecuteStream(context.Context, json.RawMessage, chan<- tool.StreamEvent) (tool.Result, error) {
	panic("boom")
}

func TestAgentForwardsStreamingToolPartialOutput(t *testing.T) {
	reg := tool.New()
	if err := reg.Register(&streamingFakeTool{}); err != nil {
		t.Fatalf("register: %v", err)
	}
	perm := newPermissionManager(t, nil)
	p := &scriptProvider{scripts: []fake.Script{
		{fake.ToolCall("streamer", map[string]any{}), fake.Done()},
		{fake.TextDelta("done"), fake.Done()},
	}}
	var partials []Event
	a, err := New(Options{
		Provider:   p,
		Tools:      reg,
		Permission: perm,
		Model:      "fake/model",
		Mode:       execmode.ModeWork,
		Events: func(e Event) {
			if e.Type == EventToolPartialOutput {
				partials = append(partials, e)
			}
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := a.Run(context.Background(), Request{Prompt: "go", Turn: 1}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(partials) != 2 {
		t.Fatalf("expected 2 partial events, got %d", len(partials))
	}
	if partials[0].Content != "A" || partials[1].Content != "B" {
		t.Fatalf("partial chunks = %q %q", partials[0].Content, partials[1].Content)
	}
	if partials[0].Status != "stdout" {
		t.Fatalf("status = %q, want stdout", partials[0].Status)
	}
}

func TestAgentStreamingToolPanicBecomesError(t *testing.T) {
	a := &Agent{}
	res, err := a.executeToolCall(context.Background(), &streamingPanicTool{}, toolCall{Name: "panicstream"})
	if err == nil || !strings.Contains(err.Error(), "streaming tool panicstream panic: boom") {
		t.Fatalf("error = %v", err)
	}
	if !res.IsError || !strings.Contains(res.Content, "panicstream panic") {
		t.Fatalf("result = %+v", res)
	}
}

func TestAgentDoesNotEmitPartialForPlainTool(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "x.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	reg := tool.New()
	if err := fs.Register(reg, root); err != nil {
		t.Fatalf("register: %v", err)
	}
	perm := newPermissionManager(t, nil)
	p := &scriptProvider{scripts: []fake.Script{
		{fake.ToolCall("read", map[string]any{"path": "x.txt"}), fake.Done()},
		{fake.TextDelta("done"), fake.Done()},
	}}
	var partials int
	a, err := New(Options{
		Provider:   p,
		Tools:      reg,
		Permission: perm,
		Model:      "fake/model",
		Mode:       execmode.ModeWork,
		Events: func(e Event) {
			if e.Type == EventToolPartialOutput {
				partials++
			}
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := a.Run(context.Background(), Request{Prompt: "go", Turn: 1}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if partials != 0 {
		t.Fatalf("non-streaming tool produced %d partial events, want 0", partials)
	}
}

// fakeHookRunner records every Run call and returns whatever the test
// pre-staged for each Kind.
type fakeHookRunner struct {
	calls     []hook.Event
	decisions map[hook.Kind]hook.Decision
}

func (f *fakeHookRunner) Run(_ context.Context, ev hook.Event) hook.Decision {
	f.calls = append(f.calls, ev)
	if f.decisions == nil {
		return hook.Decision{}
	}
	return f.decisions[ev.Kind]
}

func TestAgentFiresAllFourHookKinds(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	reg := tool.New()
	if err := fs.Register(reg, root); err != nil {
		t.Fatalf("register fs: %v", err)
	}
	runner := &fakeHookRunner{}
	perm := newPermissionManager(t, nil)
	p := &scriptProvider{scripts: []fake.Script{
		{fake.ToolCall("read", map[string]any{"path": "main.go"}), fake.Done()},
		{fake.TextDelta("done"), fake.Done()},
	}}
	a, err := New(Options{
		Provider:   p,
		Tools:      reg,
		Permission: perm,
		Model:      "fake/model",
		Mode:       execmode.ModeWork,
		Hooks:      runner,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := a.Run(context.Background(), Request{SessionID: "sess", Prompt: "go", Turn: 1}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	kinds := make([]hook.Kind, 0, len(runner.calls))
	for _, c := range runner.calls {
		kinds = append(kinds, c.Kind)
	}
	want := []hook.Kind{
		hook.KindPreUserTurn,
		hook.KindPreToolCall,
		hook.KindPostToolCall,
		hook.KindPostUserTurn,
	}
	if !reflect.DeepEqual(kinds, want) {
		t.Fatalf("hook kinds = %v, want %v", kinds, want)
	}
	// pre_tool_call must carry the tool name + args we observed.
	for _, c := range runner.calls {
		if c.Kind == hook.KindPreToolCall {
			if c.ToolName != "read" || c.ToolUseID == "" || len(c.ToolArgs) == 0 {
				t.Fatalf("pre_tool_call event = %#v", c)
			}
		}
		if c.Kind == hook.KindPostToolCall && c.Result == nil {
			t.Fatalf("post_tool_call missing Result")
		}
	}
}

func TestAgentPreToolCallHookCanBlockExecution(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	reg := tool.New()
	if err := fs.Register(reg, root); err != nil {
		t.Fatalf("register fs: %v", err)
	}
	runner := &fakeHookRunner{
		decisions: map[hook.Kind]hook.Decision{
			hook.KindPreToolCall: {Block: true, Reason: "policy denied"},
		},
	}
	perm := newPermissionManager(t, nil)
	p := &scriptProvider{scripts: []fake.Script{
		{fake.ToolCall("read", map[string]any{"path": "main.go"}), fake.Done()},
		{fake.TextDelta("done"), fake.Done()},
	}}
	a, err := New(Options{
		Provider:   p,
		Tools:      reg,
		Permission: perm,
		Model:      "fake/model",
		Mode:       execmode.ModeWork,
		Hooks:      runner,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := a.Run(context.Background(), Request{SessionID: "sess", Prompt: "go", Turn: 1}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(p.requests) < 2 {
		t.Fatalf("expected at least 2 provider calls, got %d", len(p.requests))
	}
	last := lastMessage(t, p.requests[1].Messages)
	if last.Role != message.RoleTool || len(last.Content) != 1 {
		t.Fatalf("expected tool_result message: %#v", last)
	}
	block := last.Content[0]
	if block.Type != message.BlockToolResult || !block.IsError || !strings.Contains(block.Output, "policy denied") {
		t.Fatalf("blocked tool_result = %#v", block)
	}
	// post_tool_call must still fire when pre_tool_call blocks? In this
	// implementation pre-block short-circuits *before* Execute and returns
	// early — so post_tool_call should NOT fire. Verify that.
	for _, c := range runner.calls {
		if c.Kind == hook.KindPostToolCall {
			t.Fatalf("post_tool_call should be skipped after pre-block, got: %#v", c)
		}
	}
}

// captureSubagentTool records whether the SubagentRunner was visible in
// the ctx that the agent passed into Execute.
type captureSubagentTool struct {
	gotRunner    bool
	gotDepth     int
	gotTurn      int
	gotToolUseID string
	schema       *jsonschema.Schema
}

func (t *captureSubagentTool) Name() string        { return "captureSubagent" }
func (t *captureSubagentTool) Description() string { return "capture ctx" }
func (t *captureSubagentTool) Schema() *jsonschema.Schema {
	if t.schema == nil {
		t.schema = jsonschema.Reflect(&struct{}{})
	}
	return t.schema
}
func (t *captureSubagentTool) Risk() tool.Risk { return tool.RiskSafe }
func (t *captureSubagentTool) Execute(ctx context.Context, _ json.RawMessage) (tool.Result, error) {
	t.gotRunner = tool.SubagentRunnerFromContext(ctx) != nil
	t.gotDepth = tool.SubagentDepthFromContext(ctx)
	t.gotTurn = tool.AgentTurnFromContext(ctx)
	t.gotToolUseID = tool.ToolUseIDFromContext(ctx)
	return tool.Result{Content: "ok"}, nil
}

type fakeAgentSubagentRunner struct{}

func (fakeAgentSubagentRunner) RunSubagent(_ context.Context, _ string, _ int) (string, error) {
	return "ok", nil
}

func TestAgent_InjectsSubagentRunnerIntoToolContext(t *testing.T) {
	cap := &captureSubagentTool{}
	reg := tool.New()
	if err := reg.Register(cap); err != nil {
		t.Fatalf("register: %v", err)
	}
	perm := newPermissionManager(t, nil)
	p := &scriptProvider{scripts: []fake.Script{
		{{Type: provider.EventToolCall, ToolUseID: "call_capture", ToolName: "captureSubagent", Input: json.RawMessage(`{}`)}, fake.Done()},
		{fake.TextDelta("done"), fake.Done()},
	}}
	a, err := New(Options{
		Provider:       p,
		Tools:          reg,
		Permission:     perm,
		Model:          "fake/model",
		Mode:           execmode.ModeWork,
		SubagentRunner: fakeAgentSubagentRunner{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := a.Run(context.Background(), Request{Prompt: "go", Turn: 4}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !cap.gotRunner {
		t.Fatalf("ctx did not carry SubagentRunner")
	}
	if cap.gotDepth != 0 {
		t.Fatalf("depth in tool ctx = %d, want 0 (root call)", cap.gotDepth)
	}
	if cap.gotTurn != 4 {
		t.Fatalf("turn in tool ctx = %d, want 4", cap.gotTurn)
	}
	if cap.gotToolUseID != "call_capture" {
		t.Fatalf("tool use id in ctx = %q, want call_capture", cap.gotToolUseID)
	}
}

func TestAgent_NoRunnerWhenOptionUnset(t *testing.T) {
	cap := &captureSubagentTool{}
	reg := tool.New()
	if err := reg.Register(cap); err != nil {
		t.Fatalf("register: %v", err)
	}
	perm := newPermissionManager(t, nil)
	p := &scriptProvider{scripts: []fake.Script{
		{fake.ToolCall("captureSubagent", map[string]any{}), fake.Done()},
		{fake.TextDelta("done"), fake.Done()},
	}}
	a, err := New(Options{
		Provider:   p,
		Tools:      reg,
		Permission: perm,
		Model:      "fake/model",
		Mode:       execmode.ModeWork,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := a.Run(context.Background(), Request{Prompt: "go", Turn: 1}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if cap.gotRunner {
		t.Fatalf("ctx unexpectedly carried a SubagentRunner")
	}
}

func TestAgent_InjectsWorkspaceMemoryWhenPresent(t *testing.T) {
	ws := t.TempDir()
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	// Write auto memory via the memory package.
	if _, _, err := memory.Append(ws, memory.ScopeAuto, memory.CatProject, "build is `make build`"); err != nil {
		t.Fatalf("append memory: %v", err)
	}
	reg := tool.New()
	if err := fs.Register(reg, ws); err != nil {
		t.Fatalf("register fs: %v", err)
	}
	perm := newPermissionManager(t, nil)
	p := &scriptProvider{scripts: []fake.Script{
		{fake.TextDelta("ok"), fake.Done()},
	}}
	a, err := New(Options{
		Provider:      p,
		Tools:         reg,
		Permission:    perm,
		Model:         "fake/model",
		Mode:          execmode.ModeWork,
		WorkspaceRoot: ws,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := a.Run(context.Background(), Request{Prompt: "hi", Turn: 1}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(p.requests) == 0 {
		t.Fatalf("no provider call")
	}
	// One of the request's messages MUST contain the memory block.
	found := false
	for _, m := range p.requests[0].Messages {
		if m.Role == message.RoleSystem && strings.Contains(m.Text(), "<memory>") && strings.Contains(m.Text(), "make build") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected memory injected; got: %#v", p.requests[0].Messages)
	}
}

func TestAgent_OmitsMemoryWhenAbsent(t *testing.T) {
	ws := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	reg := tool.New()
	if err := fs.Register(reg, ws); err != nil {
		t.Fatalf("register fs: %v", err)
	}
	perm := newPermissionManager(t, nil)
	p := &scriptProvider{scripts: []fake.Script{
		{fake.TextDelta("ok"), fake.Done()},
	}}
	a, err := New(Options{
		Provider:      p,
		Tools:         reg,
		Permission:    perm,
		Model:         "fake/model",
		Mode:          execmode.ModeWork,
		WorkspaceRoot: ws,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := a.Run(context.Background(), Request{Prompt: "hi", Turn: 1}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, m := range p.requests[0].Messages {
		if strings.Contains(m.Text(), "<memory>") {
			t.Fatalf("memory should not be injected when files absent")
		}
	}
}

func TestAgentAutoWritesMemoryAfterSuccessfulTurn(t *testing.T) {
	ws := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	reg := tool.New()
	perm := newPermissionManager(t, nil)
	writer := &recordingRollout{}
	main := &scriptProvider{scripts: []fake.Script{
		{fake.TextDelta("Use make build from now on."), fake.Done()},
	}}
	summary := &scriptProvider{scripts: []fake.Script{
		{fake.TextDelta(`{"memories":[{"category":"project","text":"build command is ` + "`make build`" + `"}]}`), fake.Done()},
	}}
	enabled := true
	var events []Event
	var backgroundEvents []Event
	a, err := New(Options{
		Provider:           main,
		Tools:              reg,
		Permission:         perm,
		Rollout:            writer,
		Model:              "fake/model",
		Mode:               execmode.ModeWork,
		AutoMemoryProvider: summary,
		AutoMemoryModel:    "small",
		WorkspaceRoot:      ws,
		Memory: config.MemoryConfig{Auto: config.MemoryAutoConfig{
			Enabled:       &enabled,
			MaxCandidates: 3,
		}},
		Events: func(event Event) {
			events = append(events, event)
		},
		BackgroundEvents: func(event Event) {
			backgroundEvents = append(backgroundEvents, event)
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := a.Run(context.Background(), Request{SessionID: "sess_auto_mem", Prompt: "remember build command", Turn: 1}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if err := a.DrainAutoMemory(context.Background()); err != nil {
		t.Fatalf("DrainAutoMemory: %v", err)
	}
	if len(summary.requests) != 1 || summary.requests[0].Model != "small" {
		t.Fatalf("summary requests = %#v", summary.requests)
	}
	got := memory.Read(ws, 0)
	if !strings.Contains(got, "build command is `make build`") {
		t.Fatalf("memory missing auto fact:\n%s", got)
	}
	var payload rollout.MemoryWritePayload
	found := false
	for _, event := range writer.events {
		if event.Type != rollout.TypeMemoryWrite {
			continue
		}
		found = true
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			t.Fatalf("decode memory_write: %v", err)
		}
	}
	if !found {
		t.Fatalf("missing memory_write event: %#v", writer.events)
	}
	if payload.Source != "auto" || payload.Scope != "auto" || payload.Category != "project" || !strings.Contains(payload.Text, "make build") {
		t.Fatalf("memory_write payload = %#v", payload)
	}
	for _, event := range events {
		if event.Type == EventActivity && strings.Contains(event.Summary, "memory ") {
			t.Fatalf("auto memory emitted post-turn UI activity: %+v", event)
		}
	}
	if !hasActivity(backgroundEvents, ActivityNotice, "memory ") {
		t.Fatalf("auto memory did not emit background write notice: %+v", backgroundEvents)
	}
}

func TestMemoryAutoSchedulerRecoversJobPanic(t *testing.T) {
	scheduler := NewMemoryAutoScheduler()
	var events []Event
	a := &Agent{events: func(event Event) {
		events = append(events, event)
	}}
	scheduler.inProgress = true
	scheduler.run(autoMemoryJob{
		agent:    a,
		ctx:      context.Background(),
		session:  "sess",
		turn:     1,
		messages: []message.Message{message.Text(message.RoleUser, "remember this")},
	})

	scheduler.mu.Lock()
	inProgress := scheduler.inProgress
	pending := scheduler.pending
	scheduler.mu.Unlock()
	if inProgress || pending != nil {
		t.Fatalf("scheduler did not release state after panic: inProgress=%v pending=%v", inProgress, pending)
	}
	if len(events) != 1 || events[0].Type != EventActivity || !events[0].IsError || !strings.Contains(events[0].Summary, "auto memory panic") {
		t.Fatalf("events = %+v", events)
	}
}

func TestAgentAutoMemorySkipsPlanMode(t *testing.T) {
	ws := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	reg := tool.New()
	main := &scriptProvider{scripts: []fake.Script{
		{fake.TextDelta("plan only"), fake.Done()},
	}}
	summary := &scriptProvider{scripts: []fake.Script{
		{fake.Error("memory should not run")},
	}}
	enabled := true
	a, err := New(Options{
		Provider:           main,
		Tools:              reg,
		Model:              "fake/model",
		Mode:               execmode.ModePlan,
		AutoMemoryProvider: summary,
		WorkspaceRoot:      ws,
		Memory: config.MemoryConfig{Auto: config.MemoryAutoConfig{
			Enabled: &enabled,
		}},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := a.Run(context.Background(), Request{SessionID: "sess_plan_mem", Prompt: "make a plan", Turn: 1}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(summary.requests) != 0 {
		t.Fatalf("auto memory requests = %d, want 0", len(summary.requests))
	}
	if got := memory.Read(ws, 0); got != "" {
		t.Fatalf("plan mode should not write memory:\n%s", got)
	}
}

func TestAgentAutoMemoryDoesNotRunForSimpleSingleTurn(t *testing.T) {
	ws := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	reg := tool.New()
	main := &scriptProvider{scripts: []fake.Script{
		{fake.TextDelta("hello"), fake.Done()},
	}}
	summary := &scriptProvider{scripts: []fake.Script{
		{fake.Error("memory should not run for a simple single turn")},
	}}
	enabled := true
	a, err := New(Options{
		Provider:           main,
		Tools:              reg,
		Model:              "fake/model",
		Mode:               execmode.ModeWork,
		AutoMemoryProvider: summary,
		AutoMemoryModel:    "small",
		WorkspaceRoot:      ws,
		Memory: config.MemoryConfig{Auto: config.MemoryAutoConfig{
			Enabled: &enabled,
		}},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := a.Run(context.Background(), Request{SessionID: "sess_simple_mem", Prompt: "hi", Turn: 1}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if err := a.DrainAutoMemory(context.Background()); err != nil {
		t.Fatalf("DrainAutoMemory: %v", err)
	}
	if len(summary.requests) != 0 {
		t.Fatalf("summary requests = %d, want 0", len(summary.requests))
	}
	if got := memory.Read(ws, 0); got != "" {
		t.Fatalf("simple turn should not write memory:\n%s", got)
	}
}

func TestAgentAutoMemoryBatchesTurnsBeforeExtraction(t *testing.T) {
	ws := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	reg := tool.New()
	main := &scriptProvider{scripts: []fake.Script{
		{fake.TextDelta("first answer"), fake.Done()},
		{fake.TextDelta("second answer"), fake.Done()},
	}}
	summary := &scriptProvider{scripts: []fake.Script{
		{fake.TextDelta(`{"memories":[{"category":"project","text":"test command is ` + "`make test`" + `"}]}`), fake.Done()},
	}}
	enabled := true
	a, err := New(Options{
		Provider:           main,
		Tools:              reg,
		Model:              "fake/model",
		Mode:               execmode.ModeWork,
		AutoMemoryProvider: summary,
		AutoMemoryModel:    "small",
		WorkspaceRoot:      ws,
		Memory: config.MemoryConfig{Auto: config.MemoryAutoConfig{
			Enabled:                 &enabled,
			MinTurnsSinceExtraction: 2,
			MinNewMessages:          99,
		}},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	res, err := a.Run(context.Background(), Request{SessionID: "sess_batch_mem", Prompt: "inspect build setup", Turn: 1})
	if err != nil {
		t.Fatalf("Run turn 1: %v", err)
	}
	if err := a.DrainAutoMemory(context.Background()); err != nil {
		t.Fatalf("DrainAutoMemory turn 1: %v", err)
	}
	if len(summary.requests) != 0 {
		t.Fatalf("auto memory requests after turn 1 = %d, want 0", len(summary.requests))
	}
	if _, err := a.Run(context.Background(), Request{
		SessionID: "sess_batch_mem",
		Prompt:    "inspect tests",
		Turn:      2,
		History:   res.Messages,
	}); err != nil {
		t.Fatalf("Run turn 2: %v", err)
	}
	if err := a.DrainAutoMemory(context.Background()); err != nil {
		t.Fatalf("DrainAutoMemory turn 2: %v", err)
	}
	if len(summary.requests) != 1 {
		t.Fatalf("auto memory requests after turn 2 = %d, want 1", len(summary.requests))
	}
	rendered := renderMessages(summary.requests[0].Messages)
	if !strings.Contains(rendered, "first answer") || !strings.Contains(rendered, "second answer") {
		t.Fatalf("memory extraction did not receive batched turns:\n%s", rendered)
	}
}

func TestAgentAutoMemorySkipsExternalContextTools(t *testing.T) {
	ws := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	reg := tool.New()
	if err := reg.Register(&namedSafeTool{name: "mcp__remote__lookup"}); err != nil {
		t.Fatalf("register mcp tool: %v", err)
	}
	main := &scriptProvider{scripts: []fake.Script{
		{fake.ToolCall("mcp__remote__lookup", map[string]any{"query": "latest"}), fake.Done()},
		{fake.TextDelta("done"), fake.Done()},
	}}
	summary := &scriptProvider{scripts: []fake.Script{
		{fake.Error("memory should not run after external context")},
	}}
	enabled := true
	disableExternal := true
	var events []Event
	var backgroundEvents []Event
	a, err := New(Options{
		Provider:           main,
		Tools:              reg,
		Model:              "fake/model",
		Mode:               execmode.ModeWork,
		AutoMemoryProvider: summary,
		AutoMemoryModel:    "small",
		WorkspaceRoot:      ws,
		Memory: config.MemoryConfig{Auto: config.MemoryAutoConfig{
			Enabled:                  &enabled,
			DisableOnExternalContext: &disableExternal,
		}},
		Events: func(event Event) {
			events = append(events, event)
		},
		BackgroundEvents: func(event Event) {
			backgroundEvents = append(backgroundEvents, event)
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := a.Run(context.Background(), Request{SessionID: "sess_external_mem", Prompt: "remember this external result", Turn: 1}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if err := a.DrainAutoMemory(context.Background()); err != nil {
		t.Fatalf("DrainAutoMemory: %v", err)
	}
	if len(summary.requests) != 0 {
		t.Fatalf("auto memory requests = %d, want 0", len(summary.requests))
	}
	if hasActivity(events, ActivityNotice, "auto memory skipped") {
		t.Fatalf("external-context skip emitted on turn event stream: %+v", events)
	}
	if !hasActivity(backgroundEvents, ActivityNotice, "auto memory skipped") {
		t.Fatalf("external-context skip did not emit background notice: %+v", backgroundEvents)
	}
}

func TestAgentRecordsRememberToolMemoryWrite(t *testing.T) {
	ws := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	reg := tool.New()
	if err := memorytool.Register(reg, ws); err != nil {
		t.Fatalf("register memory: %v", err)
	}
	perm := newPermissionManager(t, nil)
	writer := &recordingRollout{}
	p := &scriptProvider{scripts: []fake.Script{
		{fake.ToolCall("remember", map[string]any{"text": "test command is `make test`", "category": "project"}), fake.Done()},
		{fake.TextDelta("done"), fake.Done()},
	}}
	a, err := New(Options{
		Provider:   p,
		Tools:      reg,
		Permission: perm,
		Rollout:    writer,
		Model:      "fake/model",
		Mode:       execmode.ModeWork,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := a.Run(context.Background(), Request{SessionID: "sess_tool_mem", Prompt: "remember this", Turn: 1}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	var payload rollout.MemoryWritePayload
	found := false
	for _, event := range writer.events {
		if event.Type != rollout.TypeMemoryWrite {
			continue
		}
		found = true
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			t.Fatalf("decode memory_write: %v", err)
		}
	}
	if !found {
		t.Fatalf("missing memory_write event: %#v", writer.events)
	}
	if payload.Source != "tool" || payload.Scope != "auto" || payload.Category != "project" || !strings.Contains(payload.Text, "make test") {
		t.Fatalf("memory_write payload = %#v", payload)
	}
}

func TestAgentInjectsSessionIDIntoToolContext(t *testing.T) {
	rec := &recordSessionTool{}
	reg := tool.New()
	if err := reg.Register(rec); err != nil {
		t.Fatalf("register: %v", err)
	}
	perm := newPermissionManager(t, nil)
	p := &scriptProvider{scripts: []fake.Script{
		{fake.ToolCall("recordsess", map[string]any{}), fake.Done()},
		{fake.TextDelta("done"), fake.Done()},
	}}
	a := newTestAgent(t, p, reg, perm, execmode.ModeWork)
	if _, err := a.Run(context.Background(), Request{SessionID: "sess-xyz", Prompt: "go", Turn: 1}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rec.got != "sess-xyz" {
		t.Fatalf("tool ctx sessionID = %q, want sess-xyz", rec.got)
	}
}
