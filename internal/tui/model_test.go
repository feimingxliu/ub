package tui

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/feimingxliu/ub/internal/execution"
	"github.com/feimingxliu/ub/internal/permission"
	"github.com/feimingxliu/ub/internal/tool"
	"github.com/feimingxliu/ub/internal/tui/tuitheme"
)

func TestModelEchoesInputOnEnter(t *testing.T) {
	model := NewModel(Options{Model: "fake/test", ExecutionMode: "plan", Cwd: "/work"})
	model = sendText(t, model, "hello")

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("enter returned unexpected command")
	}
	model = assertModel(t, updated)

	if got, want := model.MessageTexts(), []string{"hello"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("messages = %#v, want %#v", got, want)
	}
	if got := model.InputValue(); got != "" {
		t.Fatalf("input = %q, want empty", got)
	}
	view := model.View()
	for _, want := range []string{"› hello", "model: fake/test", "mode: plan", "cwd: /work"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
}

func TestModelIgnoresEmptyEnter(t *testing.T) {
	model := NewModel(Options{})

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("enter returned unexpected command")
	}
	model = assertModel(t, updated)

	if got := model.MessageTexts(); len(got) != 0 {
		t.Fatalf("messages = %#v, want none", got)
	}
	if !strings.Contains(model.View(), "No messages yet") {
		t.Fatalf("empty view missing placeholder:\n%s", model.View())
	}
}

func TestModelStreamsRunnerEvents(t *testing.T) {
	runner := &scriptedRunner{events: []Event{
		{Type: EventDeltaText, Text: "he"},
		{Type: EventDeltaText, Text: "llo"},
		{Type: EventDone},
	}}
	model := NewModel(Options{Runner: runner, Model: "fake/test"})
	model = sendText(t, model, "ping")

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatalf("enter returned nil command")
	}
	model = assertModel(t, updated)
	if !model.Running() || model.Turn() != 1 {
		t.Fatalf("running=%v turn=%d, want running turn 1", model.Running(), model.Turn())
	}

	model = drainBatch(t, model, cmd)

	if runner.calls != 1 || runner.prompts[0] != "ping" {
		t.Fatalf("runner calls=%d prompts=%v", runner.calls, runner.prompts)
	}
	if got, want := model.MessageTexts(), []string{"ping", "hello"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("messages = %#v, want %#v", got, want)
	}
	if model.Running() {
		t.Fatalf("model still running after done")
	}
	if !strings.Contains(model.View(), "state: idle") {
		t.Fatalf("view missing idle state:\n%s", model.View())
	}
}

func TestModelUpdatesContextStatusFromEvent(t *testing.T) {
	model := NewModel(Options{Model: "fake/test"})
	model.running = true
	model.runID = 1
	model.events = make(chan Event)

	updated, cmd := model.Update(streamEventMsg{
		runID: 1,
		ok:    true,
		event: Event{Type: EventContext, ContextUsedTokens: 1200, ContextMaxTokens: 8000, ContextRatio: 0.15},
	})
	if cmd == nil {
		t.Fatal("context event should continue waiting for stream events")
	}
	model = assertModel(t, updated)
	if !strings.Contains(model.View(), "ctx: 1200/8000 15%") {
		t.Fatalf("view missing context usage:\n%s", model.View())
	}

	updated, _ = model.Update(streamEventMsg{
		runID: 1,
		ok:    true,
		event: Event{Type: EventContext, ContextUsedTokens: 1200},
	})
	model = assertModel(t, updated)
	view := model.View()
	if !strings.Contains(view, "ctx: 1200") || strings.Contains(view, "ctx: 1200/") {
		t.Fatalf("view should show used tokens without unknown max:\n%s", view)
	}
}

func TestModelDoneFinalizesUntilRunnerCloses(t *testing.T) {
	events := make(chan Event)
	model := NewModel(Options{Model: "fake/test"})
	model.running = true
	model.status.state = statusStreaming
	model.events = events

	updated, cmd := model.Update(streamEventMsg{event: Event{Type: EventDone}, ok: true})
	if cmd == nil {
		t.Fatalf("done returned nil command")
	}
	model = assertModel(t, updated)
	if !model.Running() || !strings.Contains(model.View(), "state: finalizing") {
		t.Fatalf("done should keep run finalizing: running=%v view=\n%s", model.Running(), model.View())
	}

	close(events)
	updated, cmd = model.Update(cmd())
	if cmd != nil {
		t.Fatalf("closed stream returned unexpected command")
	}
	model = assertModel(t, updated)
	if model.Running() || !strings.Contains(model.View(), "state: idle") {
		t.Fatalf("closed stream should return idle: running=%v view=\n%s", model.Running(), model.View())
	}
}

func TestModelIgnoresSecondEnterWhileRunning(t *testing.T) {
	runner := &scriptedRunner{}
	model := NewModel(Options{Runner: runner})
	model = sendText(t, model, "first")

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatalf("first enter returned nil command")
	}
	model = assertModel(t, updated)

	updated, secondCmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = assertModel(t, updated)
	if secondCmd != nil {
		t.Fatalf("second enter returned unexpected command")
	}
	if got, want := model.MessageTexts(), []string{"first", "Thinking..."}; !reflect.DeepEqual(got, want) {
		t.Fatalf("messages = %#v, want %#v", got, want)
	}
}

func TestModelRendersToolEvents(t *testing.T) {
	runner := &scriptedRunner{events: []Event{
		{Type: EventToolCallStart, ToolName: "read"},
		{Type: EventToolCallEnd, ToolName: "read"},
		{Type: EventDone},
	}}
	model := NewModel(Options{Runner: runner})
	model = sendText(t, model, "read file")

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = assertModel(t, updated)
	model = drainBatch(t, model, cmd)

	view := model.View()
	for _, want := range []string{"tool read started", "tool read finished"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
}

func TestModelRendersActivityEvents(t *testing.T) {
	runner := &scriptedRunner{events: []Event{
		{Type: EventActivity, ActivityKind: "thinking", Summary: "checking repository context"},
		{Type: EventActivity, ActivityKind: "tool", ToolUseID: "call_1", ToolName: "read", Status: "running", Summary: "path=main.go"},
		{Type: EventActivity, ActivityKind: "permission", ToolName: "bash", Source: "approval_agent", Decision: "allow", Allowed: true, Reason: "read-only command"},
		{Type: EventActivity, ActivityKind: "tool", ToolUseID: "call_1", ToolName: "read", Status: "done", Summary: "path=main.go", Content: "package main"},
		{Type: EventDone},
	}}
	model := NewModel(Options{Runner: runner})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	model = assertModel(t, updated)
	model = sendText(t, model, "inspect")

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = assertModel(t, updated)
	model = drainBatch(t, model, cmd)

	view := model.View()
	for _, want := range []string{
		"tools: 1 done",
		"permissions: 1",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
	updated, cmd = model.Update(tea.MouseMsg{Type: tea.MouseLeft, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, Y: 2})
	if cmd != nil {
		t.Fatalf("mouse click returned unexpected command")
	}
	model = assertModel(t, updated)
	view = model.View()
	for _, want := range []string{
		"checking repository context",
		"Read path=main.go",
		"permission approval_agent allow bash: read-only command",
		"package main",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("expanded view missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "assistant:") || strings.Contains(view, "user:") {
		t.Fatalf("view should not render explicit role labels:\n%s", view)
	}
}

func TestToolActivityUpdatesInPlace(t *testing.T) {
	model := NewModel(Options{})
	model.running = true
	model.runID = 1
	model.events = make(chan Event)

	updated, cmd := model.Update(streamEventMsg{
		runID: 1,
		ok:    true,
		event: Event{Type: EventActivity, ActivityKind: "tool", ToolUseID: "call_1", ToolName: "read", Status: "queued", Summary: "path=main.go"},
	})
	if cmd == nil {
		t.Fatal("activity event should continue waiting for stream events")
	}
	model = assertModel(t, updated)
	updated, _ = model.Update(streamEventMsg{
		runID: 1,
		ok:    true,
		event: Event{Type: EventActivity, ActivityKind: "tool", ToolUseID: "call_1", ToolName: "read", Status: "done", Summary: "path=main.go", Content: "file content"},
	})
	model = assertModel(t, updated)

	got := model.MessageTexts()
	if len(got) != 1 || got[0] != "tools: 1 done · last: Read path=main.go" {
		t.Fatalf("messages = %#v, want single updated tool activity", got)
	}
}

func TestActivityGroupSummaryShowsLatestActiveTools(t *testing.T) {
	model := NewModel(Options{})
	model.running = true
	model.runID = 2
	model.events = make(chan Event)

	for _, event := range []Event{
		{Type: EventActivity, ActivityKind: "tool", ToolUseID: "call_read", ToolName: "read", Status: "done", Summary: "path=main.go", Content: "file content"},
		{Type: EventActivity, ActivityKind: "tool", ToolUseID: "call_test", ToolName: "bash", Status: "running", Summary: "cmd=go test ./..."},
		{Type: EventActivity, ActivityKind: "tool", ToolUseID: "call_grep", ToolName: "grep", Status: "queued", Summary: "pattern=TODO"},
	} {
		updated, cmd := model.Update(streamEventMsg{runID: 2, ok: true, event: event})
		if cmd == nil {
			t.Fatal("activity event should continue waiting for stream events")
		}
		model = assertModel(t, updated)
	}

	got := model.MessageTexts()
	if len(got) != 1 {
		t.Fatalf("messages = %#v, want one activity group", got)
	}
	for _, want := range []string{"1 running", "1 queued", "now: pattern=TODO, cmd=go test ./..."} {
		if !strings.Contains(got[0], want) {
			t.Fatalf("summary missing %q: %#v", want, got)
		}
	}
	if strings.Contains(got[0], "path=main.go") {
		t.Fatalf("summary should prioritize active tools over completed tools: %#v", got)
	}
}

func TestMarkdownMessagesRenderReadableBlocks(t *testing.T) {
	model := NewModel(Options{Messages: []InitialMessage{{
		Role: assistantRole,
		Text: "# Plan\n\n- inspect repository\n- patch renderer\n\n```go\nfmt.Println(\"ok\")\n```",
	}}})

	view := model.View()
	for _, want := range []string{"Plan", "inspect repository", "patch renderer", `fmt.Println("ok")`} {
		if !strings.Contains(view, want) {
			t.Fatalf("markdown view missing %q:\n%s", want, view)
		}
	}
}

func TestPlainMessageRenderHasNoANSI(t *testing.T) {
	list := newMessageList()
	list.append(userRole, "# Title\n\n- item")

	view := list.view(60, 20, 0, tuitheme.Plain())
	if strings.Contains(view, "\x1b[") {
		t.Fatalf("plain render contains ANSI:\n%q", view)
	}
	for _, want := range []string{"Title", "item"} {
		if !strings.Contains(view, want) {
			t.Fatalf("plain render missing %q:\n%s", want, view)
		}
	}
}

func TestStyledMessageRenderContainsANSIAndSymbols(t *testing.T) {
	oldProfile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(oldProfile)

	model := NewModel(Options{Model: "fake/test"})
	model = sendText(t, model, "hello")
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = assertModel(t, updated)

	view := model.View()
	if !strings.Contains(view, "\x1b[") || !strings.Contains(view, "› ") || !strings.Contains(view, "hello") {
		t.Fatalf("styled render missing ANSI or role symbol:\n%q", view)
	}
}

func TestKeyboardDoesNotToggleActivityBlocks(t *testing.T) {
	model := NewModel(Options{})
	model = sendText(t, model, "first")
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = assertModel(t, updated)
	model = sendText(t, model, "second")
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = assertModel(t, updated)

	model.messages.appendOrUpdateActivity(Event{
		Type:         EventActivity,
		ActivityKind: "tool",
		ToolUseID:    "call_1",
		ToolName:     "read",
		Status:       "done",
		Summary:      "path=main.go",
		Content:      "file content",
	})
	if strings.Contains(model.View(), "file content") {
		t.Fatalf("tool detail should default collapsed:\n%s", model.View())
	}

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyUp})
	model = assertModel(t, updated)
	if cmd != nil {
		t.Fatalf("up returned unexpected command")
	}
	if got := model.InputValue(); got != "second" {
		t.Fatalf("up input = %q, want second", got)
	}
	if strings.Contains(model.View(), "└ file content") {
		t.Fatalf("up should not expand activity:\n%s", model.View())
	}

	updated, cmd = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = assertModel(t, updated)
	if cmd != nil {
		t.Fatalf("down returned unexpected command")
	}
	if got := model.InputValue(); got != "" {
		t.Fatalf("down input = %q, want empty draft", got)
	}
	if strings.Contains(model.View(), "└ file content") {
		t.Fatalf("down should not expand activity:\n%s", model.View())
	}

	updated, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = assertModel(t, updated)
	if cmd != nil {
		t.Fatalf("enter returned unexpected command")
	}
	if strings.Contains(model.View(), "└ file content") {
		t.Fatalf("enter should not expand activity:\n%s", model.View())
	}

	updated, cmd = model.Update(tea.KeyMsg{Type: tea.KeySpace})
	model = assertModel(t, updated)
	if cmd != nil {
		t.Fatalf("space returned unexpected command")
	}
	if strings.Contains(model.View(), "└ file content") {
		t.Fatalf("space should not expand activity:\n%s", model.View())
	}
}

func TestMouseTogglesActivityBlocks(t *testing.T) {
	model := NewModel(Options{})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	model = assertModel(t, updated)
	model.messages.appendOrUpdateActivity(Event{
		Type:         EventActivity,
		ActivityKind: "thinking",
		Summary:      "checking context",
		Content:      "full reasoning summary",
	})
	if strings.Contains(model.View(), "full reasoning summary") {
		t.Fatalf("thinking detail should default collapsed:\n%s", model.View())
	}

	updated, cmd := model.Update(tea.MouseMsg{
		Type:   tea.MouseLeft,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
		Y:      0,
	})
	if cmd != nil {
		t.Fatalf("mouse click returned unexpected command")
	}
	model = assertModel(t, updated)
	if !strings.Contains(model.View(), "└ full reasoning summary") {
		t.Fatalf("mouse click did not expand activity:\n%s", model.View())
	}
}

func TestCollapsedActivityBlocksShareRowsAndMouseTargetsChip(t *testing.T) {
	model := NewModel(Options{})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	model = assertModel(t, updated)
	model.messages.appendOrUpdateActivity(Event{
		Type:         EventActivity,
		ActivityKind: "tool",
		ToolUseID:    "call_read",
		ToolName:     "read",
		Status:       "done",
		Summary:      "path=main.go",
		Content:      "read detail",
	})
	model.messages.appendOrUpdateActivity(Event{
		Type:         EventActivity,
		ActivityKind: "tool",
		ToolUseID:    "call_grep",
		ToolName:     "grep",
		Status:       "done",
		Summary:      "pattern=TODO",
		Content:      "grep detail",
	})

	view := model.messages.view(80, 20, 0, tuitheme.Plain())
	lines := strings.Split(view, "\n")
	if len(lines) != 1 {
		t.Fatalf("collapsed activity blocks should share a row:\n%s", view)
	}
	if !strings.Contains(lines[0], "Read path=main.go") || !strings.Contains(lines[0], "Searched pattern=TODO") {
		t.Fatalf("compact row missing tool chips:\n%s", view)
	}

	x := strings.Index(lines[0], "Searched")
	if x < 0 {
		t.Fatalf("second chip not found:\n%s", view)
	}
	updated, cmd := model.Update(tea.MouseMsg{
		Type:   tea.MouseLeft,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
		X:      x,
		Y:      0,
	})
	if cmd != nil {
		t.Fatalf("mouse click returned unexpected command")
	}
	model = assertModel(t, updated)
	rendered := model.View()
	if !strings.Contains(rendered, "└ grep detail") {
		t.Fatalf("mouse click did not expand second activity:\n%s", rendered)
	}
	if strings.Contains(rendered, "└ read detail") {
		t.Fatalf("mouse click expanded wrong activity:\n%s", rendered)
	}
}

func TestModelPermissionRequestReturnsDecision(t *testing.T) {
	response := make(chan permission.Decision, 1)
	model := NewModel(Options{})
	req := PermissionRequest{
		Request: permission.Request{
			Tool: "bash",
			Risk: tool.RiskExec,
			Mode: execution.ModeWork,
		},
		Response: response,
	}

	updated, cmd := model.Update(permissionRequestMsg{request: req, ok: true})
	if cmd != nil {
		t.Fatalf("permission request returned unexpected command")
	}
	model = assertModel(t, updated)
	if !strings.Contains(model.View(), "Permission required") || !strings.Contains(model.View(), "tool: bash") {
		t.Fatalf("view missing modal:\n%s", model.View())
	}

	updated, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'5'}})
	model = assertModel(t, updated)
	if cmd != nil {
		msg := cmd()
		if _, ok := msg.(permissionRequestMsg); ok {
			t.Fatalf("unexpected immediate permission request: %#v", msg)
		}
	}
	select {
	case got := <-response:
		if got != permission.DecisionAlwaysGlobal {
			t.Fatalf("decision = %q, want always global", got)
		}
	default:
		t.Fatalf("no decision returned")
	}
	if strings.Contains(model.View(), "Permission required") {
		t.Fatalf("modal still visible:\n%s", model.View())
	}
}

func TestModelPermissionSelectionReturnsDecision(t *testing.T) {
	response := make(chan permission.Decision, 1)
	model := NewModel(Options{})
	req := PermissionRequest{
		Request: permission.Request{
			Tool: "bash",
			Risk: tool.RiskExec,
			Mode: execution.ModeWork,
		},
		Response: response,
	}

	updated, _ := model.Update(permissionRequestMsg{request: req, ok: true})
	model = assertModel(t, updated)
	if !strings.Contains(model.View(), "> Allow once") {
		t.Fatalf("permission modal missing selectable options:\n%s", model.View())
	}

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyDown})
	if cmd != nil {
		t.Fatalf("down returned unexpected command")
	}
	model = assertModel(t, updated)
	if !strings.Contains(model.View(), "> Deny") {
		t.Fatalf("permission modal did not move selection:\n%s", model.View())
	}

	updated, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = assertModel(t, updated)
	if cmd != nil {
		msg := cmd()
		if _, ok := msg.(permissionRequestMsg); ok {
			t.Fatalf("unexpected immediate permission request: %#v", msg)
		}
	}
	select {
	case got := <-response:
		if got != permission.DecisionDeny {
			t.Fatalf("decision = %q, want deny", got)
		}
	default:
		t.Fatalf("no decision returned")
	}
}

func TestSlashHelpDoesNotCallRunner(t *testing.T) {
	runner := &scriptedRunner{}
	model := NewModel(Options{Runner: runner})
	model = sendText(t, model, "/help")

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("slash help returned unexpected command")
	}
	model = assertModel(t, updated)

	if runner.calls != 0 {
		t.Fatalf("runner calls = %d, want 0", runner.calls)
	}
	if got := model.MessageTexts(); len(got) != 1 || !strings.Contains(got[0], "/model [model]") {
		t.Fatalf("messages = %#v, want help message", got)
	}
}

func TestSlashCompactRunsCompactRunner(t *testing.T) {
	runner := &scriptedRunner{compactEvents: []Event{
		{Type: EventActivity, ActivityKind: "notice", Status: "done", Summary: "compacted 4 earlier messages"},
		{Type: EventContext, ContextUsedTokens: 900, ContextMaxTokens: 3000, ContextRatio: 0.3},
		{Type: EventDone},
	}}
	model := NewModel(Options{Runner: runner})
	model = sendText(t, model, "/compact")

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatalf("slash compact returned nil command")
	}
	model = assertModel(t, updated)
	model = drainBatch(t, model, cmd)

	if runner.compactCalls != 1 {
		t.Fatalf("compact calls = %d, want 1", runner.compactCalls)
	}
	if runner.calls != 0 || len(runner.prompts) != 0 {
		t.Fatalf("prompt runner should not be called: calls=%d prompts=%v", runner.calls, runner.prompts)
	}
	view := model.View()
	if !strings.Contains(view, "compacted 4 earlier messages") || !strings.Contains(view, "ctx: 900/3000 30%") {
		t.Fatalf("view missing compact result:\n%s", view)
	}
}

func TestSlashCompactUnavailable(t *testing.T) {
	runner := &promptOnlyRunner{}
	model := NewModel(Options{Runner: runner})
	model = sendText(t, model, "/compact")

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("slash compact returned unexpected command")
	}
	model = assertModel(t, updated)

	if runner.calls != 0 {
		t.Fatalf("prompt runner calls = %d, want 0", runner.calls)
	}
	if got := model.MessageTexts(); len(got) != 1 || !strings.Contains(got[0], "compact is unavailable") {
		t.Fatalf("messages = %#v", got)
	}
}

func TestSlashClear(t *testing.T) {
	model := NewModel(Options{})
	model = sendText(t, model, "hello")
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = assertModel(t, updated)
	model = sendText(t, model, "/clear")

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("slash clear returned unexpected command")
	}
	model = assertModel(t, updated)
	if got := model.MessageTexts(); len(got) != 0 {
		t.Fatalf("messages = %#v, want cleared", got)
	}
}

func TestSlashQuit(t *testing.T) {
	for _, input := range []string{"/quit", "/exit"} {
		t.Run(input, func(t *testing.T) {
			model := NewModel(Options{})
			model = sendText(t, model, input)

			updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
			_ = assertModel(t, updated)
			if cmd == nil {
				t.Fatalf("%s returned nil command", input)
			}
			if _, ok := cmd().(tea.QuitMsg); !ok {
				t.Fatalf("%s command = %T, want tea.QuitMsg", input, cmd())
			}
		})
	}
}

func TestSlashModelAndModeUpdateRunner(t *testing.T) {
	runner := &scriptedRunner{models: []string{"fake/old", "fake/new"}}
	model := NewModel(Options{Runner: runner, Model: "fake/old", Models: runner.models, ExecutionMode: "default"})
	model = sendText(t, model, "/model fake/new")
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = assertModel(t, updated)
	if runner.model != "fake/new" || !strings.Contains(model.View(), "model: fake/new") {
		t.Fatalf("model update failed: runner=%q view=\n%s", runner.model, model.View())
	}

	model = sendText(t, model, "/mode plan")
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = assertModel(t, updated)
	if runner.mode != "plan" || !strings.Contains(model.View(), "mode: plan") {
		t.Fatalf("mode update failed: runner=%q view=\n%s", runner.mode, model.View())
	}
	if got, want := model.MessageTexts(), []string{"model set to fake/new"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("messages = %#v, want %#v", got, want)
	}
}

func TestSlashEffortUpdatesRunner(t *testing.T) {
	runner := &scriptedRunner{effort: "low", efforts: []string{"none", "low", "high"}}
	model := NewModel(Options{Runner: runner, Model: "fake/model", Effort: runner.effort, Efforts: runner.efforts})
	model = sendText(t, model, "/effort high")

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = assertModel(t, updated)
	if runner.effort != "high" || !strings.Contains(model.View(), "effort: high") {
		t.Fatalf("effort update failed: runner=%q view=\n%s", runner.effort, model.View())
	}
	if got := model.MessageTexts(); len(got) != 1 || got[0] != "effort set to high" {
		t.Fatalf("messages = %#v", got)
	}
}

func TestSlashEffortWithoutArgsListsCandidates(t *testing.T) {
	runner := &scriptedRunner{effort: "low", efforts: []string{"none", "low", "high"}}
	model := NewModel(Options{Runner: runner, Model: "fake/model", Effort: runner.effort, Efforts: runner.efforts})
	model = sendText(t, model, "/effort")

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("slash effort returned unexpected command")
	}
	model = assertModel(t, updated)
	view := model.View()
	for _, want := range []string{"select effort", "> low", "  high"} {
		if !strings.Contains(view, want) {
			t.Fatalf("effort picker missing %q:\n%s", want, view)
		}
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = assertModel(t, updated)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = assertModel(t, updated)
	if runner.effort != "high" || !strings.Contains(model.View(), "effort: high") {
		t.Fatalf("effort picker selection failed: runner=%q view=\n%s", runner.effort, model.View())
	}
}

func TestSlashEffortRejectsUnsupportedCandidate(t *testing.T) {
	runner := &scriptedRunner{effort: "low", efforts: []string{"none", "low"}}
	model := NewModel(Options{Runner: runner, Model: "fake/model", Effort: runner.effort, Efforts: runner.efforts})
	model = sendText(t, model, "/effort high")

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = assertModel(t, updated)
	if runner.effort != "low" {
		t.Fatalf("runner effort changed to %q, want low", runner.effort)
	}
	view := model.View()
	if !strings.Contains(view, "effort: low") || !strings.Contains(view, "not available") {
		t.Fatalf("invalid effort handling failed:\n%s", view)
	}
}

func TestSlashModelWithoutArgsListsCandidates(t *testing.T) {
	runner := &scriptedRunner{models: []string{"fake/old", "fake/new"}}
	model := NewModel(Options{Runner: runner, Model: "fake/old", Models: runner.models})
	model = sendText(t, model, "/model")

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("slash model returned unexpected command")
	}
	model = assertModel(t, updated)
	if runner.model != "" {
		t.Fatalf("runner model changed to %q, want unchanged", runner.model)
	}
	view := model.View()
	for _, want := range []string{"select model", "> fake/old", "  fake/new"} {
		if !strings.Contains(view, want) {
			t.Fatalf("model picker missing %q:\n%s", want, view)
		}
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = assertModel(t, updated)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = assertModel(t, updated)
	if runner.model != "fake/new" || !strings.Contains(model.View(), "model: fake/new") {
		t.Fatalf("model picker selection failed: runner=%q view=\n%s", runner.model, model.View())
	}
}

func TestSlashApprovalModelUpdatesRunner(t *testing.T) {
	runner := &scriptedRunner{
		approvalModel:  "fake/review-old",
		approvalModels: []string{"fake/review-old", "fake/review-new"},
	}
	model := NewModel(Options{
		Runner:         runner,
		ApprovalModel:  runner.approvalModel,
		ApprovalModels: runner.approvalModels,
	})
	model = sendText(t, model, "/approval-model fake/review-new")

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = assertModel(t, updated)
	if runner.approvalModel != "fake/review-new" {
		t.Fatalf("approval model = %q, want fake/review-new", runner.approvalModel)
	}
	if got := model.MessageTexts(); len(got) != 1 || got[0] != "approval model set to fake/review-new" {
		t.Fatalf("messages = %#v", got)
	}
}

func TestSlashApprovalModelWithoutArgsListsCandidates(t *testing.T) {
	runner := &scriptedRunner{
		approvalModel:  "fake/review-old",
		approvalModels: []string{"fake/review-old", "fake/review-new"},
	}
	model := NewModel(Options{
		Runner:         runner,
		ApprovalModel:  runner.approvalModel,
		ApprovalModels: runner.approvalModels,
	})
	model = sendText(t, model, "/approval-model")

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("slash approval-model returned unexpected command")
	}
	model = assertModel(t, updated)
	view := model.View()
	for _, want := range []string{"select model", "> fake/review-old", "  fake/review-new"} {
		if !strings.Contains(view, want) {
			t.Fatalf("approval model picker missing %q:\n%s", want, view)
		}
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = assertModel(t, updated)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = assertModel(t, updated)
	if runner.approvalModel != "fake/review-new" {
		t.Fatalf("approval model = %q, want fake/review-new", runner.approvalModel)
	}
}

func TestSlashModelRejectsUnsupportedCandidate(t *testing.T) {
	runner := &scriptedRunner{models: []string{"fake/old", "fake/new"}}
	model := NewModel(Options{Runner: runner, Model: "fake/old", Models: runner.models})
	model = sendText(t, model, "/model fake/missing")

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = assertModel(t, updated)
	if runner.model != "" {
		t.Fatalf("runner model changed to %q, want unchanged", runner.model)
	}
	view := model.View()
	if !strings.Contains(view, "model: fake/old") || !strings.Contains(view, "not available") {
		t.Fatalf("invalid model handling failed:\n%s", view)
	}
}

func TestPermissionEventRendersInConversation(t *testing.T) {
	model := NewModel(Options{})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	model = assertModel(t, updated)
	model.running = true
	model.runID = 4
	model.events = make(chan Event)
	updated, cmd := model.Update(streamEventMsg{
		runID: 4,
		ok:    true,
		event: Event{
			Type:         EventActivity,
			ActivityKind: "permission",
			ToolName:     "bash",
			Source:       "approval_agent",
			Decision:     "allow",
			Allowed:      true,
			Reason:       "read-only command",
		},
	})
	if cmd == nil {
		t.Fatal("permission event should continue waiting for stream events")
	}
	model = assertModel(t, updated)
	if got := model.MessageTexts(); len(got) != 1 || got[0] != "permissions: 1" {
		t.Fatalf("messages = %#v, want permission summary", got)
	}
	updated, cmd = model.Update(tea.MouseMsg{Type: tea.MouseLeft, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, Y: 0})
	if cmd != nil {
		t.Fatalf("mouse click returned unexpected command")
	}
	model = assertModel(t, updated)
	view := model.View()
	if !strings.Contains(view, "approval_agent") || !strings.Contains(view, "read-only command") {
		t.Fatalf("expanded permission view missing detail:\n%s", view)
	}
}

func TestShiftTabCyclesMode(t *testing.T) {
	runner := &scriptedRunner{}
	model := NewModel(Options{Runner: runner, ExecutionMode: "work"})

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if cmd != nil {
		t.Fatalf("shift+tab returned unexpected command")
	}
	model = assertModel(t, updated)
	if runner.mode != "plan" || !strings.Contains(model.View(), "mode: plan") {
		t.Fatalf("first shift+tab failed: runner=%q view=\n%s", runner.mode, model.View())
	}
	if got := model.MessageTexts(); len(got) != 0 {
		t.Fatalf("messages = %#v, want no mode switch log", got)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	model = assertModel(t, updated)
	if runner.mode != "auto" || !strings.Contains(model.View(), "mode: auto") {
		t.Fatalf("second shift+tab failed: runner=%q view=\n%s", runner.mode, model.View())
	}
	if got := model.MessageTexts(); len(got) != 0 {
		t.Fatalf("messages = %#v, want no mode switch log", got)
	}
}

func TestShiftTabCyclesModeWhileRunning(t *testing.T) {
	runner := &scriptedRunner{}
	model := NewModel(Options{Runner: runner, ExecutionMode: "work"})
	model.running = true
	model.status.state = statusTool

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if cmd != nil {
		t.Fatalf("shift+tab returned unexpected command")
	}
	model = assertModel(t, updated)
	if runner.mode != "plan" || !strings.Contains(model.View(), "mode: plan") || !model.Running() {
		t.Fatalf("running mode switch failed: runner=%q running=%v view=\n%s", runner.mode, model.Running(), model.View())
	}
}

func TestShiftTabCyclesModeDuringPermission(t *testing.T) {
	response := make(chan permission.Decision, 1)
	runner := &scriptedRunner{}
	model := NewModel(Options{Runner: runner, ExecutionMode: "work"})
	req := PermissionRequest{
		Request: permission.Request{
			Tool: "bash",
			Risk: tool.RiskExec,
			Mode: execution.ModeWork,
		},
		Response: response,
	}
	updated, _ := model.Update(permissionRequestMsg{request: req, ok: true})
	model = assertModel(t, updated)

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if cmd != nil {
		t.Fatalf("shift+tab returned unexpected command")
	}
	model = assertModel(t, updated)
	if runner.mode != "plan" || !strings.Contains(model.View(), "mode: plan") {
		t.Fatalf("permission mode switch failed: runner=%q view=\n%s", runner.mode, model.View())
	}
	if !strings.Contains(model.View(), "mode: plan") {
		t.Fatalf("permission modal did not reflect mode switch:\n%s", model.View())
	}
	select {
	case decision := <-response:
		t.Fatalf("mode switch resolved permission unexpectedly: %q", decision)
	default:
	}
}

func TestStreamEventsContinueDuringPermissionModeSwitch(t *testing.T) {
	response := make(chan permission.Decision, 1)
	runner := &scriptedRunner{}
	model := NewModel(Options{Runner: runner, ExecutionMode: "plan"})
	model.running = true
	model.status.state = statusTool
	model.runID = 9
	model.events = make(chan Event)

	updated, cmd := model.Update(streamEventMsg{
		runID: 9,
		ok:    true,
		event: Event{Type: EventActivity, ActivityKind: "tool", ToolUseID: "call_1", ToolName: "bash", Status: "queued", Summary: "cmd=go test ./..."},
	})
	if cmd == nil {
		t.Fatal("queued event should continue waiting for stream events")
	}
	model = assertModel(t, updated)

	updated, _ = model.Update(permissionRequestMsg{request: PermissionRequest{
		Request:  permission.Request{Tool: "bash", Risk: tool.RiskExec, Mode: execution.ModePlan},
		Response: response,
	}, ok: true})
	model = assertModel(t, updated)

	updated, cmd = model.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if cmd != nil {
		t.Fatalf("shift+tab returned unexpected command")
	}
	model = assertModel(t, updated)
	if runner.mode != "auto" || !strings.Contains(model.View(), "mode: auto") {
		t.Fatalf("permission mode switch failed: runner=%q view=\n%s", runner.mode, model.View())
	}

	updated, cmd = model.Update(streamEventMsg{
		runID: 9,
		ok:    true,
		event: Event{Type: EventActivity, ActivityKind: "tool", ToolUseID: "call_1", ToolName: "bash", Status: "done", Summary: "cmd=go test ./...", Content: "completed"},
	})
	if cmd == nil {
		t.Fatal("done event should continue waiting for stream events")
	}
	model = assertModel(t, updated)
	if got := model.MessageTexts(); len(got) != 1 || !strings.Contains(got[0], "done") || strings.Contains(got[0], "queued") {
		t.Fatalf("messages = %#v, want done summary while permission is open", got)
	}
	if model.pending == nil {
		t.Fatal("stream event should not close pending permission modal")
	}

	updated, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = assertModel(t, updated)
	if cmd != nil {
		msg := cmd()
		if _, ok := msg.(permissionRequestMsg); ok {
			t.Fatalf("unexpected immediate permission request: %#v", msg)
		}
	}
	select {
	case got := <-response:
		if got != permission.DecisionAllow {
			t.Fatalf("decision = %q, want allow once", got)
		}
	default:
		t.Fatal("approval did not resolve permission")
	}
	if model.pending != nil {
		t.Fatalf("permission modal still pending after approval")
	}
}

func TestTabCompletesSlashCommand(t *testing.T) {
	runner := &scriptedRunner{}
	model := NewModel(Options{Runner: runner, ExecutionMode: "work"})
	model = sendText(t, model, "/m")

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyTab})
	if cmd != nil {
		t.Fatalf("tab returned unexpected command")
	}
	model = assertModel(t, updated)
	if got := model.InputValue(); got != "/model " {
		t.Fatalf("input = %q, want /model ", got)
	}
	if runner.mode != "" || !strings.Contains(model.View(), "mode: work") {
		t.Fatalf("tab unexpectedly changed mode: runner=%q view=\n%s", runner.mode, model.View())
	}
}

func TestTabCompletesSlashValueCandidate(t *testing.T) {
	model := NewModel(Options{Model: "fake/current", Effort: "low", Efforts: []string{"none", "low", "high"}})
	model = sendText(t, model, "/effort h")

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyTab})
	if cmd != nil {
		t.Fatalf("tab returned unexpected command")
	}
	model = assertModel(t, updated)
	if got := model.InputValue(); got != "/effort high" {
		t.Fatalf("input = %q, want /effort high", got)
	}
}

func TestEnterSelectsSlashValueCandidate(t *testing.T) {
	runner := &scriptedRunner{effort: "low", efforts: []string{"none", "low", "high"}}
	model := NewModel(Options{Runner: runner, Model: "fake/current", Effort: "low", Efforts: runner.efforts})
	model = sendText(t, model, "/effort h")

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("enter returned unexpected command")
	}
	model = assertModel(t, updated)
	if runner.effort != "high" || model.InputValue() != "" {
		t.Fatalf("effort/input = %q/%q, want high/empty", runner.effort, model.InputValue())
	}
	if !strings.Contains(model.View(), "effort set to high") || !strings.Contains(model.View(), "effort: high") {
		t.Fatalf("enter did not apply selected effort:\n%s", model.View())
	}
}

func TestArrowSelectsSlashValueCandidate(t *testing.T) {
	runner := &scriptedRunner{effort: "none", efforts: []string{"none", "low", "high"}}
	model := NewModel(Options{Runner: runner, Model: "fake/current", Effort: "none", Efforts: runner.efforts})
	model = sendText(t, model, "/effort ")

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyDown})
	if cmd != nil {
		t.Fatalf("down returned unexpected command")
	}
	model = assertModel(t, updated)
	if view := model.View(); !strings.Contains(view, "  none") || !strings.Contains(view, "> low") {
		t.Fatalf("down did not move effort selection:\n%s", view)
	}

	updated, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("enter returned unexpected command")
	}
	model = assertModel(t, updated)
	if runner.effort != "low" {
		t.Fatalf("effort = %q, want low", runner.effort)
	}
}

func TestArrowSelectsSlashSuggestion(t *testing.T) {
	model := NewModel(Options{})
	model = sendText(t, model, "/m")

	view := model.View()
	if !strings.Contains(view, "> /model [model]") || !strings.Contains(view, "  /mode <work|plan|auto>") {
		t.Fatalf("initial slash selection missing:\n%s", view)
	}
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyDown})
	if cmd != nil {
		t.Fatalf("down returned unexpected command")
	}
	model = assertModel(t, updated)
	view = model.View()
	if !strings.Contains(view, "  /model [model]") || !strings.Contains(view, "> /mode <work|plan|auto>") {
		t.Fatalf("down did not move slash selection:\n%s", view)
	}

	updated, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("enter returned unexpected command")
	}
	model = assertModel(t, updated)
	if got := model.InputValue(); got != "/mode " {
		t.Fatalf("input = %q, want /mode ", got)
	}
}

func TestArrowNavigatesPromptHistory(t *testing.T) {
	model := NewModel(Options{})
	model = sendText(t, model, "first")
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = assertModel(t, updated)
	model = sendText(t, model, "second")
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = assertModel(t, updated)

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyUp})
	model = assertModel(t, updated)
	if got := model.InputValue(); got != "second" {
		t.Fatalf("first up input = %q, want second", got)
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyUp})
	model = assertModel(t, updated)
	if got := model.InputValue(); got != "first" {
		t.Fatalf("second up input = %q, want first", got)
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = assertModel(t, updated)
	if got := model.InputValue(); got != "second" {
		t.Fatalf("first down input = %q, want second", got)
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = assertModel(t, updated)
	if got := model.InputValue(); got != "" {
		t.Fatalf("second down input = %q, want empty draft", got)
	}
}

func TestPromptHistoryIncludesRestoredUserMessages(t *testing.T) {
	model := NewModel(Options{Messages: []InitialMessage{
		{Role: userRole, Text: "past prompt"},
		{Role: assistantRole, Text: "past answer"},
	}})

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyUp})
	model = assertModel(t, updated)
	if got := model.InputValue(); got != "past prompt" {
		t.Fatalf("input = %q, want restored prompt", got)
	}
}

func TestMessageAreaScrollsWithinWindow(t *testing.T) {
	var messages []InitialMessage
	for _, text := range []string{"message-01", "message-02", "message-03", "message-04", "message-05", "message-06"} {
		messages = append(messages, InitialMessage{Role: assistantRole, Text: text})
	}
	model := NewModel(Options{Messages: messages})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 8})
	model = assertModel(t, updated)

	view := model.View()
	if !strings.Contains(view, "message-06") || strings.Contains(view, "message-01") {
		t.Fatalf("initial view should show bottom of message area:\n%s", view)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	model = assertModel(t, updated)
	view = model.View()
	if !strings.Contains(view, "message-03") || strings.Contains(view, "message-06") {
		t.Fatalf("pgup did not scroll message area up:\n%s", view)
	}

	updated, _ = model.Update(tea.MouseMsg{Type: tea.MouseWheelDown})
	model = assertModel(t, updated)
	view = model.View()
	if !strings.Contains(view, "message-06") {
		t.Fatalf("mouse wheel down did not return to bottom:\n%s", view)
	}
}

func TestActivityMessagesScrollWithinWindow(t *testing.T) {
	model := NewModel(Options{})
	for _, text := range []string{"activity-01", "activity-02", "activity-03", "activity-04", "activity-05", "activity-06"} {
		model.messages.append(activityRole, text)
	}
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 8})
	model = assertModel(t, updated)

	view := model.View()
	if !strings.Contains(view, "activity-06") || strings.Contains(view, "activity-01") {
		t.Fatalf("initial view should show bottom of activity messages:\n%s", view)
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	model = assertModel(t, updated)
	view = model.View()
	if !strings.Contains(view, "activity-03") || strings.Contains(view, "activity-06") {
		t.Fatalf("pgup did not scroll activity messages:\n%s", view)
	}
}

func TestMessageAreaScrollsDuringPermission(t *testing.T) {
	var messages []InitialMessage
	for _, text := range []string{"message-01", "message-02", "message-03", "message-04", "message-05", "message-06"} {
		messages = append(messages, InitialMessage{Role: assistantRole, Text: text})
	}
	model := NewModel(Options{Messages: messages})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	model = assertModel(t, updated)
	updated, _ = model.Update(permissionRequestMsg{request: PermissionRequest{
		Request:  permission.Request{Tool: "bash", Risk: tool.RiskExec, Mode: execution.ModeWork},
		Response: make(chan permission.Decision, 1),
	}, ok: true})
	model = assertModel(t, updated)

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	if cmd != nil {
		t.Fatalf("pgup returned unexpected command")
	}
	model = assertModel(t, updated)
	view := model.View()
	if !strings.Contains(view, "message-03") {
		t.Fatalf("pgup did not scroll while permission modal is open:\n%s", view)
	}
}

func TestSlashSuggestionsRenderUsage(t *testing.T) {
	model := NewModel(Options{})
	model = sendText(t, model, "/m")

	view := model.View()
	if !strings.Contains(view, "/model [model]") || !strings.Contains(view, "supported model") {
		t.Fatalf("view missing slash suggestions:\n%s", view)
	}
}

func TestSlashModelSuggestionsRenderCandidates(t *testing.T) {
	model := NewModel(Options{Model: "fake/current", Models: []string{"fake/new", "fake/other"}})
	model = sendText(t, model, "/model new")

	view := model.View()
	if !strings.Contains(view, "> fake/new") || strings.Contains(view, "fake/other") {
		t.Fatalf("view missing filtered model suggestions:\n%s", view)
	}
}

func TestSlashEffortSuggestionsRenderCandidates(t *testing.T) {
	model := NewModel(Options{Model: "fake/current", Effort: "low", Efforts: []string{"none", "low", "high"}})
	model = sendText(t, model, "/effort h")

	view := model.View()
	if !strings.Contains(view, "> high") || strings.Contains(view, "\n  low") || strings.Contains(view, "\n> low") {
		t.Fatalf("view missing filtered effort suggestions:\n%s", view)
	}
}

func TestSlashSessionsPickerSwitchesSession(t *testing.T) {
	runner := &scriptedRunner{
		sessions: []SessionInfo{
			{ID: "s1", Title: "First", Model: "fake/one", Current: true},
			{ID: "s2", Title: "Second", Model: "fake/two"},
		},
		sessionStates: map[string]SessionState{
			"s2": {
				ID:    "s2",
				Model: "fake/two",
				Turn:  3,
				Messages: []InitialMessage{
					{Role: userRole, Text: "old prompt"},
					{Role: assistantRole, Text: "old answer"},
				},
			},
		},
	}
	model := NewModel(Options{Runner: runner, Model: "fake/one"})
	model = sendText(t, model, "/sessions")

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("slash sessions returned unexpected command")
	}
	model = assertModel(t, updated)
	if !strings.Contains(model.View(), "select session") || !strings.Contains(model.View(), "s2") {
		t.Fatalf("sessions picker missing:\n%s", model.View())
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = assertModel(t, updated)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = assertModel(t, updated)
	if runner.currentSessionID != "s2" {
		t.Fatalf("current session = %q, want s2", runner.currentSessionID)
	}
	if got := model.MessageTexts(); !reflect.DeepEqual(got, []string{"old prompt", "old answer", "session set to s2"}) {
		t.Fatalf("messages = %#v", got)
	}
	if !strings.Contains(model.View(), "model: fake/two") || !strings.Contains(model.View(), "turn: 3") {
		t.Fatalf("view missing restored state:\n%s", model.View())
	}
}

func TestViewWrapsLongMessagesToWidth(t *testing.T) {
	model := NewModel(Options{})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 24, Height: 20})
	model = assertModel(t, updated)
	model = sendText(t, model, "abcdefghijklmnopqrstuvwxyz")
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = assertModel(t, updated)

	view := model.View()
	if strings.Contains(view, "› abcdefghijklmnopqrstuvwxyz") {
		t.Fatalf("long line was not wrapped:\n%s", view)
	}
	if !strings.Contains(view, "› abcdefghijklmnopqrstuv") || !strings.Contains(view, "  wxyz") {
		t.Fatalf("wrapped message missing expected fragments:\n%s", view)
	}
}

func TestViewWrapsLongActivityAndKeepsRedaction(t *testing.T) {
	model := NewModel(Options{})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 30, Height: 20})
	model = assertModel(t, updated)
	model.messages.append(activityRole, activityEventText(Event{
		Type:         EventActivity,
		ActivityKind: "tool",
		ToolName:     "bash",
		Status:       "running",
		Summary:      "cmd=[redacted], cwd=/workspace, detail=abcdefghijklmnopqrstuvwxyz",
	}))

	view := model.View()
	if strings.Contains(view, "secret-token") {
		t.Fatalf("view leaked secret:\n%s", view)
	}
	if !strings.Contains(view, "[redacted]") || !strings.Contains(view, "• Writing command...") {
		t.Fatalf("activity view missing summary:\n%s", view)
	}
	if strings.Contains(view, "• Writing command... cmd=[redacted], cwd=/workspace, detail=abcdefghijklmnopqrstuvwxyz") {
		t.Fatalf("activity line was not wrapped:\n%s", view)
	}
}

func TestSlashUnknownCommand(t *testing.T) {
	runner := &scriptedRunner{}
	model := NewModel(Options{Runner: runner})
	model = sendText(t, model, "/wat")

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("unknown slash returned unexpected command")
	}
	model = assertModel(t, updated)
	if runner.calls != 0 {
		t.Fatalf("runner calls = %d, want 0", runner.calls)
	}
	if got := model.MessageTexts(); len(got) != 1 || !strings.Contains(got[0], "unknown slash command") {
		t.Fatalf("messages = %#v, want unknown command error", got)
	}
}

func TestModelCtrlCQuits(t *testing.T) {
	model := NewModel(Options{})

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatalf("ctrl+c returned nil command")
	}
	_ = assertModel(t, updated)
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("ctrl+c command = %T, want tea.QuitMsg", cmd())
	}
}

func TestEscInterruptsRunningInsteadOfQuitting(t *testing.T) {
	cancelled := false
	model := NewModel(Options{})
	model.running = true
	model.status.state = statusStreaming
	model.runID = 3
	model.cancel = func() { cancelled = true }

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		t.Fatalf("esc returned unexpected command")
	}
	model = assertModel(t, updated)
	if !cancelled || model.Running() || !strings.Contains(model.View(), "state: idle") {
		t.Fatalf("esc did not interrupt: cancelled=%v running=%v view=\n%s", cancelled, model.Running(), model.View())
	}
	if model.runID != 4 {
		t.Fatalf("runID = %d, want 4", model.runID)
	}
}

func TestEscDuringPermissionDeniesAndInterrupts(t *testing.T) {
	response := make(chan permission.Decision, 1)
	requests := make(chan PermissionRequest)
	model := NewModel(Options{Permissions: requests})
	model.running = true
	model.cancel = func() {}
	updated, _ := model.Update(permissionRequestMsg{request: PermissionRequest{
		Request:  permission.Request{Tool: "bash", Risk: tool.RiskExec, Mode: execution.ModeWork},
		Response: response,
	}, ok: true})
	model = assertModel(t, updated)

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatalf("esc returned nil command")
	}
	model = assertModel(t, updated)
	if model.pending != nil || model.Running() {
		t.Fatalf("permission interrupt left pending/running: pending=%v running=%v", model.pending != nil, model.Running())
	}
	select {
	case got := <-response:
		if got != permission.DecisionDeny {
			t.Fatalf("decision = %q, want deny", got)
		}
	default:
		t.Fatal("no permission decision returned")
	}
}

func TestStaleStreamEventsIgnoredAfterInterrupt(t *testing.T) {
	model := NewModel(Options{})
	model.running = true
	model.runID = 5
	model.interruptCurrent()

	updated, cmd := model.Update(streamEventMsg{
		event: Event{Type: EventDeltaText, Text: "stale"},
		ok:    true,
		runID: 5,
	})
	if cmd != nil {
		t.Fatalf("stale stream event returned command")
	}
	model = assertModel(t, updated)
	if got := model.MessageTexts(); len(got) != 0 {
		t.Fatalf("stale event changed messages: %#v", got)
	}
}

func TestStreamWaitTimeoutCancelsRun(t *testing.T) {
	cancelled := false
	events := make(chan Event)
	model := NewModel(Options{EventTimeout: time.Millisecond})
	model.running = true
	model.runID = 7
	model.events = events
	model.cancel = func() { cancelled = true }

	msg := waitForEventWithTimeout(events, model.runID, model.timeout)()
	updated, cmd := model.Update(msg)
	if cmd != nil {
		t.Fatalf("timeout event returned unexpected command")
	}
	model = assertModel(t, updated)
	if !cancelled || model.Running() {
		t.Fatalf("timeout did not cancel run: cancelled=%v running=%v", cancelled, model.Running())
	}
	if got := model.MessageTexts(); len(got) != 1 || !strings.Contains(got[0], "timed out") {
		t.Fatalf("messages = %#v, want timeout error", got)
	}
	if !strings.Contains(model.View(), "state: idle") {
		t.Fatalf("view missing idle state:\n%s", model.View())
	}
}

type scriptedRunner struct {
	events           []Event
	compactEvents    []Event
	calls            int
	compactCalls     int
	prompts          []string
	model            string
	models           []string
	effort           string
	efforts          []string
	approvalModel    string
	approvalModels   []string
	mode             string
	sessions         []SessionInfo
	sessionStates    map[string]SessionState
	currentSessionID string
}

func (r *scriptedRunner) Run(_ context.Context, prompt string, events chan<- Event) error {
	r.calls++
	r.prompts = append(r.prompts, prompt)
	for _, event := range r.events {
		events <- event
	}
	return nil
}

func (r *scriptedRunner) Compact(_ context.Context, events chan<- Event) error {
	r.compactCalls++
	for _, event := range r.compactEvents {
		events <- event
	}
	return nil
}

func (r *scriptedRunner) SetModel(model string) error {
	r.model = model
	return nil
}

func (r *scriptedRunner) SetMode(mode string) error {
	r.mode = mode
	return nil
}

func (r *scriptedRunner) Models() []string {
	return append([]string(nil), r.models...)
}

func (r *scriptedRunner) SetEffort(effort string) error {
	for _, candidate := range r.efforts {
		if candidate == effort {
			r.effort = effort
			return nil
		}
	}
	return fmt.Errorf("effort %q is not available", effort)
}

func (r *scriptedRunner) Effort() string {
	return r.effort
}

func (r *scriptedRunner) Efforts() []string {
	return append([]string(nil), r.efforts...)
}

func (r *scriptedRunner) SetApprovalModel(model string) error {
	r.approvalModel = model
	return nil
}

func (r *scriptedRunner) ApprovalModel() string {
	return r.approvalModel
}

func (r *scriptedRunner) ApprovalModels() []string {
	return append([]string(nil), r.approvalModels...)
}

func (r *scriptedRunner) ListSessions(context.Context) ([]SessionInfo, error) {
	return append([]SessionInfo(nil), r.sessions...), nil
}

func (r *scriptedRunner) SwitchSession(_ context.Context, id string) (SessionState, error) {
	state := r.sessionStates[id]
	r.currentSessionID = id
	if state.Model != "" {
		r.model = state.Model
	}
	return state, nil
}

func (r *scriptedRunner) CurrentSessionID() string {
	return r.currentSessionID
}

type promptOnlyRunner struct {
	calls int
}

func (r *promptOnlyRunner) Run(_ context.Context, _ string, _ chan<- Event) error {
	r.calls++
	return nil
}

func drainBatch(t *testing.T, model Model, cmd tea.Cmd) Model {
	t.Helper()
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("cmd returned %T, want tea.BatchMsg", cmd())
	}
	if len(batch) != 2 {
		t.Fatalf("batch len = %d, want 2", len(batch))
	}
	_ = batch[0]()
	msg := batch[1]()
	for {
		updated, next := model.Update(msg)
		model = assertModel(t, updated)
		if next == nil {
			return model
		}
		msg = next()
	}
}

func sendText(t *testing.T, model Model, text string) Model {
	t.Helper()
	for _, r := range text {
		updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		model = assertModel(t, updated)
	}
	return model
}

func assertModel(t *testing.T, model tea.Model) Model {
	t.Helper()
	m, ok := model.(Model)
	if !ok {
		t.Fatalf("model = %T, want tui.Model", model)
	}
	return m
}
