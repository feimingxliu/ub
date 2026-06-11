package tui

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	xansi "github.com/charmbracelet/x/ansi"

	"github.com/feimingxliu/ub/internal/app/ub/agent"
	"github.com/feimingxliu/ub/internal/app/ub/tui/tuitheme"
	"github.com/feimingxliu/ub/internal/pkg/core/execution"
	"github.com/feimingxliu/ub/internal/pkg/runtime/permission"
	"github.com/feimingxliu/ub/internal/pkg/tool"
	"github.com/feimingxliu/ub/internal/pkg/tool/plan"
)

func TestMain(m *testing.M) {
	oldProfile := lipgloss.Writer.Profile
	lipgloss.Writer.Profile = colorprofile.NoTTY
	code := m.Run()
	lipgloss.Writer.Profile = oldProfile
	os.Exit(code)
}

func TestInputUsesRealCursorForIMERendering(t *testing.T) {
	model := NewModel(Options{Model: "fake/test"})
	if model.input.VirtualCursor() {
		t.Fatalf("input should use a renderer-owned real cursor")
	}
	cur := model.input.Cursor()
	if cur == nil {
		t.Fatalf("input cursor is nil")
	}
	if cur.Blink {
		t.Fatalf("input cursor should not schedule virtual blink redraws")
	}
	assertInitRequestsWindowSizes(t, model, defaultViewWidth, defaultViewHeight)
}

func TestDetectInitialWindowSizeUsesEnvironmentWhenLarger(t *testing.T) {
	t.Setenv("COLUMNS", "160")
	t.Setenv("LINES", "48")

	width, height := detectInitialWindowSize(&bytes.Buffer{})
	if width != 160 || height != 48 {
		t.Fatalf("detectInitialWindowSize = %dx%d, want 160x48", width, height)
	}
}

func TestInitRequestsDetectedWindowSize(t *testing.T) {
	model := NewModel(Options{Model: "fake/test", initialWidth: 132, initialHeight: 37})
	assertInitRequestsWindowSizes(t, model, 132, 37)
}

func TestNarrowStartupWidthShowsWarning(t *testing.T) {
	model := NewModel(Options{Model: "fake/test", initialWidth: 72, initialHeight: 20})
	if got := model.MessageTexts(); len(got) != 1 || !strings.Contains(got[0], "terminal width is 72 columns") {
		t.Fatalf("messages = %#v, want narrow terminal warning", got)
	}
}

func TestInputViewFitsTerminalWidth(t *testing.T) {
	model := NewModel(Options{Model: "fake/test"})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 24, Height: 12})
	model = assertModel(t, updated)
	model.input.SetValue("正在输入中文")
	line := strings.Split(model.input.View(), "\n")[0]
	if got := lipgloss.Width(line); got > 24 {
		t.Fatalf("input line width = %d, want <= 24\n%s", got, line)
	}
}

func TestFooterKeepsStatusAtBottom(t *testing.T) {
	model := NewModel(Options{Model: "fake/test"})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 12})
	model = assertModel(t, updated)
	model = sendText(t, model, "!echo")

	view := viewString(model)
	lines := strings.Split(view, "\n")
	if !strings.Contains(lines[len(lines)-1], "state: idle") {
		t.Fatalf("status bar should remain at the bottom:\n%s", view)
	}
	if lineContaining(strings.Split(view, "\n"), "› !echo") < 0 {
		t.Fatalf("input line missing:\n%s", view)
	}
	if !strings.Contains(view, "shell mode") {
		t.Fatalf("shell hint should remain visible above input:\n%s", view)
	}
}

func TestViewCursorTracksInputLine(t *testing.T) {
	model := NewModel(Options{Model: "fake/test"})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 12})
	model = assertModel(t, updated)
	model = sendText(t, model, "hello")

	view := model.View()
	lines := strings.Split(view.Content, "\n")
	inputLine := lineContaining(lines, "› hello")
	if inputLine < 0 {
		t.Fatalf("input line missing:\n%s", view.Content)
	}
	if !view.AltScreen || view.MouseMode != tea.MouseModeCellMotion {
		t.Fatalf("view flags = alt:%v mouse:%v", view.AltScreen, view.MouseMode)
	}
	if view.Cursor == nil {
		t.Fatalf("view cursor is nil")
	}
	if view.Cursor.Y != inputLine {
		t.Fatalf("cursor Y = %d, want input line %d\n%s", view.Cursor.Y, inputLine, view.Content)
	}
	if view.Cursor.Y == len(lines)-1 {
		t.Fatalf("cursor should not point at status line:\n%s", view.Content)
	}
}

func TestInputCursorStaysEditableWithFooterAddons(t *testing.T) {
	model := NewModel(Options{Model: "fake/test"})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 14})
	model = assertModel(t, updated)
	model = sendText(t, model, "!echo")
	assertCursorOnInputLine(t, model, "› !echo")

	model.running = true
	model = sendText(t, model, " queued")
	updated, _ = model.Update(keyPress(tea.KeyEnter))
	model = assertModel(t, updated)
	assertCursorOnInputLine(t, model, "› ")

	model.running = false
	model.input.SetValue("")
	model = sendText(t, model, "/model")
	updated, _ = model.Update(keyPress(tea.KeyEnter))
	model = assertModel(t, updated)
	if view := model.View(); view.Cursor != nil {
		t.Fatalf("model picker should not expose an input cursor: %+v\n%s", view.Cursor, view.Content)
	}
}

func TestSubmitKeepsSingleFooterAndInputEditable(t *testing.T) {
	model := NewModel(Options{Model: "fake/test"})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 12})
	model = assertModel(t, updated)
	model = sendText(t, model, "hello")
	updated, cmd := model.Update(keyPress(tea.KeyEnter))
	if cmd != nil {
		t.Fatalf("enter returned unexpected command")
	}
	model = assertModel(t, updated)
	model = sendText(t, model, "next")

	view := model.View()
	if count := strings.Count(view.Content, "state: idle"); count != 1 {
		t.Fatalf("status footer count = %d, want 1\n%s", count, view.Content)
	}
	if view.Cursor == nil || lineContaining(strings.Split(view.Content, "\n"), "› next") != view.Cursor.Y {
		t.Fatalf("input cursor did not remain editable:\n%+v\n%s", view.Cursor, view.Content)
	}
}

func TestModelEchoesInputOnEnter(t *testing.T) {
	model := NewModel(Options{Model: "fake/test", ExecutionMode: "plan", Cwd: "/work"})
	model = sendText(t, model, "hello")

	updated, cmd := model.Update(keyPress(tea.KeyEnter))
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
	view := viewString(model)
	for _, want := range []string{"› hello", "model: fake/test", "mode: plan", "cwd: /work"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
}

func TestModelIgnoresEmptyEnter(t *testing.T) {
	model := NewModel(Options{})

	updated, cmd := model.Update(keyPress(tea.KeyEnter))
	if cmd != nil {
		t.Fatalf("enter returned unexpected command")
	}
	model = assertModel(t, updated)

	if got := model.MessageTexts(); len(got) != 0 {
		t.Fatalf("messages = %#v, want none", got)
	}
	if !strings.Contains(viewString(model), "No messages yet") {
		t.Fatalf("empty view missing placeholder:\n%s", viewString(model))
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

	updated, cmd := model.Update(keyPress(tea.KeyEnter))
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
	if !strings.Contains(viewString(model), "state: idle") {
		t.Fatalf("view missing idle state:\n%s", viewString(model))
	}
}

func TestModelShowsBackgroundActivityNotice(t *testing.T) {
	events := make(chan Event, 1)
	model := NewModel(Options{BackgroundEvents: events})
	updated, cmd := model.handleBackgroundEvent(backgroundEventMsg{
		event: Event{Type: EventActivity, ActivityKind: "notice", Status: "done", Summary: "memory wrote: build command is `make build`"},
		ok:    true,
	})
	model = assertModel(t, updated)
	if got := model.MessageTexts(); !reflect.DeepEqual(got, []string{"memory wrote: build command is `make build`"}) {
		t.Fatalf("messages = %#v", got)
	}
	if cmd == nil {
		t.Fatalf("background handler did not continue waiting for events")
	}
	events <- Event{Type: EventActivity, Summary: "next background notice"}
	msg, ok := cmd().(backgroundEventMsg)
	if !ok || !msg.ok || msg.event.Summary != "next background notice" {
		t.Fatalf("next background msg = %#v", msg)
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
	if !strings.Contains(viewString(model), "ctx est: 1200/8000 15%") {
		t.Fatalf("view missing context usage:\n%s", viewString(model))
	}

	updated, _ = model.Update(streamEventMsg{
		runID: 1,
		ok:    true,
		event: Event{Type: EventContext, ContextUsedTokens: 800, ContextMaxTokens: 8000, ContextRatio: 0.10},
	})
	model = assertModel(t, updated)
	if !strings.Contains(viewString(model), "ctx est: 1200/8000 15%") {
		t.Fatalf("context usage should not shrink without reset:\n%s", viewString(model))
	}

	updated, _ = model.Update(streamEventMsg{
		runID: 1,
		ok:    true,
		event: Event{Type: EventContext, ContextUsedTokens: 800, ContextMaxTokens: 8000, ContextRatio: 0.10, ContextReset: true},
	})
	model = assertModel(t, updated)
	if !strings.Contains(viewString(model), "ctx est: 800/8000 10%") {
		t.Fatalf("context usage should shrink after reset:\n%s", viewString(model))
	}

	updated, _ = model.Update(streamEventMsg{
		runID: 1,
		ok:    true,
		event: Event{Type: EventContext, ContextUsedTokens: 1200},
	})
	model = assertModel(t, updated)
	view := viewString(model)
	if !strings.Contains(view, "ctx est: 1200") || strings.Contains(view, "ctx est: 1200/") {
		t.Fatalf("view should show used tokens without unknown max:\n%s", view)
	}
}

func TestModelLabelsProviderUsageAsLastContext(t *testing.T) {
	model := NewModel(Options{Model: "fake/test"})
	model.running = true
	model.runID = 1
	model.events = make(chan Event)

	updated, _ := model.Update(streamEventMsg{
		runID: 1,
		ok:    true,
		event: Event{Type: EventContext, ContextUsedTokens: 1400, ContextMaxTokens: 8000, ContextRatio: 0.175, ContextKind: "last"},
	})
	model = assertModel(t, updated)
	if !strings.Contains(viewString(model), "ctx last: 1400/8000 18%") {
		t.Fatalf("view missing last context label:\n%s", viewString(model))
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
	if !model.Running() || !strings.Contains(viewString(model), "state: finalizing") {
		t.Fatalf("done should keep run finalizing: running=%v view=\n%s", model.Running(), viewString(model))
	}

	close(events)
	updated, cmd = model.Update(cmd())
	if cmd != nil {
		t.Fatalf("closed stream returned unexpected command")
	}
	model = assertModel(t, updated)
	if model.Running() || !strings.Contains(viewString(model), "state: idle") {
		t.Fatalf("closed stream should return idle: running=%v view=\n%s", model.Running(), viewString(model))
	}
}

func TestModelIgnoresSecondEnterWhileRunning(t *testing.T) {
	runner := &scriptedRunner{}
	model := NewModel(Options{Runner: runner})
	model = sendText(t, model, "first")

	updated, cmd := model.Update(keyPress(tea.KeyEnter))
	if cmd == nil {
		t.Fatalf("first enter returned nil command")
	}
	model = assertModel(t, updated)

	updated, secondCmd := model.Update(keyPress(tea.KeyEnter))
	model = assertModel(t, updated)
	if secondCmd != nil {
		t.Fatalf("second enter returned unexpected command")
	}
	if got, want := model.MessageTexts(), []string{"first", "Thinking..."}; !reflect.DeepEqual(got, want) {
		t.Fatalf("messages = %#v, want %#v", got, want)
	}
}

func TestModelQueuesPromptWhileRunningAndStartsAfterCurrentRun(t *testing.T) {
	runner := &scriptedRunner{events: []Event{
		{Type: EventDeltaText, Text: "queued response"},
		{Type: EventDone},
	}}
	model := NewModel(Options{Runner: runner})
	model = sendText(t, model, "first")

	updated, cmd := model.Update(keyPress(tea.KeyEnter))
	if cmd == nil {
		t.Fatalf("first enter returned nil command")
	}
	model = assertModel(t, updated)
	firstRunID := model.runID

	model = sendText(t, model, "second")
	updated, cmd = model.Update(keyPress(tea.KeyEnter))
	if cmd != nil {
		t.Fatalf("queued enter returned unexpected command")
	}
	model = assertModel(t, updated)
	if got, want := model.QueuedPrompts(), []string{"second"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("queued prompts = %#v, want %#v", got, want)
	}
	if got := model.InputValue(); got != "" {
		t.Fatalf("input = %q, want empty after queueing", got)
	}
	if !strings.Contains(viewString(model), "queued: 1") || !strings.Contains(viewString(model), "next: second") {
		t.Fatalf("view missing queued prompt:\n%s", viewString(model))
	}

	updated, cmd = model.Update(streamEventMsg{runID: firstRunID, ok: true, event: Event{Type: EventDone}})
	model = assertModel(t, updated)
	if cmd == nil {
		t.Fatal("done should wait for stream close")
	}
	updated, cmd = model.Update(streamEventMsg{runID: firstRunID, ok: false})
	model = assertModel(t, updated)
	if cmd == nil {
		t.Fatal("stream close should start queued prompt")
	}
	if got := model.QueuedPrompts(); len(got) != 0 {
		t.Fatalf("queued prompts after start = %#v, want empty", got)
	}
	if got := model.MessageTexts(); len(got) < 2 || got[0] != "first" || got[1] != "second" {
		t.Fatalf("messages after dequeuing = %#v, want first then second", got)
	}

	model = drainBatch(t, model, cmd)
	if runner.calls != 1 || runner.prompts[0] != "second" {
		t.Fatalf("runner calls/prompts = %d/%#v, want queued second", runner.calls, runner.prompts)
	}
	if got := model.MessageTexts(); len(got) != 3 || got[2] != "queued response" {
		t.Fatalf("messages after queued run = %#v", got)
	}
}

func TestModelEditsQueuedPromptsWithArrowKeys(t *testing.T) {
	model := NewModel(Options{Runner: &scriptedRunner{}})
	model.running = true
	model = sendText(t, model, "first queued")
	updated, _ := model.Update(keyPress(tea.KeyEnter))
	model = assertModel(t, updated)
	model = sendText(t, model, "second queued")
	updated, _ = model.Update(keyPress(tea.KeyEnter))
	model = assertModel(t, updated)

	updated, cmd := model.Update(keyPress(tea.KeyUp))
	if cmd != nil {
		t.Fatalf("up returned unexpected command")
	}
	model = assertModel(t, updated)
	if got := model.InputValue(); got != "second queued" {
		t.Fatalf("up input = %q, want second queued", got)
	}
	model.input.SetValue("second edited")

	updated, _ = model.Update(keyPress(tea.KeyUp))
	model = assertModel(t, updated)
	if got := model.InputValue(); got != "first queued" {
		t.Fatalf("second up input = %q, want first queued", got)
	}
	if got, want := model.QueuedPrompts(), []string{"first queued", "second edited"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("queued after editing second = %#v, want %#v", got, want)
	}
	model.input.SetValue("first edited")

	updated, _ = model.Update(keyPress(tea.KeyDown))
	model = assertModel(t, updated)
	if got := model.InputValue(); got != "second edited" {
		t.Fatalf("down input = %q, want second edited", got)
	}
	if got, want := model.QueuedPrompts(), []string{"first edited", "second edited"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("queued after editing first = %#v, want %#v", got, want)
	}

	updated, _ = model.Update(keyPress(tea.KeyDown))
	model = assertModel(t, updated)
	if got := model.InputValue(); got != "" {
		t.Fatalf("second down input = %q, want empty draft", got)
	}
	if got, want := model.QueuedPrompts(), []string{"first edited", "second edited"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("queued prompts = %#v, want %#v", got, want)
	}
}

func TestModelSavesQueuedEditOnEnterWhileRunning(t *testing.T) {
	model := NewModel(Options{Runner: &scriptedRunner{}})
	model.running = true
	model = sendText(t, model, "queued")
	updated, _ := model.Update(keyPress(tea.KeyEnter))
	model = assertModel(t, updated)

	updated, _ = model.Update(keyPress(tea.KeyUp))
	model = assertModel(t, updated)
	model.input.SetValue("queued edited")
	updated, cmd := model.Update(keyPress(tea.KeyEnter))
	if cmd != nil {
		t.Fatalf("edit enter returned unexpected command")
	}
	model = assertModel(t, updated)
	if got := model.InputValue(); got != "" {
		t.Fatalf("input = %q, want empty after saving edit", got)
	}
	if got, want := model.QueuedPrompts(), []string{"queued edited"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("queued prompts = %#v, want %#v", got, want)
	}
	if !strings.Contains(viewString(model), "next: queued edited") {
		t.Fatalf("view missing edited queued prompt:\n%s", viewString(model))
	}
}

func TestModelRemovesQueuedPromptImmediatelyWhenEditBecomesEmpty(t *testing.T) {
	model := NewModel(Options{Runner: &scriptedRunner{}})
	model.running = true
	model = sendText(t, model, "queued")
	updated, _ := model.Update(keyPress(tea.KeyEnter))
	model = assertModel(t, updated)
	updated, _ = model.Update(keyPress(tea.KeyUp))
	model = assertModel(t, updated)
	if got := model.InputValue(); got != "queued" {
		t.Fatalf("input = %q, want queued", got)
	}

	for range "queued" {
		updated, _ = model.Update(keyPress(tea.KeyBackspace))
		model = assertModel(t, updated)
	}
	if got := model.InputValue(); got != "" {
		t.Fatalf("input = %q, want restored empty draft", got)
	}
	if got := model.QueuedPrompts(); len(got) != 0 {
		t.Fatalf("queued prompts = %#v, want empty after deleting edit", got)
	}
	if strings.Contains(viewString(model), "queued:") || strings.Contains(viewString(model), "next: queued") {
		t.Fatalf("view still shows deleted queued prompt:\n%s", viewString(model))
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

	updated, cmd := model.Update(keyPress(tea.KeyEnter))
	model = assertModel(t, updated)
	model = drainBatch(t, model, cmd)

	view := viewString(model)
	for _, want := range []string{"tool read started", "tool read finished"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
}

func TestToastShowsToolResultFeedback(t *testing.T) {
	model := NewModel(Options{})
	updated, _ := model.Update(streamEventMsg{
		event: Event{Type: EventActivity, ActivityKind: "tool", ToolName: "read", Status: "done"},
		ok:    true,
		runID: model.runID,
	})
	model = assertModel(t, updated)
	if !strings.Contains(viewString(model), "ok: tool read succeeded") {
		t.Fatalf("view missing success toast:\n%s", viewString(model))
	}

	updated, _ = model.Update(streamEventMsg{
		event: Event{Type: EventActivity, ActivityKind: "tool", ToolName: "write", Status: "failed", IsError: true},
		ok:    true,
		runID: model.runID,
	})
	model = assertModel(t, updated)
	if !strings.Contains(viewString(model), "error: tool write failed") {
		t.Fatalf("view missing failure toast:\n%s", viewString(model))
	}
}

func TestToolCallEndDoesNotDoubleToast(t *testing.T) {
	model := NewModel(Options{})
	updated, _ := model.Update(streamEventMsg{
		event: Event{Type: EventActivity, ActivityKind: "tool", ToolName: "read", Status: "done"},
		ok:    true,
		runID: model.runID,
	})
	model = assertModel(t, updated)
	beforeGen := model.toast.generation
	updated, _ = model.Update(streamEventMsg{
		event: Event{Type: EventToolCallEnd, ToolName: "read"},
		ok:    true,
		runID: model.runID,
	})
	model = assertModel(t, updated)
	if model.toast.generation != beforeGen {
		t.Fatalf("EventToolCallEnd should not push a second toast (generation %d -> %d)", beforeGen, model.toast.generation)
	}
}

func TestToastShowsApprovalFeedbackAndClearsOnInput(t *testing.T) {
	response := make(chan permission.Decision, 1)
	model := NewModel(Options{})
	updated, _ := model.Update(permissionRequestMsg{
		request: PermissionRequest{
			Request:  permission.Request{Tool: "bash"},
			Response: response,
		},
		ok: true,
	})
	model = assertModel(t, updated)

	updated, _ = model.Update(keyPress(tea.KeyEnter))
	model = assertModel(t, updated)
	if !strings.Contains(viewString(model), "ok: approval allowed bash") {
		t.Fatalf("view missing approval toast:\n%s", viewString(model))
	}

	updated, _ = model.Update(runePress('x'))
	model = assertModel(t, updated)
	if strings.Contains(viewString(model), "ok: approval allowed bash") {
		t.Fatalf("toast did not clear on input:\n%s", viewString(model))
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

	updated, cmd := model.Update(keyPress(tea.KeyEnter))
	model = assertModel(t, updated)
	model = drainBatch(t, model, cmd)

	view := viewString(model)
	for _, want := range []string{
		"checking repository context",
		"Read path=main.go",
		"Permission",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
	// Verify the full permission text is available even if the view truncates it.
	texts := model.MessageTexts()
	permFound := false
	for _, txt := range texts {
		if strings.Contains(txt, "Permission approval_agent allow bash") {
			permFound = true
		}
	}
	if !permFound {
		t.Fatalf("MessageTexts missing full permission text: %#v", texts)
	}
	for i := range model.messages.items {
		if model.messages.items[i].collapsible() {
			model.messages.items[i].collapsed = false
		}
	}
	model.messages.invalidateRender()
	view = viewString(model)
	for _, want := range []string{
		"checking repository context",
		"Read path=main.go",
		"Permission approval_agent allow bash: read-only command",
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

func TestActivityEventsSplitThinkingAndTools(t *testing.T) {
	model := NewModel(Options{})
	model.running = true
	model.runID = 6
	model.events = make(chan Event)

	for _, event := range []Event{
		{Type: EventActivity, ActivityKind: "thinking", Summary: "checking repository", Content: "checking repository"},
		{Type: EventActivity, ActivityKind: "tool", ToolUseID: "call_1", ToolName: "read", Status: "done", Summary: "path=main.go", Content: "file content"},
	} {
		updated, cmd := model.Update(streamEventMsg{runID: 6, ok: true, event: event})
		if cmd == nil {
			t.Fatal("activity event should continue waiting for stream events")
		}
		model = assertModel(t, updated)
	}

	got := model.MessageTexts()
	if len(got) != 2 {
		t.Fatalf("messages = %#v, want 2 individual activity items (thinking + tool)", got)
	}
	if !strings.HasPrefix(got[0], "thinking: checking repository") || got[1] != "Read path=main.go" {
		t.Fatalf("messages = %#v, want thinking and tool items", got)
	}
}

func TestLoadScopesActivityUpdatesByTurn(t *testing.T) {
	model := NewModel(Options{Messages: []InitialMessage{
		{Role: userRole, Turn: 1, Text: "first prompt"},
		{Turn: 1, ActivityKind: "thinking", Summary: "first thought", Content: "first thought"},
		{Turn: 1, ActivityKind: "tool", ToolUseID: "call_1", ToolName: "read", Status: "queued", Summary: "path=one.go"},
		{Turn: 1, ActivityKind: "tool", ToolUseID: "call_1", ToolName: "read", Status: "done", Summary: "path=one.go", Content: "one"},
		{Role: assistantRole, Turn: 1, Text: "first answer"},
		{Role: userRole, Turn: 2, Text: "second prompt"},
		{Turn: 2, ActivityKind: "thinking", Summary: "second thought", Content: "second thought"},
		{Turn: 2, ActivityKind: "tool", ToolUseID: "call_1", ToolName: "read", Status: "queued", Summary: "path=two.go"},
		{Turn: 2, ActivityKind: "tool", ToolUseID: "call_1", ToolName: "read", Status: "done", Summary: "path=two.go", Content: "two"},
		{Role: assistantRole, Turn: 2, Text: "second answer"},
	}})

	got := model.MessageTexts()
	joined := strings.Join(got, "\n")
	for _, want := range []string{
		"thinking: first thought",
		"Read path=one.go",
		"thinking: second thought",
		"Read path=two.go",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("messages = %#v, missing %q", got, want)
		}
	}
	if strings.Count(joined, "thinking:") != 2 {
		t.Fatalf("messages = %#v, want two separate thinking items", got)
	}
	if strings.Count(joined, "Read path=") != 2 {
		t.Fatalf("messages = %#v, want duplicate tool IDs separated by turn", got)
	}
}

func TestLiveActivityUpdatesAreScopedByTurn(t *testing.T) {
	runner := &scriptedRunner{events: []Event{
		{Type: EventActivity, ActivityKind: "thinking", Summary: "checking context", Content: "checking context"},
		{Type: EventDone},
	}}
	model := NewModel(Options{Runner: runner})

	for _, prompt := range []string{"first prompt", "second prompt"} {
		model = sendText(t, model, prompt)
		updated, cmd := model.Update(keyPress(tea.KeyEnter))
		if cmd == nil {
			t.Fatal("enter returned nil command")
		}
		model = assertModel(t, updated)
		model = drainBatch(t, model, cmd)
	}

	got := model.MessageTexts()
	joined := strings.Join(got, "\n")
	if count := strings.Count(joined, "thinking: checking context"); count != 2 {
		t.Fatalf("messages = %#v, want two separate live thinking blocks", got)
	}
}

func TestToolGroupMixedFailureDoesNotFailWholeGroup(t *testing.T) {
	model := NewModel(Options{})
	model.running = true
	model.runID = 7
	model.events = make(chan Event)

	for _, event := range []Event{
		{Type: EventActivity, ActivityKind: "tool", ToolUseID: "call_read", ToolName: "read", Status: "done", Summary: "path=main.go"},
		{Type: EventActivity, ActivityKind: "tool", ToolUseID: "call_bash", ToolName: "bash", Status: "failed", Summary: "cmd=go test ./...", IsError: true},
	} {
		updated, cmd := model.Update(streamEventMsg{runID: 7, ok: true, event: event})
		if cmd == nil {
			t.Fatal("activity event should continue waiting for stream events")
		}
		model = assertModel(t, updated)
	}

	if len(model.messages.items) != 2 {
		t.Fatalf("messages = %#v, want two separate tool items", model.MessageTexts())
	}
	readItem := model.messages.items[0]
	bashItem := model.messages.items[1]
	if readItem.status != "done" {
		t.Fatalf("read tool status = %q, want done", readItem.status)
	}
	if bashItem.status != "failed" {
		t.Fatalf("bash tool status = %q, want failed", bashItem.status)
	}
	got := model.MessageTexts()
	if len(got) != 2 || got[0] != "Read path=main.go" || got[1] != "Ran cmd=go test ./... failed" {
		t.Fatalf("messages = %#v, want individual done and failed tool items", got)
	}
	readItem.collapsed = false
	bashItem.collapsed = false
	model.messages.items[0] = readItem
	model.messages.items[1] = bashItem
	view := viewString(model)
	for _, want := range []string{"✓ Read path=main.go", "× Ran cmd=go test ./... failed"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
}

func TestFailedToolCollapsedTitleHidesDetail(t *testing.T) {
	model := NewModel(Options{})
	model.running = true
	model.runID = 7
	model.events = make(chan Event)
	content := "<shell_metadata>\nexit_code=1\nduration_ms=12\n</shell_metadata>\n--- stdout ---\nok\n--- stderr ---\nfailed\n"

	updated, cmd := model.Update(streamEventMsg{runID: 7, ok: true, event: Event{
		Type:         EventActivity,
		ActivityKind: "tool",
		ToolUseID:    "call_bash",
		ToolName:     "bash",
		IsError:      true,
		Summary:      "cmd=go test ./...",
		Content:      content,
	}})
	if cmd == nil {
		t.Fatal("activity event should continue waiting for stream events")
	}
	model = assertModel(t, updated)

	if len(model.messages.items) != 1 {
		t.Fatalf("messages = %#v, want one tool item", model.MessageTexts())
	}
	item := model.messages.items[0]
	if item.status != "failed" {
		t.Fatalf("tool status = %q, want failed", item.status)
	}
	if got := model.MessageTexts(); len(got) != 1 || got[0] != "Ran cmd=go test ./... failed" {
		t.Fatalf("messages = %#v, want failed title without detail", got)
	}
	collapsed := viewString(model)
	for _, unexpected := range []string{"exit_code=1", "duration_ms=12", "--- stdout ---", "--- stderr ---"} {
		if strings.Contains(collapsed, unexpected) {
			t.Fatalf("collapsed failed tool leaked %q:\n%s", unexpected, collapsed)
		}
	}

	model.messages.items[0].collapsed = false
	model.messages.invalidateRender()
	expanded := viewString(model)
	for _, want := range []string{"× Ran cmd=go test ./... failed", "exit_code=1", "duration_ms=12", "--- stdout ---", "--- stderr ---", "failed"} {
		if !strings.Contains(expanded, want) {
			t.Fatalf("expanded failed tool missing %q:\n%s", want, expanded)
		}
	}
}

func TestTodoActivityRendersStandaloneChecklist(t *testing.T) {
	model := NewModel(Options{})
	model.running = true
	model.runID = 7
	model.events = make(chan Event)
	content := "session_id=sess_1\ntodo_count=3\n\n## Todo\n\n- [x] 1. inspect code\n- [>] 2. patch files {id=patch}\n- [!] 3. run tests - validation failed\n"

	updated, cmd := model.Update(streamEventMsg{runID: 7, ok: true, event: Event{
		Type:         EventActivity,
		ActivityKind: "tool",
		ToolUseID:    "call_todo",
		ToolName:     "todo_update",
		Status:       "done",
		Summary:      "id=patch, status=in_progress",
		Content:      content,
	}})
	if cmd == nil {
		t.Fatal("activity event should continue waiting for stream events")
	}
	model = assertModel(t, updated)
	if len(model.messages.items) != 2 {
		t.Fatalf("message item count = %d, want tool audit plus standalone todo", len(model.messages.items))
	}
	if model.messages.items[0].kind != toolMessage || model.messages.items[0].detail != "" {
		t.Fatalf("first item = %#v, want tool audit without todo detail", model.messages.items[0])
	}
	if model.messages.items[1].kind != todoMessage {
		t.Fatalf("second item kind = %s, want todo", model.messages.items[1].kind)
	}

	view := viewString(model)
	for _, want := range []string{"Todo: 1 running, 1 done, 1 failed", "[x] inspect code", "[>] patch files {id=patch}", "[!] run tests - validation failed"} {
		if !strings.Contains(view, want) {
			t.Fatalf("todo view missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "session_id=sess_1") || strings.Contains(view, "todo_count=3") {
		t.Fatalf("todo view should hide metadata:\n%s", view)
	}
}

func TestTodoActivityUpdatesStandaloneChecklistInPlace(t *testing.T) {
	model := NewModel(Options{})
	model.running = true
	model.runID = 7
	model.events = make(chan Event)
	first := "session_id=sess_1\ntodo_count=2\n\n## Todo\n\n- [>] 1. inspect\n- [ ] 2. patch\n"
	second := "session_id=sess_1\ntodo_count=2\n\n## Todo\n\n- [x] 1. inspect\n- [>] 2. patch\n"

	updated, cmd := model.Update(streamEventMsg{runID: 7, ok: true, event: Event{
		Type:         EventActivity,
		ActivityKind: "tool",
		ToolUseID:    "call_todo_1",
		ToolName:     "todo_write",
		Status:       "done",
		Summary:      "items=2",
		Content:      first,
	}})
	model = assertModel(t, updated)
	if cmd == nil {
		t.Fatal("first todo activity should continue waiting")
	}
	updated, cmd = model.Update(streamEventMsg{runID: 7, ok: true, event: Event{
		Type:         EventActivity,
		ActivityKind: "tool",
		ToolUseID:    "call_todo_2",
		ToolName:     "todo_update",
		Status:       "done",
		Summary:      "id=patch, status=in_progress",
		Content:      second,
	}})
	model = assertModel(t, updated)
	if cmd == nil {
		t.Fatal("second todo activity should continue waiting")
	}

	todoCount := 0
	for _, item := range model.messages.items {
		if item.kind == todoMessage {
			todoCount++
			if !strings.Contains(item.detail, "- [>] 2. patch") {
				t.Fatalf("todo detail not updated in place:\n%s", item.detail)
			}
		}
	}
	if todoCount != 1 {
		t.Fatalf("todo block count = %d, want 1; items=%#v", todoCount, model.messages.items)
	}
}

func TestTodoWriteMovesStandaloneChecklistToLatestPosition(t *testing.T) {
	model := NewModel(Options{})
	model.running = true
	model.runID = 7
	model.events = make(chan Event)
	first := "session_id=sess_1\ntodo_count=1\n\n## Todo\n\n- [>] 1. inspect\n"
	second := "session_id=sess_1\ntodo_count=2\n\n## Todo\n\n- [>] 1. patch\n- [ ] 2. test\n"

	updated, cmd := model.Update(streamEventMsg{runID: 7, ok: true, event: Event{
		Type:         EventActivity,
		ActivityKind: "tool",
		ToolUseID:    "call_todo_1",
		ToolName:     "todo_write",
		Status:       "done",
		Summary:      "items=1",
		Content:      first,
	}})
	model = assertModel(t, updated)
	if cmd == nil {
		t.Fatal("first todo activity should continue waiting")
	}
	model.messages.append(userRole, "later prompt")
	model.messages.append(assistantRole, "later answer")

	updated, cmd = model.Update(streamEventMsg{runID: 7, ok: true, event: Event{
		Type:         EventActivity,
		ActivityKind: "tool",
		ToolUseID:    "call_todo_2",
		ToolName:     "todo_write",
		Status:       "done",
		Summary:      "items=2",
		Content:      second,
	}})
	model = assertModel(t, updated)
	if cmd == nil {
		t.Fatal("second todo activity should continue waiting")
	}

	todoCount := 0
	for _, item := range model.messages.items {
		if item.kind != todoMessage {
			continue
		}
		todoCount++
		if strings.Contains(item.detail, "inspect") || !strings.Contains(item.detail, "- [>] 1. patch") {
			t.Fatalf("todo detail = %q, want latest list only", item.detail)
		}
	}
	if todoCount != 1 {
		t.Fatalf("todo block count = %d, want 1; items=%#v", todoCount, model.messages.items)
	}
	last := model.messages.items[len(model.messages.items)-1]
	if last.kind != todoMessage {
		t.Fatalf("last item = %#v, want latest todo block at current position", last)
	}
}

func TestTodoActivityRestoresStandaloneChecklistFromHistory(t *testing.T) {
	content := "session_id=sess_1\ntodo_count=1\n\n## Todo\n\n- [>] 1. patch\n"
	model := NewModel(Options{Messages: []InitialMessage{{
		Turn:         1,
		ActivityKind: "tool",
		ToolUseID:    "call_todo",
		ToolName:     "todo_write",
		Status:       "done",
		Summary:      "items=1",
		Content:      content,
	}}})
	if len(model.messages.items) != 2 || model.messages.items[1].kind != todoMessage {
		t.Fatalf("restored items = %#v, want tool audit plus todo", model.messages.items)
	}
	if view := viewString(model); !strings.Contains(view, "[>] patch") {
		t.Fatalf("restored todo missing checklist:\n%s", view)
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
	if len(got) != 1 || got[0] != "Read path=main.go" {
		t.Fatalf("messages = %#v, want single updated tool activity", got)
	}
	if model.messages.items[0].status != "done" {
		t.Fatalf("tool status = %q, want done", model.messages.items[0].status)
	}
}

func TestToolPartialOutputUpdatesRunningToolDetail(t *testing.T) {
	model := NewModel(Options{})
	model.running = true
	model.runID = 3
	model.events = make(chan Event)

	for _, event := range []Event{
		{Type: EventActivity, ActivityKind: "tool", ToolUseID: "call_1", ToolName: "bash", Status: "running", Summary: "cmd=go test ./..."},
		{Type: EventToolPartialOutput, ToolUseID: "call_1", ToolName: "bash", Status: "stdout", Summary: "cmd=go test ./...", Content: "one\n"},
		{Type: EventToolPartialOutput, ToolUseID: "call_1", ToolName: "bash", Status: "stdout", Summary: "cmd=go test ./...", Content: "two\n"},
	} {
		updated, cmd := model.Update(streamEventMsg{runID: 3, ok: true, event: event})
		if cmd == nil {
			t.Fatal("tool partial event should continue waiting for stream events")
		}
		model = assertModel(t, updated)
	}

	got := model.MessageTexts()
	if len(got) != 1 || got[0] != "Writing command... cmd=go test ./..." {
		t.Fatalf("messages = %#v, want one running tool activity", got)
	}
	item := model.messages.items[0]
	if item.detail != "one\ntwo\n" {
		t.Fatalf("tool partial detail = %q, want accumulated chunks", item.detail)
	}
}

func TestToolInputDetailDoesNotDuplicateBeforePartialOutput(t *testing.T) {
	model := NewModel(Options{})
	model.running = true
	model.runID = 3
	model.events = make(chan Event)
	commandDetail := "command:\ngo test ./...\nprintf 'done'"

	for _, event := range []Event{
		{Type: EventActivity, ActivityKind: "tool", ToolUseID: "call_1", ToolName: "bash", Status: "queued", Summary: "cmd=go test ./...", Content: commandDetail},
		{Type: EventActivity, ActivityKind: "tool", ToolUseID: "call_1", ToolName: "bash", Status: "running", Summary: "cmd=go test ./...", Content: commandDetail},
		{Type: EventToolPartialOutput, ToolUseID: "call_1", ToolName: "bash", Status: "stdout", Summary: "cmd=go test ./...", Content: "ok\n"},
	} {
		updated, cmd := model.Update(streamEventMsg{runID: 3, ok: true, event: event})
		if cmd == nil {
			t.Fatal("tool activity event should continue waiting for stream events")
		}
		model = assertModel(t, updated)
	}

	item := model.messages.items[0]
	if strings.Count(item.detail, "command:\n") != 1 {
		t.Fatalf("tool input detail duplicated:\n%s", item.detail)
	}
	if item.detail != commandDetail+"\nok\n" {
		t.Fatalf("tool detail = %q, want command detail followed by partial output", item.detail)
	}
}

func TestToolPartialOutputSurvivesGenericFinalDetail(t *testing.T) {
	model := NewModel(Options{})
	model.running = true
	model.runID = 3
	model.events = make(chan Event)

	for _, event := range []Event{
		{Type: EventActivity, ActivityKind: "tool", ToolUseID: "call_1", ToolName: "bash", Status: "running", Summary: "cmd=go test ./..."},
		{Type: EventToolPartialOutput, ToolUseID: "call_1", ToolName: "bash", Status: "stdout", Summary: "cmd=go test ./...", Content: "one\n"},
		{Type: EventActivity, ActivityKind: "tool", ToolUseID: "call_1", ToolName: "bash", Status: "done", Summary: "cmd=go test ./...", Content: "<shell_metadata>"},
	} {
		updated, cmd := model.Update(streamEventMsg{runID: 3, ok: true, event: event})
		if cmd == nil {
			t.Fatal("activity event should continue waiting for stream events")
		}
		model = assertModel(t, updated)
	}

	got := model.MessageTexts()
	if len(got) != 1 || got[0] != "Ran cmd=go test ./..." {
		t.Fatalf("messages = %#v, want one completed tool activity", got)
	}
	item := model.messages.items[0]
	if item.status != "done" {
		t.Fatalf("tool status = %q, want done", item.status)
	}
	if item.detail != "one\n" {
		t.Fatalf("tool detail = %q, want preserved partial output", item.detail)
	}
}

func TestThinkingActivityDeltasAccumulateInGroup(t *testing.T) {
	model := NewModel(Options{})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	model = assertModel(t, updated)
	model.running = true
	model.runID = 4
	model.events = make(chan Event)

	for _, event := range []Event{
		{Type: EventActivity, ActivityKind: "thinking", Summary: "checking repository", Content: "checking repository "},
		{Type: EventActivity, ActivityKind: "thinking", Summary: "context", Content: "context "},
		{Type: EventActivity, ActivityKind: "thinking", Summary: "before reading files", Content: "before reading files"},
	} {
		updated, cmd := model.Update(streamEventMsg{runID: 4, ok: true, event: event})
		if cmd == nil {
			t.Fatal("thinking event should continue waiting for stream events")
		}
		model = assertModel(t, updated)
	}

	got := model.MessageTexts()
	if len(got) != 1 || !strings.Contains(got[0], "checking repository context before reading files") {
		t.Fatalf("messages = %#v, want accumulated thinking summary", got)
	}
	model.messages.items[0].collapsed = false
	if view := viewString(model); !strings.Contains(view, "checking repository context before reading files") {
		t.Fatalf("expanded view missing accumulated thinking detail:\n%s", view)
	}
}

func TestThinkingActivityPreservesParagraphBreaks(t *testing.T) {
	model := NewModel(Options{})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	model = assertModel(t, updated)
	model.running = true
	model.runID = 11
	model.events = make(chan Event)

	for _, event := range []Event{
		{Type: EventActivity, ActivityKind: "thinking", Summary: "first paragraph", Content: "first paragraph"},
		{Type: EventActivity, ActivityKind: "thinking", Summary: "", Content: "\n\n"},
		{Type: EventActivity, ActivityKind: "thinking", Summary: "second paragraph", Content: "second paragraph"},
	} {
		updated, _ = model.Update(streamEventMsg{runID: 11, ok: true, event: event})
		model = assertModel(t, updated)
	}

	if len(model.messages.items) != 1 {
		t.Fatalf("messages = %#v, want one thinking item", model.messages.items)
	}
	detail := model.messages.items[0].detail
	if !strings.Contains(detail, "first paragraph\n\nsecond paragraph") {
		t.Fatalf("paragraph break lost in detail: %q", detail)
	}
	model.messages.items[0].collapsed = false
	view := viewString(model)
	lines := strings.Split(view, "\n")
	firstIdx, secondIdx := -1, -1
	for i, line := range lines {
		// Skip the title line ("▾ … thinking: …") so we measure the gap
		// inside the rendered detail, not the header.
		if strings.Contains(line, "thinking:") {
			continue
		}
		if firstIdx < 0 && strings.Contains(line, "first paragraph") {
			firstIdx = i
		}
		if firstIdx >= 0 && secondIdx < 0 && strings.Contains(line, "second paragraph") {
			secondIdx = i
		}
	}
	if firstIdx < 0 || secondIdx < 0 || secondIdx-firstIdx < 2 {
		t.Fatalf("expected blank line between paragraphs (first=%d second=%d):\n%s", firstIdx, secondIdx, view)
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
	if len(got) != 3 {
		t.Fatalf("messages = %#v, want three separate tool items", got)
	}
	if got[0] != "Read path=main.go" {
		t.Fatalf("first item = %q, want Read path=main.go", got[0])
	}
	if got[1] != "Writing command... cmd=go test ./..." {
		t.Fatalf("second item = %q, want running bash tool", got[1])
	}
	if got[2] != "Searching content... pattern=TODO" {
		t.Fatalf("third item = %q, want queued grep tool", got[2])
	}
}

func TestMarkdownMessagesRenderReadableBlocks(t *testing.T) {
	model := NewModel(Options{Messages: []InitialMessage{{
		Role: assistantRole,
		Text: "# Plan\n\n- inspect repository\n- patch renderer\n\n```go\nfmt.Println(\"ok\")\n```",
	}}})

	view := viewString(model)
	for _, want := range []string{"Plan", "inspect repository", "patch renderer", `fmt.Println("ok")`} {
		if !strings.Contains(view, want) {
			t.Fatalf("markdown view missing %q:\n%s", want, view)
		}
	}
}

func TestModelUsesConfiguredMarkdownTheme(t *testing.T) {
	model := NewModel(Options{Theme: "light"})
	if got := model.styles.Markdown.StyleName; got != "light" {
		t.Fatalf("markdown style = %q, want light", got)
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
	oldProfile := lipgloss.Writer.Profile
	lipgloss.Writer.Profile = colorprofile.ANSI256
	defer func() {
		lipgloss.Writer.Profile = oldProfile
	}()

	model := NewModel(Options{Model: "fake/test"})
	model = sendText(t, model, "hello")
	updated, _ := model.Update(keyPress(tea.KeyEnter))
	model = assertModel(t, updated)

	view := model.View().Content
	if !strings.Contains(view, "\x1b[") || !strings.Contains(view, "› ") || !strings.Contains(view, "hello") {
		t.Fatalf("styled render missing ANSI or role symbol:\n%q", view)
	}
}

func TestKeyboardDoesNotToggleActivityBlocks(t *testing.T) {
	model := NewModel(Options{})
	model = sendText(t, model, "first")
	updated, _ := model.Update(keyPress(tea.KeyEnter))
	model = assertModel(t, updated)
	model = sendText(t, model, "second")
	updated, _ = model.Update(keyPress(tea.KeyEnter))
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
	if strings.Contains(viewString(model), "file content") {
		t.Fatalf("tool detail should default collapsed:\n%s", viewString(model))
	}

	updated, cmd := model.Update(keyPress(tea.KeyUp))
	model = assertModel(t, updated)
	if cmd != nil {
		t.Fatalf("up returned unexpected command")
	}
	if got := model.InputValue(); got != "second" {
		t.Fatalf("up input = %q, want second", got)
	}
	if strings.Contains(viewString(model), "└ file content") {
		t.Fatalf("up should not expand activity:\n%s", viewString(model))
	}

	updated, cmd = model.Update(keyPress(tea.KeyDown))
	model = assertModel(t, updated)
	if cmd != nil {
		t.Fatalf("down returned unexpected command")
	}
	if got := model.InputValue(); got != "" {
		t.Fatalf("down input = %q, want empty draft", got)
	}
	if strings.Contains(viewString(model), "└ file content") {
		t.Fatalf("down should not expand activity:\n%s", viewString(model))
	}

	updated, cmd = model.Update(keyPress(tea.KeyEnter))
	model = assertModel(t, updated)
	if cmd != nil {
		t.Fatalf("enter returned unexpected command")
	}
	if strings.Contains(viewString(model), "└ file content") {
		t.Fatalf("enter should not expand activity:\n%s", viewString(model))
	}

	updated, cmd = model.Update(keyPress(tea.KeySpace))
	model = assertModel(t, updated)
	if cmd != nil {
		t.Fatalf("space returned unexpected command")
	}
	if strings.Contains(viewString(model), "└ file content") {
		t.Fatalf("space should not expand activity:\n%s", viewString(model))
	}
}

func TestNoticeActivityRendersSeparateFromToolGroup(t *testing.T) {
	model := NewModel(Options{})
	model.running = true
	model.runID = 3
	model.events = make(chan Event)

	for _, event := range []Event{
		{Type: EventActivity, ActivityKind: "tool", ToolUseID: "call_read", ToolName: "read", Status: "done", Summary: "path=main.go", Content: "file content"},
		{Type: EventActivity, ActivityKind: "notice", Status: "done", Summary: "summarized 8 earlier messages"},
	} {
		updated, cmd := model.Update(streamEventMsg{runID: 3, ok: true, event: event})
		if cmd == nil {
			t.Fatal("activity event should continue waiting for stream events")
		}
		model = assertModel(t, updated)
	}

	got := model.MessageTexts()
	if len(got) != 2 {
		t.Fatalf("messages = %#v, want tool item and separate notice", got)
	}
	if got[0] != "Read path=main.go" || !strings.Contains(got[1], "summarized 8 earlier messages") {
		t.Fatalf("messages = %#v, want notice separate from tool item", got)
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
	if strings.Contains(viewString(model), "full reasoning summary") {
		t.Fatalf("thinking detail should default collapsed:\n%s", viewString(model))
	}

	updated, cmd := model.Update(mouseClick(0, 0))
	if cmd != nil {
		t.Fatalf("mouse click returned unexpected command")
	}
	model = assertModel(t, updated)
	if !strings.Contains(viewString(model), "└ full reasoning summary") {
		t.Fatalf("mouse click did not expand activity:\n%s", viewString(model))
	}

	updated, cmd = model.Update(mouseRelease(0, 0))
	if cmd != nil {
		t.Fatalf("mouse release returned unexpected command")
	}
	model = assertModel(t, updated)
	if !strings.Contains(viewString(model), "└ full reasoning summary") {
		t.Fatalf("mouse release should not collapse activity:\n%s", viewString(model))
	}
}

func TestMouseTogglesGroupedThinkingBlock(t *testing.T) {
	model := NewModel(Options{})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	model = assertModel(t, updated)
	model.messages.startActivityGroup(thinkingActivityGroupKey(1), "Thinking...")
	model.messages.appendOrUpdateActivityInGroup(thinkingActivityGroupKey(1), thinkingGroupName, Event{
		Type:         EventActivity,
		ActivityKind: "thinking",
		Summary:      "checking context",
		Content:      "full grouped reasoning",
	})
	if strings.Contains(viewString(model), "└") {
		t.Fatalf("grouped thinking detail should default collapsed:\n%s", viewString(model))
	}

	updated, cmd := model.Update(mouseClick(0, 0))
	if cmd != nil {
		t.Fatalf("mouse click returned unexpected command")
	}
	model = assertModel(t, updated)
	view := viewString(model)
	if !strings.Contains(view, "└   … thinking: full grouped reasoning") || !strings.Contains(view, "  full grouped reasoning") {
		t.Fatalf("mouse click did not expand grouped thinking:\n%s", view)
	}
}

func TestThinkingActivityDoesNotReplaceRunIndicatorSummary(t *testing.T) {
	model := NewModel(Options{})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	model = assertModel(t, updated)
	model.running = true
	model.status.state = statusThinking
	model.runStartedAt = time.Now()
	model.runID = 5
	model.events = make(chan Event)

	updated, cmd := model.Update(streamEventMsg{
		runID: 5,
		ok:    true,
		event: Event{Type: EventActivity, ActivityKind: "thinking", Summary: "private reasoning stream"},
	})
	if cmd == nil {
		t.Fatal("thinking event should continue waiting for stream events")
	}
	model = assertModel(t, updated)
	if strings.Contains(viewString(model), " · private reasoning stream") {
		t.Fatalf("run indicator leaked thinking summary:\n%s", viewString(model))
	}

	updated, _ = model.Update(streamEventMsg{
		runID: 5,
		ok:    true,
		event: Event{Type: EventActivity, ActivityKind: "tool", ToolUseID: "call_1", ToolName: "read", Status: "running", Summary: "path=main.go"},
	})
	model = assertModel(t, updated)
	view := viewString(model)
	if !strings.Contains(view, "Tool ·") || !strings.Contains(view, " · path=main.go") {
		t.Fatalf("run indicator should still show tool summary:\n%s", viewString(model))
	}
}

func TestMouseExpandsToolGroupFileDiffDetails(t *testing.T) {
	model := NewModel(Options{})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	model = assertModel(t, updated)
	model.running = true
	model.runID = 8
	model.events = make(chan Event)

	updated, cmd := model.Update(streamEventMsg{
		runID: 8,
		ok:    true,
		event: Event{
			Type:         EventActivity,
			ActivityKind: "tool",
			ToolUseID:    "call_write",
			ToolName:     "write",
			Status:       "done",
			Summary:      "create write.md",
			Content:      "create write.md\n--- write.md\n+++ write.md\n@@\n+hello",
		},
	})
	if cmd == nil {
		t.Fatal("activity event should continue waiting for stream events")
	}
	model = assertModel(t, updated)
	if strings.Contains(viewString(model), "+hello") {
		t.Fatalf("tool diff should default collapsed:\n%s", viewString(model))
	}

	updated, cmd = model.Update(mouseClick(0, 0))
	if cmd != nil {
		t.Fatalf("mouse click returned unexpected command")
	}
	model = assertModel(t, updated)
	if !strings.Contains(viewString(model), "Wrote create write.md") {
		t.Fatalf("mouse click did not expand tool item:\n%s", viewString(model))
	}
	if !strings.Contains(viewString(model), "+hello") {
		t.Fatalf("expanded tool item should show diff detail:\n%s", viewString(model))
	}
}

func TestToolGroupDoesNotExpandTrivialCompletedDetail(t *testing.T) {
	model := NewModel(Options{})
	model.messages.appendOrUpdateActivityInGroup("activity:tool:1", toolGroupName, Event{
		Type:         EventActivity,
		ActivityKind: "tool",
		ToolUseID:    "call_ls",
		ToolName:     "ls",
		Status:       "done",
		Summary:      "path=.",
		Content:      "completed",
	})
	model.messages.items[0].collapsed = false

	view := viewString(model)
	if strings.Contains(view, "completed") {
		t.Fatalf("trivial completed detail should not be rendered:\n%s", view)
	}
	if strings.Contains(view, "▸ ✓ Listed path=.") {
		t.Fatalf("trivial detail should not make the tool row clickable:\n%s", view)
	}
	if !strings.Contains(view, "✓ Listed path=.") {
		t.Fatalf("tool summary should remain visible:\n%s", view)
	}
}

func TestTaskToolActivityUsesReadableTitle(t *testing.T) {
	model := NewModel(Options{})
	model.running = true
	model.runID = 9
	model.events = make(chan Event)
	updated, _ := model.Update(streamEventMsg{
		runID: 9,
		ok:    true,
		event: Event{
			Type:         EventActivity,
			ActivityKind: "tool",
			ToolUseID:    "call_task",
			ToolName:     "task",
			Status:       "running",
			Summary:      "prompt=Research providers max_turns=20",
		},
	})
	model = assertModel(t, updated)
	view := viewString(model)
	if !strings.Contains(view, "Running Task... prompt=Research providers max_turns=20") {
		t.Fatalf("task activity missing readable title:\n%s", view)
	}
	if strings.Contains(view, "Working... input accepted") {
		t.Fatalf("task activity still uses generic title:\n%s", view)
	}
}

func TestSubagentActivityUsesPrefixAndScopedKeys(t *testing.T) {
	tool := activityMessage(Event{
		Type:            EventActivity,
		ActivityKind:    "tool",
		ToolUseID:       "subagent:call_task:call_read",
		ToolName:        "read",
		ParentToolUseID: "call_task",
		SubagentID:      "subagent-1",
		Status:          "done",
		Summary:         "path=README.md",
	})
	if !strings.HasPrefix(tool.title, "subagent: ") {
		t.Fatalf("tool title = %q, want subagent prefix", tool.title)
	}
	if tool.key != "tool:subagent:call_task:call_read" {
		t.Fatalf("tool key = %q, want namespaced key", tool.key)
	}

	thinking := activityMessage(Event{
		Type:         EventActivity,
		ActivityKind: "thinking",
		SubagentID:   "subagent-1",
		Summary:      "checking files",
	})
	if thinking.key != "subagent:subagent-1:thinking" {
		t.Fatalf("thinking key = %q, want subagent scoped key", thinking.key)
	}
	if !strings.HasPrefix(thinking.title, "subagent: thinking") {
		t.Fatalf("thinking title = %q, want subagent thinking prefix", thinking.title)
	}

	partial := toolPartialActivity(Event{
		Type:            EventToolPartialOutput,
		ToolUseID:       "subagent:call_task:call_bash",
		ToolName:        "bash",
		ParentToolUseID: "call_task",
		SubagentID:      "subagent-1",
		Summary:         "cmd=go test",
		Content:         "ok\n",
	})
	if partial.ParentToolUseID != "call_task" || partial.SubagentID != "subagent-1" {
		t.Fatalf("partial metadata = parent %q subagent %q, want preserved", partial.ParentToolUseID, partial.SubagentID)
	}
}

func TestSubagentThinkingMergeKeepsPrefix(t *testing.T) {
	first := activityMessage(Event{
		Type:         EventActivity,
		ActivityKind: "thinking",
		SubagentID:   "subagent-1",
		Summary:      "checking",
		Content:      "checking",
	})
	second := activityMessage(Event{
		Type:         EventActivity,
		ActivityKind: "thinking",
		SubagentID:   "subagent-1",
		Summary:      " files",
		Content:      " files",
	})

	merged := mergeActivityMessage(first, second)
	if !strings.HasPrefix(merged.title, "subagent: thinking: ") {
		t.Fatalf("merged title = %q, want subagent thinking prefix", merged.title)
	}
	if !strings.HasPrefix(merged.text, "subagent: thinking: ") {
		t.Fatalf("merged text = %q, want subagent thinking prefix", merged.text)
	}
	if merged.detail != "checking files" {
		t.Fatalf("merged detail = %q, want concatenated reasoning", merged.detail)
	}
}

func TestLoadedLegacyTaskActivityUsesCurrentReadableTitle(t *testing.T) {
	model := NewModel(Options{Messages: []InitialMessage{{
		Turn:         1,
		ActivityKind: "tool",
		ToolUseID:    "call_task",
		ToolName:     "task",
		Status:       "done",
		Summary:      "Ran task prompt=Research providers max_turns=20",
		Content:      "final answer",
	}}})

	view := viewString(model)
	if !strings.Contains(view, "Ran Task prompt=Research providers max_turns=20") {
		t.Fatalf("loaded task activity missing normalized title:\n%s", view)
	}
	if strings.Contains(view, "Ran task") {
		t.Fatalf("loaded task activity kept legacy title:\n%s", view)
	}
}

func TestLoadedLegacyToolDetailShowsTruncationNotice(t *testing.T) {
	model := NewModel(Options{Messages: []InitialMessage{{
		Turn:         1,
		ActivityKind: "tool",
		ToolUseID:    "call_task",
		ToolName:     "task",
		Status:       "done",
		Summary:      "Ran task prompt=Research providers",
		Content:      "partial task result\n... (truncated)",
	}}})
	model.messages.items[0].collapsed = false
	model.messages.invalidateRender()

	view := viewString(model)
	if !strings.Contains(view, "activity detail truncated") {
		t.Fatalf("loaded truncated detail missing notice:\n%s", view)
	}
	if strings.Contains(view, "partial task result\n") && strings.Index(view, "partial task result") < strings.Index(view, "activity detail truncated") {
		t.Fatalf("truncation notice should be visible before legacy preview:\n%s", view)
	}
}

func TestLoadedToolResultTruncationDetailOverridesLegacyActivity(t *testing.T) {
	model := NewModel(Options{Messages: []InitialMessage{
		{
			Turn:         1,
			ActivityKind: "tool",
			ToolUseID:    "call_task",
			ToolName:     "task",
			Status:       "done",
			Summary:      "Ran task prompt=Research providers",
			Content:      "legacy partial result\n... (truncated)",
		},
		{
			Turn:         1,
			ActivityKind: "tool",
			ToolUseID:    "call_task",
			ToolName:     "task",
			Status:       "done",
			Summary:      "prompt=Research providers",
			Content:      "new preview\n... [activity detail truncated: middle omitted; tool result footer preserved]\n... [tool result truncated: original_bytes=999999]\nfull_output_path=/tmp/ub-full-output.txt",
		},
	}})
	model.messages.items[0].collapsed = false
	model.messages.invalidateRender()

	view := viewString(model)
	if !strings.Contains(view, "full_output_path=/tmp/ub-full-output.txt") {
		t.Fatalf("loaded tool result truncation footer was not preserved:\n%s", view)
	}
	if strings.Contains(view, "legacy partial result") {
		t.Fatalf("legacy activity detail incorrectly overrode rebuilt tool_result detail:\n%s", view)
	}
	if strings.Index(view, "activity detail truncated") > strings.Index(view, "new preview") {
		t.Fatalf("truncation notice should be promoted before preview:\n%s", view)
	}
}

func TestBuiltInToolActivitiesUseExplicitLabels(t *testing.T) {
	cases := []struct {
		name      string
		action    string
		completed string
	}{
		{"read", "Reading file...", "Read"},
		{"ls", "Listing directory...", "Listed"},
		{"grep", "Searching content...", "Searched"},
		{"glob", "Finding files...", "Found"},
		{"write", "Preparing write...", "Wrote"},
		{"edit", "Preparing edit...", "Edited"},
		{"multiedit", "Preparing multi-edit...", "Edited multiple files"},
		{"bash", "Writing command...", "Ran"},
		{"task", "Running Task...", "Ran Task"},
		{"remember", "Writing memory...", "Remembered"},
		{"plan_write", "Writing plan...", "Wrote plan"},
		{"plan_update", "Updating plan...", "Updated plan"},
		{"plan_update_step", "Updating plan step...", "Updated plan step"},
		{"todo_write", "Writing todos...", "Wrote todos"},
		{"todo_update", "Updating todos...", "Updated todos"},
		{"tool_result", "Reading tool result...", "Read tool result"},
		{"diagnostics", "Checking diagnostics...", "Checked diagnostics"},
		{"references", "Finding references...", "Found references"},
		{"hover", "Reading hover...", "Read hover"},
		{"completion", "Getting completions...", "Got completions"},
		{"document_symbols", "Listing document symbols...", "Listed document symbols"},
		{"rename", "Preparing rename...", "Prepared rename"},
		{"code_action", "Listing code actions...", "Listed code actions"},
		{"job_run", "Starting job...", "Started job"},
		{"job_output", "Reading job output...", "Read job output"},
		{"job_kill", "Stopping job...", "Stopped job"},
		{"mcp__server__lookup", "Calling MCP server/lookup...", "Called MCP server/lookup"},
	}
	for _, tc := range cases {
		if got := toolAction(tc.name); got != tc.action {
			t.Fatalf("toolAction(%q) = %q, want %q", tc.name, got, tc.action)
		}
		if got := toolTitle(tc.name, ""); got != tc.completed {
			t.Fatalf("toolTitle(%q) = %q, want %q", tc.name, got, tc.completed)
		}
	}
}

func TestPlanToolActivityShowsPlanID(t *testing.T) {
	content := "plan_id=20260608T120000Z-fix-login\npath=/tmp/plan.md\n\n# Fix Login\n"
	if got := toolActivityText(Event{ActivityKind: "tool", ToolName: "plan_write", Status: "done", Summary: "title=Fix Login steps=3", Content: content}); got != "Wrote plan 20260608T120000Z-fix-login" {
		t.Fatalf("plan_write title = %q, want visible plan id", got)
	}
	if got := toolActivityText(Event{ActivityKind: "tool", ToolName: "plan_update", Status: "done", Summary: "title=Fix Login", Content: content}); got != "Updated plan 20260608T120000Z-fix-login" {
		t.Fatalf("plan_update title = %q, want visible plan id", got)
	}
}

func TestTaskToolDetailDoesNotUseDiffFormatting(t *testing.T) {
	var out []string
	out = appendToolDetailLines(out, "task", "- bullet\nchanged file: no\n+ plus", "  ", 80, tuitheme.Default())
	plain := xansi.Strip(strings.Join(out, "\n"))
	if strings.Contains(plain, "changed file: file: no") {
		t.Fatalf("task detail was humanized as file diff:\n%s", plain)
	}
	if !strings.Contains(plain, "- bullet") || !strings.Contains(plain, "changed file: no") || !strings.Contains(plain, "+ plus") {
		t.Fatalf("task detail did not preserve plain lines:\n%s", plain)
	}
	if !toolDetailUsesDiffStyle("write") || toolDetailUsesDiffStyle("task") {
		t.Fatalf("diff style classification broken")
	}
}

func TestToolDetailLineKindClassifiesDiffLines(t *testing.T) {
	cases := map[string]toolDetailLineKindValue{
		"create write.md": toolDetailSummaryLine,
		"--- write.md":    toolDetailHeaderLine,
		"+++ write.md":    toolDetailHeaderLine,
		"@@ -0,0 +1 @@":   toolDetailHeaderLine,
		"+hello":          toolDetailAddedLine,
		"-old":            toolDetailRemovedLine,
		" unchanged":      toolDetailContextLine,
		"":                toolDetailBlankLine,
	}
	for line, want := range cases {
		if got := toolDetailLineKind(line); got != want {
			t.Fatalf("toolDetailLineKind(%q) = %v, want %v", line, got, want)
		}
	}
}

func TestFormatToolDetailLineHumanizesFileChangeSummary(t *testing.T) {
	cases := map[string]string{
		"create write.md":      "created file: write.md",
		"modify internal/a.go": "modified file: internal/a.go",
		"delete old.txt":       "deleted file: old.txt",
	}
	for line, want := range cases {
		if got := formatToolDetailLine(line); got != want {
			t.Fatalf("formatToolDetailLine(%q) = %q, want %q", line, got, want)
		}
	}
}

func TestCollapsedActivityBlocksStackRowsAndMouseTargetsChip(t *testing.T) {
	model := NewModel(Options{})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	model = assertModel(t, updated)
	model.messages.appendOrUpdateActivity(Event{
		Type:         EventActivity,
		ActivityKind: "thinking",
		Status:       "running",
		Summary:      "checking repository",
		Content:      "checking repository",
	})
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
	if len(lines) != 3 {
		t.Fatalf("collapsed activity blocks should stack one per row:\n%s", view)
	}
	if !strings.Contains(lines[0], "checking repository") || !strings.Contains(lines[1], "Read path=main.go") || !strings.Contains(lines[2], "Searched pattern=TODO") {
		t.Fatalf("compact rows missing activity chips:\n%s", view)
	}

	x := strings.Index(lines[2], "Searched")
	if x < 0 {
		t.Fatalf("third chip not found:\n%s", view)
	}
	updated, cmd := model.Update(mouseClick(x, 2))
	if cmd != nil {
		t.Fatalf("mouse click returned unexpected command")
	}
	model = assertModel(t, updated)
	rendered := viewString(model)
	if !strings.Contains(rendered, "└ grep detail") {
		t.Fatalf("mouse click did not expand second activity:\n%s", rendered)
	}
	if strings.Contains(rendered, "└ read detail") {
		t.Fatalf("mouse click expanded wrong activity:\n%s", rendered)
	}
}

func TestCollapsedActivityBlocksUseFullRowWidth(t *testing.T) {
	var list messageList
	list.appendOrUpdateActivity(Event{
		Type:         EventActivity,
		ActivityKind: "thinking",
		Status:       "running",
		Summary:      "checking repository context before reading package manifests and tests",
		Content:      "checking repository context before reading package manifests and tests",
	})
	list.appendOrUpdateActivity(Event{
		Type:         EventActivity,
		ActivityKind: "tool",
		ToolUseID:    "call_rg",
		ToolName:     "grep",
		Status:       "running",
		Summary:      "pattern=appendOrUpdateActivity path=internal/tui/message_list.go",
	})

	// Keep a little slack for locales where ambiguous-width glyphs such as
	// the ellipsis render as two cells; this test is about using the full row,
	// not about the exact truncation boundary.
	view := list.view(84, 10, 0, tuitheme.Plain())
	for _, want := range []string{
		"package manifests and tests",
		"path=internal/tui/messa",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("collapsed activity should use full row width, missing %q:\n%s", want, view)
		}
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
	if !strings.Contains(viewString(model), "Permission required") || !strings.Contains(viewString(model), "tool: bash") {
		t.Fatalf("view missing modal:\n%s", viewString(model))
	}

	updated, cmd = model.Update(runePress('5'))
	model = assertModel(t, updated)
	if cmd != nil {
		msg := cmd()
		if _, ok := msg.(permissionRequestMsg); ok {
			t.Fatalf("unexpected immediate permission request: %#v", msg)
		}
	}
	select {
	case got := <-response:
		if got != permission.DecisionAlwaysProjectCmd {
			t.Fatalf("decision = %q, want always project command", got)
		}
	default:
		t.Fatalf("no decision returned")
	}
	if strings.Contains(viewString(model), "Permission required") {
		t.Fatalf("modal still visible:\n%s", viewString(model))
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
	if !strings.Contains(viewString(model), "> Allow once") {
		t.Fatalf("permission modal missing selectable options:\n%s", viewString(model))
	}

	updated, cmd := model.Update(keyPress(tea.KeyDown))
	if cmd != nil {
		t.Fatalf("down returned unexpected command")
	}
	model = assertModel(t, updated)
	if !strings.Contains(viewString(model), "> Deny") {
		t.Fatalf("permission modal did not move selection:\n%s", viewString(model))
	}

	updated, cmd = model.Update(keyPress(tea.KeyEnter))
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

func TestPermissionModalHidesCursorAndKeepsStatusAtBottom(t *testing.T) {
	model := NewModel(Options{Model: "fake/test"})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	model = assertModel(t, updated)
	updated, cmd := model.Update(permissionRequestMsg{request: PermissionRequest{
		Request: permission.Request{
			Tool: "bash",
			Risk: tool.RiskExec,
			Mode: execution.ModeWork,
		},
		Response: make(chan permission.Decision, 1),
	}, ok: true})
	if cmd != nil {
		t.Fatalf("permission request returned unexpected command")
	}
	model = assertModel(t, updated)

	view := model.View()
	if view.Cursor != nil {
		t.Fatalf("permission modal should not expose input cursor: %+v\n%s", view.Cursor, view.Content)
	}
	lines := strings.Split(view.Content, "\n")
	if !strings.Contains(view.Content, "Permission required") {
		t.Fatalf("view missing modal:\n%s", view.Content)
	}
	if !strings.Contains(lines[len(lines)-1], "state: idle") {
		t.Fatalf("status bar should stay at the bottom with modal:\n%s", view.Content)
	}
}

func TestSlashHelpDoesNotCallRunner(t *testing.T) {
	runner := &scriptedRunner{}
	model := NewModel(Options{Runner: runner})
	model = sendText(t, model, "/help")

	updated, cmd := model.Update(keyPress(tea.KeyEnter))
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

func TestSlashHelpListsShortcuts(t *testing.T) {
	help := slashHelp()
	for _, want := range []string{
		"input:",
		"!<command> - run a local shell command",
		"@<prefix> - search workspace files",
		"keyboard:",
		"Enter - send prompt",
		"Ctrl+C - quit",
		"Esc - clear activity focus",
		"Shift+Tab - cycle execution mode",
		"? - show this cheatsheet",
		"PgUp/PgDown - scroll the transcript",
		"Ctrl+Home/Ctrl+End - jump to the start/end",
		"Ctrl+O - expand/collapse",
		"Ctrl+N/Ctrl+P - move activity focus",
		"Up/Down - move through suggestions",
		"Tab - complete slash commands",
		"pickers and permission:",
		"model/effort/session pickers",
		"@ file picker",
		"permission modal",
		"1-5 choose",
		"d toggles preview",
		"Left/Right switches files",
	} {
		if !strings.Contains(help, want) {
			t.Fatalf("slashHelp missing %q:\n%s", want, help)
		}
	}
	for _, unwanted := range []string{"mouse:", "left click - expand/collapse"} {
		if strings.Contains(help, unwanted) {
			t.Fatalf("slashHelp should not advertise mouse tracking %q:\n%s", unwanted, help)
		}
	}
}

func TestQuestionMarkOpensCheatsheet(t *testing.T) {
	model := NewModel(Options{})
	updated, cmd := model.Update(runePress('?'))
	if cmd != nil {
		t.Fatalf("question mark returned unexpected command")
	}
	model = assertModel(t, updated)

	if got := model.MessageTexts(); len(got) != 1 || !strings.Contains(got[0], "commands:") {
		t.Fatalf("messages = %#v, want cheatsheet", got)
	}
}

func TestStatusHelpClickOpensCheatsheet(t *testing.T) {
	model := NewModel(Options{})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 12})
	model = assertModel(t, updated)
	lines := strings.Split(viewString(model), "\n")
	statusLine := lines[len(lines)-1]
	x := strings.LastIndex(statusLine, "?")
	if x < 0 {
		t.Fatalf("status bar missing help marker:\n%s", statusLine)
	}
	x = xansi.StringWidth(statusLine[:x])

	updated, cmd := model.Update(mouseClick(x, len(lines)-1))
	if cmd != nil {
		t.Fatalf("status help click returned unexpected command")
	}
	model = assertModel(t, updated)
	if got := model.MessageTexts(); len(got) != 1 || !strings.Contains(got[0], "commands:") {
		t.Fatalf("messages = %#v, want cheatsheet", got)
	}
}

func TestCtrlOTogglesLatestActivityDetail(t *testing.T) {
	model := NewModel(Options{})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	model = assertModel(t, updated)
	model.messages.appendOrUpdateActivityInGroup("activity:tool:1", toolGroupName, Event{
		Type:         EventActivity,
		ActivityKind: "tool",
		ToolUseID:    "call_write",
		ToolName:     "write",
		Status:       "done",
		Summary:      "path=main.go",
		Content:      "--- main.go\n+++ main.go\n@@\n-old\n+new",
	})

	view := model.View()
	if view.MouseMode != tea.MouseModeCellMotion {
		t.Fatalf("mouse mode = %v, want CellMotion for wheel scrolling and click toggling", view.MouseMode)
	}
	if strings.Contains(viewString(model), "└ Wrote main.go") || strings.Contains(viewString(model), "+new") {
		t.Fatalf("tool detail should default collapsed:\n%s", viewString(model))
	}

	updated, cmd := model.Update(keyPress('o', tea.ModCtrl))
	model = assertModel(t, updated)
	if cmd != nil {
		t.Fatalf("ctrl+o returned unexpected command")
	}
	if !strings.Contains(viewString(model), "└ ▸ ✓ Wrote path=main.go") {
		t.Fatalf("ctrl+o did not expand latest activity group:\n%s", viewString(model))
	}
	if strings.Contains(viewString(model), "+new") {
		t.Fatalf("ctrl+o should expand the group before file detail:\n%s", viewString(model))
	}

	updated, cmd = model.Update(keyPress('o', tea.ModCtrl))
	model = assertModel(t, updated)
	if cmd != nil {
		t.Fatalf("second ctrl+o returned unexpected command")
	}
	if !strings.Contains(viewString(model), "+new") {
		t.Fatalf("second ctrl+o did not expand latest activity detail:\n%s", viewString(model))
	}
}

func TestKeyboardFocusTogglesMultipleActivityTargets(t *testing.T) {
	model := NewModel(Options{})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	model = assertModel(t, updated)
	model.messages.appendOrUpdateActivityInGroup("activity:tool:2", toolGroupName, Event{
		Type:         EventActivity,
		ActivityKind: "tool",
		ToolUseID:    "call_read",
		ToolName:     "read",
		Status:       "done",
		Summary:      "path=main.go",
		Content:      "read detail",
	})
	model.messages.appendOrUpdateActivityInGroup("activity:tool:2", toolGroupName, Event{
		Type:         EventActivity,
		ActivityKind: "tool",
		ToolUseID:    "call_write",
		ToolName:     "write",
		Status:       "done",
		Summary:      "path=main.go",
		Content:      "--- main.go\n+++ main.go\n@@\n-old\n+new",
	})

	if model.View().MouseMode != tea.MouseModeCellMotion {
		t.Fatalf("mouse tracking should be enabled for wheel scrolling and click toggling")
	}

	updated, cmd := model.Update(keyPress('o', tea.ModCtrl))
	model = assertModel(t, updated)
	if cmd != nil {
		t.Fatalf("ctrl+o returned unexpected command")
	}
	if !strings.Contains(viewString(model), "└ ▸ ✓ Read path=main.go") || strings.Contains(viewString(model), "read detail") {
		t.Fatalf("ctrl+o should expand only the tool group first:\n%s", viewString(model))
	}

	updated, cmd = model.Update(keyPress('n', tea.ModCtrl))
	model = assertModel(t, updated)
	if cmd != nil {
		t.Fatalf("ctrl+n returned unexpected command")
	}
	updated, cmd = model.Update(keyPress(tea.KeyEnter))
	model = assertModel(t, updated)
	if cmd != nil {
		t.Fatalf("enter returned unexpected command")
	}
	if !strings.Contains(viewString(model), "read detail") || strings.Contains(viewString(model), "+new") {
		t.Fatalf("enter should toggle the first focused activity entry only:\n%s", viewString(model))
	}

	updated, cmd = model.Update(keyPress('n', tea.ModCtrl))
	model = assertModel(t, updated)
	if cmd != nil {
		t.Fatalf("second ctrl+n returned unexpected command")
	}
	updated, cmd = model.Update(keyPress(tea.KeySpace))
	model = assertModel(t, updated)
	if cmd != nil {
		t.Fatalf("space returned unexpected command")
	}
	if !strings.Contains(viewString(model), "read detail") || !strings.Contains(viewString(model), "+new") {
		t.Fatalf("space should toggle the second focused activity entry:\n%s", viewString(model))
	}
}

func TestEscClearsActivityFocus(t *testing.T) {
	model := NewModel(Options{})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
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

	updated, cmd := model.Update(keyPress('n', tea.ModCtrl))
	model = assertModel(t, updated)
	if cmd != nil {
		t.Fatalf("ctrl+n returned unexpected command")
	}
	if !model.messages.hasFocusedCollapsible() {
		t.Fatal("ctrl+n should focus the activity")
	}

	updated, cmd = model.Update(keyPress(tea.KeyEsc))
	model = assertModel(t, updated)
	if cmd != nil {
		t.Fatalf("esc returned unexpected command")
	}
	if model.messages.hasFocusedCollapsible() {
		t.Fatal("esc should clear activity focus")
	}

	updated, cmd = model.Update(keyPress(tea.KeyEnter))
	model = assertModel(t, updated)
	if cmd != nil {
		t.Fatalf("enter returned unexpected command")
	}
	if strings.Contains(viewString(model), "read detail") {
		t.Fatalf("enter should not toggle an activity after focus is cleared:\n%s", viewString(model))
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

	updated, cmd := model.Update(keyPress(tea.KeyEnter))
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
	view := viewString(model)
	if !strings.Contains(view, "compacted 4 earlier messages") || !strings.Contains(view, "ctx est: 900/3000 30%") {
		t.Fatalf("view missing compact result:\n%s", view)
	}
}

func TestSlashCompactUnavailable(t *testing.T) {
	runner := &promptOnlyRunner{}
	model := NewModel(Options{Runner: runner})
	model = sendText(t, model, "/compact")

	updated, cmd := model.Update(keyPress(tea.KeyEnter))
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

func TestSlashInitRunsAgentPrompt(t *testing.T) {
	runner := &scriptedRunner{events: []Event{
		{Type: EventActivity, ActivityKind: "tool", ToolName: "read", Status: "done", Summary: "AGENTS.md"},
		{Type: EventDeltaText, Text: "Updated AGENTS.md"},
		{Type: EventDone},
	}}
	model := NewModel(Options{Runner: runner})
	model = sendText(t, model, "/init")

	updated, cmd := model.Update(keyPress(tea.KeyEnter))
	if cmd == nil {
		t.Fatalf("slash init returned nil command")
	}
	model = assertModel(t, updated)
	model = drainBatch(t, model, cmd)

	if runner.calls != 1 {
		t.Fatalf("runner calls = %d, want 1", runner.calls)
	}
	if len(runner.prompts) != 1 {
		t.Fatalf("runner prompts = %#v, want one prompt", runner.prompts)
	}
	for _, want := range []string{"Create or update AGENTS.md", "Inspect the repository before editing", "If AGENTS.md already exists, improve it in place"} {
		if !strings.Contains(runner.prompts[0], want) {
			t.Fatalf("init prompt missing %q:\n%s", want, runner.prompts[0])
		}
	}
	for _, unwanted := range []string{"Create or update CLAUDE.md", "Create or update .ub/instructions.md"} {
		if strings.Contains(runner.prompts[0], unwanted) {
			t.Fatalf("init prompt should not target %q:\n%s", unwanted, runner.prompts[0])
		}
	}
	if got := model.MessageTexts(); len(got) < 2 || !strings.Contains(got[0], "running /init") || !strings.Contains(got[len(got)-1], "Updated AGENTS.md") {
		t.Fatalf("messages = %#v, want init notice and assistant summary", got)
	}
	view := viewString(model)
	if !strings.Contains(view, "Read AGENTS.md") {
		t.Fatalf("view missing init tool activity:\n%s", view)
	}
}

func TestSlashInitIncludesGuidance(t *testing.T) {
	runner := &scriptedRunner{events: []Event{{Type: EventDone}}}
	model := NewModel(Options{Runner: runner})
	model = sendText(t, model, "/init focus on pnpm")

	updated, cmd := model.Update(keyPress(tea.KeyEnter))
	if cmd == nil {
		t.Fatalf("slash init returned nil command")
	}
	model = assertModel(t, updated)
	model = drainBatch(t, model, cmd)

	if len(runner.prompts) != 1 || !strings.Contains(runner.prompts[0], "Additional user guidance") || !strings.Contains(runner.prompts[0], "focus on pnpm") {
		t.Fatalf("runner prompts = %#v", runner.prompts)
	}
}

func TestSlashInitUnavailable(t *testing.T) {
	model := NewModel(Options{})
	model = sendText(t, model, "/init")

	updated, cmd := model.Update(keyPress(tea.KeyEnter))
	if cmd != nil {
		t.Fatalf("slash init returned unexpected command")
	}
	model = assertModel(t, updated)
	if got := model.MessageTexts(); len(got) != 1 || !strings.Contains(got[0], "init is unavailable") {
		t.Fatalf("messages = %#v", got)
	}
}

func TestSlashPlanEditRequiresPlanID(t *testing.T) {
	model := NewModel(Options{})
	model = sendText(t, model, "/plan-edit")

	updated, cmd := model.Update(keyPress(tea.KeyEnter))
	if cmd != nil {
		t.Fatalf("slash plan-edit returned unexpected command")
	}
	model = assertModel(t, updated)
	if got := model.MessageTexts(); len(got) != 1 || !strings.Contains(got[0], "usage: /plan-edit <plan-id>") {
		t.Fatalf("messages = %#v", got)
	}
}

func TestSlashPlanEditOpensExistingPlan(t *testing.T) {
	stateHome := filepath.Join(t.TempDir(), "state")
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("VISUAL", "true")
	workspace := t.TempDir()
	path, err := plan.Path(workspace, "plan-1")
	if err != nil {
		t.Fatalf("plan path: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("# Plan\n"), 0o644); err != nil {
		t.Fatalf("write plan: %v", err)
	}

	model := NewModel(Options{Cwd: workspace})
	model = sendText(t, model, "/plan-edit plan-1")
	updated, cmd := model.Update(keyPress(tea.KeyEnter))
	if cmd == nil {
		t.Fatalf("slash plan-edit returned nil command")
	}
	model = assertModel(t, updated)
	if got := model.MessageTexts(); len(got) != 1 || !strings.Contains(got[0], "editing plan "+path) {
		t.Fatalf("messages = %#v", got)
	}
}

func TestSlashPlansPickerOpensExistingPlan(t *testing.T) {
	stateHome := filepath.Join(t.TempDir(), "state")
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("VISUAL", "true")
	workspace := t.TempDir()
	path, err := plan.Path(workspace, "20260608T120000Z-fix")
	if err != nil {
		t.Fatalf("plan path: %v", err)
	}
	writePlanFile(t, path, "Fix Plan", "in_progress", []string{"inspect", "patch"})

	model := NewModel(Options{Cwd: workspace})
	model = sendText(t, model, "/plans")
	updated, cmd := model.Update(keyPress(tea.KeyEnter))
	if cmd != nil {
		t.Fatalf("slash plans returned unexpected command")
	}
	model = assertModel(t, updated)
	view := viewString(model)
	if !strings.Contains(view, "select plan") || !strings.Contains(view, "20260608T120000Z-fix") || !strings.Contains(view, "Fix Plan") {
		t.Fatalf("plans picker missing plan id/title:\n%s", view)
	}

	updated, cmd = model.Update(keyPress(tea.KeyEnter))
	if cmd == nil {
		t.Fatalf("selecting plan returned nil command")
	}
	model = assertModel(t, updated)
	if got := model.MessageTexts(); len(got) != 1 || !strings.Contains(got[0], "editing plan "+path) {
		t.Fatalf("messages = %#v", got)
	}
}

func TestSlashPlansReportsEmptyWorkspace(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	model := NewModel(Options{Cwd: t.TempDir()})
	model = sendText(t, model, "/plans")

	updated, cmd := model.Update(keyPress(tea.KeyEnter))
	if cmd != nil {
		t.Fatalf("slash plans returned unexpected command")
	}
	model = assertModel(t, updated)
	if got := model.MessageTexts(); len(got) != 1 || got[0] != "no plans in this workspace" {
		t.Fatalf("messages = %#v", got)
	}
}

func TestPlanEditorCommandUsesVisualThenEditor(t *testing.T) {
	lookup := func(key string) (string, bool) {
		switch key {
		case "VISUAL":
			return "code --wait", true
		case "EDITOR":
			return "vim", true
		default:
			return "", false
		}
	}
	name, args := planEditorCommandFromEnv(lookup)
	if name != "code" || len(args) != 1 || args[0] != "--wait" {
		t.Fatalf("editor command = %q %#v, want code --wait", name, args)
	}
}

func TestSlashRetryRunsLastUserTurn(t *testing.T) {
	runner := &scriptedRunner{events: []Event{{Type: EventDone}}}
	model := NewModel(Options{Runner: runner})
	model = sendText(t, model, "first prompt")

	updated, cmd := model.Update(keyPress(tea.KeyEnter))
	if cmd == nil {
		t.Fatalf("initial prompt returned nil command")
	}
	model = assertModel(t, updated)
	model = drainBatch(t, model, cmd)

	model = sendText(t, model, "/retry")
	updated, cmd = model.Update(keyPress(tea.KeyEnter))
	if cmd == nil {
		t.Fatalf("slash retry returned nil command")
	}
	model = assertModel(t, updated)
	model = drainBatch(t, model, cmd)

	if got, want := runner.prompts, []string{"first prompt", "first prompt"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("runner prompts = %#v, want %#v", got, want)
	}
	if got, want := model.MessageTexts(), []string{"first prompt", "first prompt"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("messages = %#v, want %#v", got, want)
	}
}

func TestSlashRetryWithoutUserTurnReportsMessage(t *testing.T) {
	model := NewModel(Options{})
	model = sendText(t, model, "/retry")

	updated, cmd := model.Update(keyPress(tea.KeyEnter))
	if cmd != nil {
		t.Fatalf("slash retry returned unexpected command")
	}
	model = assertModel(t, updated)
	if got := model.MessageTexts(); len(got) != 1 || !strings.Contains(got[0], "no user turn to retry") {
		t.Fatalf("messages = %#v", got)
	}
}

func TestSlashRewindPickerRestoresSelectedPrompt(t *testing.T) {
	runner := &scriptedRunner{
		rewindTargets: []RewindTarget{
			{Turn: 2, Text: "second prompt"},
			{Turn: 1, Text: "first prompt"},
		},
		rewindState: SessionState{
			ID:       "sess_1",
			Turn:     1,
			Messages: []InitialMessage{{Role: userRole, Text: "first prompt", Turn: 1}},
		},
		rewindResult: RewindResult{Target: RewindTarget{Turn: 2, Text: "second prompt"}, DeletedEvents: 2},
	}
	model := NewModel(Options{Runner: runner})
	model = sendText(t, model, "/rewind")

	updated, cmd := model.Update(keyPress(tea.KeyEnter))
	if cmd != nil {
		t.Fatalf("slash rewind returned unexpected command")
	}
	model = assertModel(t, updated)
	if model.rewind == nil || model.rewind.phase != rewindPickerTargets {
		t.Fatalf("rewind picker = %#v, want target picker", model.rewind)
	}

	updated, cmd = model.Update(keyPress(tea.KeyEnter))
	if cmd != nil {
		t.Fatalf("rewind selection returned unexpected command")
	}
	model = assertModel(t, updated)
	if got := model.InputValue(); got != "second prompt" {
		t.Fatalf("input = %q, want selected prompt", got)
	}
	if got, want := runner.rewindRequests, []RewindRequest{{Turn: 2}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("rewind requests = %#v, want %#v", got, want)
	}
	texts := model.MessageTexts()
	if len(texts) != 2 || texts[0] != "first prompt" || !strings.Contains(texts[1], "rewound to before turn 2") {
		t.Fatalf("messages = %#v", texts)
	}
}

func TestSlashRewindPickerChoosesFileRevertMode(t *testing.T) {
	target := RewindTarget{
		Turn: 2,
		Text: "change file",
		AffectedFiles: []RewindFileChange{{
			Path: "main.go",
			Kind: "modify",
		}},
	}
	runner := &scriptedRunner{
		rewindTargets: []RewindTarget{target},
		rewindState:   SessionState{ID: "sess_1", Turn: 1},
		rewindResult:  RewindResult{Target: target, DeletedEvents: 3, RevertedFiles: []string{"main.go modify"}},
	}
	model := NewModel(Options{Runner: runner})
	model = sendText(t, model, "/rewind")

	updated, cmd := model.Update(keyPress(tea.KeyEnter))
	if cmd != nil {
		t.Fatalf("slash rewind returned unexpected command")
	}
	model = assertModel(t, updated)
	updated, cmd = model.Update(keyPress(tea.KeyEnter))
	if cmd != nil {
		t.Fatalf("target selection returned unexpected command")
	}
	model = assertModel(t, updated)
	if model.rewind == nil || model.rewind.phase != rewindPickerMode {
		t.Fatalf("rewind picker = %#v, want mode picker", model.rewind)
	}

	updated, cmd = model.Update(keyPress(tea.KeyDown))
	if cmd != nil {
		t.Fatalf("mode navigation returned unexpected command")
	}
	model = assertModel(t, updated)
	updated, cmd = model.Update(keyPress(tea.KeyEnter))
	if cmd != nil {
		t.Fatalf("mode selection returned unexpected command")
	}
	model = assertModel(t, updated)
	if got, want := runner.rewindRequests, []RewindRequest{{Turn: 2, RevertFiles: true}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("rewind requests = %#v, want %#v", got, want)
	}
	if got := model.InputValue(); got != "change file" {
		t.Fatalf("input = %q, want restored prompt", got)
	}
	texts := model.MessageTexts()
	if len(texts) != 1 || !strings.Contains(texts[0], "reverted files: main.go modify") {
		t.Fatalf("messages = %#v", texts)
	}
}

func TestSlashDoctorAppendsHealthCheckReport(t *testing.T) {
	runner := &scriptedRunner{doctorReport: "providers:\n  fake\tfake\toffline\n"}
	model := NewModel(Options{Runner: runner})
	model = sendText(t, model, "/doctor")

	updated, cmd := model.Update(keyPress(tea.KeyEnter))
	model = assertModel(t, updated)
	if cmd == nil {
		t.Fatalf("slash doctor should kick off an async runner command")
	}
	updated, next := model.Update(cmd())
	model = assertModel(t, updated)
	if next != nil {
		t.Fatalf("doctor result handler returned unexpected command")
	}

	if runner.doctorCalls != 1 {
		t.Fatalf("doctor calls = %d, want 1", runner.doctorCalls)
	}
	if got := model.MessageTexts(); !reflect.DeepEqual(got, []string{"running doctor…", "providers:\n  fake\tfake\toffline"}) {
		t.Fatalf("messages = %#v", got)
	}
}

func TestSlashDoctorUnavailable(t *testing.T) {
	model := NewModel(Options{Runner: &promptOnlyRunner{}})
	model = sendText(t, model, "/doctor")

	updated, cmd := model.Update(keyPress(tea.KeyEnter))
	if cmd != nil {
		t.Fatalf("slash doctor returned unexpected command")
	}
	model = assertModel(t, updated)
	if got := model.MessageTexts(); len(got) != 1 || !strings.Contains(got[0], "doctor is unavailable") {
		t.Fatalf("messages = %#v", got)
	}
}

func TestSlashSessionsSearchDelegatesToRunner(t *testing.T) {
	runner := &scriptedRunner{}
	model := NewModel(Options{Runner: runner})
	model = sendText(t, model, "/sessions search test query")
	updated, cmd := model.Update(keyPress(tea.KeyEnter))
	model = assertModel(t, updated)
	if cmd != nil {
		t.Fatalf("sessions search should not return async command, got %v", cmd)
	}
	texts := model.MessageTexts()
	if len(texts) < 2 {
		t.Fatalf("expected at least 2 messages, got %d: %#v", len(texts), texts)
	}
	if !strings.Contains(texts[0], "searching sessions") {
		t.Fatalf("first message should mention searching, got %q", texts[0])
	}
	if !strings.Contains(texts[1], "search results for") || !strings.Contains(texts[1], "test query") {
		t.Fatalf("second message should contain search results, got %q", texts[1])
	}
}

func TestSlashSessionsSearchUnavailable(t *testing.T) {
	model := NewModel(Options{Runner: &promptOnlyRunner{}})
	model = sendText(t, model, "/sessions search test")
	updated, cmd := model.Update(keyPress(tea.KeyEnter))
	if cmd != nil {
		t.Fatalf("sessions search returned unexpected command")
	}
	model = assertModel(t, updated)
	if got := model.MessageTexts(); len(got) != 1 || !strings.Contains(got[0], "unavailable") {
		t.Fatalf("messages = %#v", got)
	}
}

func TestSlashSessionsSearchEmptyQuery(t *testing.T) {
	runner := &scriptedRunner{}
	model := NewModel(Options{Runner: runner})
	model = sendText(t, model, "/sessions search")
	updated, cmd := model.Update(keyPress(tea.KeyEnter))
	if cmd != nil {
		t.Fatalf("sessions search empty should not return command")
	}
	model = assertModel(t, updated)
	if got := model.MessageTexts(); len(got) != 1 || !strings.Contains(got[0], "usage") {
		t.Fatalf("messages = %#v, want usage hint", got)
	}
}

func TestSlashCopyCopiesNthMessage(t *testing.T) {
	clipboard := &recordingClipboard{}
	model := NewModel(Options{
		Clipboard: clipboard,
		Messages: []InitialMessage{
			{Role: userRole, Text: "first prompt"},
			{Role: assistantRole, Text: "second answer"},
		},
	})
	model = sendText(t, model, "/copy 2")

	updated, cmd := model.Update(keyPress(tea.KeyEnter))
	model = assertModel(t, updated)
	if cmd == nil {
		t.Fatalf("slash copy should issue an async clipboard command")
	}
	updated, _ = model.Update(cmd())
	model = assertModel(t, updated)

	if clipboard.calls != 1 || clipboard.text != "second answer" {
		t.Fatalf("clipboard calls/text = %d/%q, want 1/second answer", clipboard.calls, clipboard.text)
	}
	if got, want := model.MessageTexts(), []string{"first prompt", "second answer"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("messages = %#v, want %#v", got, want)
	}
	if !strings.Contains(viewString(model), "ok: copied message 2") {
		t.Fatalf("view missing copy toast:\n%s", viewString(model))
	}
}

func TestSlashCopyNoArgsCopiesLastAssistant(t *testing.T) {
	clipboard := &recordingClipboard{}
	model := NewModel(Options{
		Clipboard: clipboard,
		Messages: []InitialMessage{
			{Role: userRole, Text: "first prompt"},
			{Role: assistantRole, Text: "first answer"},
			{Role: userRole, Text: "second prompt"},
			{Role: assistantRole, Text: "second answer"},
		},
	})
	model = sendText(t, model, "/copy")

	updated, cmd := model.Update(keyPress(tea.KeyEnter))
	model = assertModel(t, updated)
	if cmd == nil {
		t.Fatalf("slash copy should issue an async clipboard command")
	}
	updated, _ = model.Update(cmd())
	model = assertModel(t, updated)

	if clipboard.calls != 1 || clipboard.text != "second answer" {
		t.Fatalf("clipboard calls/text = %d/%q, want 1/second answer", clipboard.calls, clipboard.text)
	}
	if !strings.Contains(viewString(model), "ok: copied last response") {
		t.Fatalf("view missing copy toast:\n%s", viewString(model))
	}
}

func TestSlashCopyNoAssistantResponse(t *testing.T) {
	clipboard := &recordingClipboard{}
	model := NewModel(Options{
		Clipboard: clipboard,
		Messages: []InitialMessage{
			{Role: userRole, Text: "only a prompt"},
		},
	})
	model = sendText(t, model, "/copy")

	updated, cmd := model.Update(keyPress(tea.KeyEnter))
	if cmd != nil {
		t.Fatalf("slash copy should not issue a command when no assistant response")
	}
	model = assertModel(t, updated)
	if clipboard.calls != 0 {
		t.Fatalf("clipboard calls = %d, want 0", clipboard.calls)
	}
	if got := model.MessageTexts(); len(got) != 2 || !strings.Contains(got[1], "no assistant response to copy") {
		t.Fatalf("messages = %#v", got)
	}
}

func TestSlashBtwRunsWhileMainTurnIsRunningWithoutTranscriptPollution(t *testing.T) {
	runner := &scriptedRunner{sideEvents: []Event{
		{Type: EventDeltaText, Text: "side "},
		{Type: EventDeltaText, Text: "answer"},
		{Type: EventDone},
	}}
	model := NewModel(Options{Runner: runner, Model: "fake/test"})
	model = sendText(t, model, "main task")
	updated, mainCmd := model.Update(keyPress(tea.KeyEnter))
	model = assertModel(t, updated)
	if mainCmd == nil || !model.Running() {
		t.Fatalf("main prompt should start a running turn")
	}

	model = sendText(t, model, "/btw what does this mean")
	updated, sideCmd := model.Update(keyPress(tea.KeyEnter))
	model = assertModel(t, updated)
	if sideCmd == nil {
		t.Fatalf("/btw should start an async side question")
	}
	model = drainSideQuestion(t, model, sideCmd)

	if runner.sideCalls != 1 || runner.sideQuestions[0] != "what does this mean" {
		t.Fatalf("side calls/questions = %d/%#v", runner.sideCalls, runner.sideQuestions)
	}
	if !model.Running() {
		t.Fatalf("main run should remain active after /btw")
	}
	if len(model.QueuedPrompts()) != 0 {
		t.Fatalf("/btw should not queue as a main prompt: %#v", model.QueuedPrompts())
	}
	gotMessages := model.MessageTexts()
	if len(gotMessages) < 1 || gotMessages[0] != "main task" {
		t.Fatalf("messages = %#v, want main task first", gotMessages)
	}
	for _, text := range gotMessages {
		if strings.Contains(text, "what does this mean") || strings.Contains(text, "side answer") {
			t.Fatalf("side question polluted transcript messages: %#v", gotMessages)
		}
	}
	view := viewString(model)
	if !strings.Contains(view, "BTW (1)") ||
		!strings.Contains(view, "Q1: what does this mean") ||
		!strings.Contains(view, "A1: side answer") {
		t.Fatalf("view missing side question view:\n%s", view)
	}
}

func TestSlashBtwViewShowsWaitingHintWhileAsking(t *testing.T) {
	runner := &scriptedRunner{sideEvents: []Event{
		{Type: EventDeltaText, Text: "slow answer"},
		{Type: EventDone},
	}}
	model := NewModel(Options{Runner: runner, Model: "fake/test"})
	model = sendText(t, model, "/btw slow question")
	updated, cmd := model.Update(keyPress(tea.KeyEnter))
	model = assertModel(t, updated)
	if cmd == nil {
		t.Fatalf("/btw should return a command")
	}

	view := viewString(model)
	if !strings.Contains(view, "waiting for answer...") || strings.Contains(view, "type follow-up...") {
		t.Fatalf("asking BTW view should show waiting hint, not follow-up hint:\n%s", view)
	}

	model = sendText(t, model, "next question")
	if model.btw.draft != "" {
		t.Fatalf("asking BTW view accepted draft input: %q", model.btw.draft)
	}
	if strings.Contains(viewString(model), "next question") {
		t.Fatalf("asking BTW view should not render draft input:\n%s", viewString(model))
	}
}

func TestSlashBtwViewCopyClearAndEscClearsHistory(t *testing.T) {
	clipboard := &recordingClipboard{}
	runner := &scriptedRunner{sideEvents: []Event{
		{Type: EventDeltaText, Text: "detached answer"},
		{Type: EventDone},
	}}
	model := NewModel(Options{Runner: runner, Model: "fake/test", Clipboard: clipboard})
	model = sendText(t, model, "/btw quick question")
	updated, cmd := model.Update(keyPress(tea.KeyEnter))
	model = assertModel(t, updated)
	if cmd == nil {
		t.Fatalf("/btw should return a command")
	}
	assertBTWStatusLine(t, model, "answering")
	model = drainSideQuestion(t, model, cmd)
	assertCursorOnBTWLine(t, model)
	assertBTWStatusLine(t, model, "idle")

	updated, copyCmd := model.Update(keyPress('y', tea.ModCtrl))
	model = assertModel(t, updated)
	if copyCmd == nil {
		t.Fatalf("copying side answer should return clipboard command")
	}
	updated, _ = model.Update(copyCmd())
	model = assertModel(t, updated)
	if clipboard.calls != 1 || clipboard.text != "detached answer" {
		t.Fatalf("clipboard calls/text = %d/%q, want 1/detached answer", clipboard.calls, clipboard.text)
	}

	updated, _ = model.Update(keyPress('u', tea.ModCtrl))
	model = assertModel(t, updated)
	if strings.Contains(viewString(model), "detached answer") {
		t.Fatalf("Ctrl+U should clear BTW view:\n%s", viewString(model))
	}
	if !strings.Contains(viewString(model), "BTW") || !strings.Contains(viewString(model), "no BTW questions yet") {
		t.Fatalf("Ctrl+U should keep an empty BTW view visible:\n%s", viewString(model))
	}
	assertCursorOnBTWLine(t, model)
	assertBTWStatusLine(t, model, "idle")

	model = sendText(t, model, "second question")
	assertCursorOnBTWLine(t, model)
	updated, cmd = model.Update(keyPress(tea.KeyEnter))
	model = assertModel(t, updated)
	if cmd == nil {
		t.Fatalf("second BTW question should return a command")
	}
	model = drainSideQuestion(t, model, cmd)
	if !strings.Contains(viewString(model), "detached answer") {
		t.Fatalf("second BTW answer missing:\n%s", viewString(model))
	}

	updated, _ = model.Update(keyPress(tea.KeyEsc))
	model = assertModel(t, updated)
	if strings.Contains(viewString(model), "detached answer") {
		t.Fatalf("Esc should exit BTW view and clear its history:\n%s", viewString(model))
	}
	if model.btw.visible || len(model.btw.entries) != 0 || model.btw.draft != "" {
		t.Fatalf("Esc did not reset BTW state: %#v", model.btw)
	}
	model = sendText(t, model, "/btw")
	updated, reopenCmd := model.Update(keyPress(tea.KeyEnter))
	model = assertModel(t, updated)
	if reopenCmd != nil {
		t.Fatalf("/btw with no args should only open an empty BTW view")
	}
	if strings.Contains(viewString(model), "detached answer") || !strings.Contains(viewString(model), "no BTW questions yet") {
		t.Fatalf("/btw should not reopen an Esc-cleared answer:\n%s", viewString(model))
	}
	if got := model.MessageTexts(); len(got) != 0 {
		t.Fatalf("side question should not create transcript messages: %#v", got)
	}
}

func TestSlashBtwViewSupportsFollowUpWithSideHistory(t *testing.T) {
	runner := &scriptedRunner{sideEventScripts: [][]Event{
		{
			{Type: EventDeltaText, Text: "first answer"},
			{Type: EventDone},
		},
		{
			{Type: EventDeltaText, Text: "second answer"},
			{Type: EventDone},
		},
	}}
	model := NewModel(Options{Runner: runner, Model: "fake/test"})
	model = sendText(t, model, "/btw first question")
	updated, cmd := model.Update(keyPress(tea.KeyEnter))
	model = assertModel(t, updated)
	model = drainSideQuestion(t, model, cmd)

	model = sendText(t, model, "follow up")
	updated, cmd = model.Update(keyPress(tea.KeyEnter))
	model = assertModel(t, updated)
	if cmd == nil {
		t.Fatalf("follow-up should start a side question request")
	}
	model = drainSideQuestion(t, model, cmd)

	if runner.sideCalls != 2 {
		t.Fatalf("sideCalls = %d, want 2", runner.sideCalls)
	}
	if runner.sideQuestions[1] != "follow up" {
		t.Fatalf("follow-up question = %q, want follow up", runner.sideQuestions[1])
	}
	if len(runner.sideRequests) != 2 || len(runner.sideRequests[1].History) != 1 {
		t.Fatalf("second side request history = %#v", runner.sideRequests)
	}
	if got := runner.sideRequests[1].History[0]; got.Question != "first question" || got.Answer != "first answer" {
		t.Fatalf("history[0] = %#v, want first Q/A", got)
	}
	view := viewString(model)
	for _, want := range []string{"Q1: first question", "A1: first answer", "Q2: follow up", "A2: second answer"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
	if got := model.MessageTexts(); len(got) != 0 {
		t.Fatalf("btw side chat should not create transcript messages: %#v", got)
	}
}

func TestSlashBtwViewRendersAnswerMarkdown(t *testing.T) {
	runner := &scriptedRunner{sideEvents: []Event{
		{Type: EventDeltaText, Text: "# BTW Plan\n\n- inspect repository\n- patch renderer\n\n```go\nfmt.Println(\"btw\")\n```"},
		{Type: EventDone},
	}}
	model := NewModel(Options{
		Runner:        runner,
		Model:         "fake/test",
		initialWidth:  100,
		initialHeight: 24,
	})
	model = sendText(t, model, "/btw markdown answer")
	updated, cmd := model.Update(keyPress(tea.KeyEnter))
	model = assertModel(t, updated)
	if cmd == nil {
		t.Fatalf("/btw should return a command")
	}
	model = drainSideQuestion(t, model, cmd)

	view := viewString(model)
	for _, want := range []string{"BTW Plan", "inspect repository", "patch renderer", `fmt.Println("btw")`} {
		if !strings.Contains(view, want) {
			t.Fatalf("BTW markdown view missing %q:\n%s", want, view)
		}
	}
	for _, raw := range []string{"# BTW Plan", "```go"} {
		if strings.Contains(view, raw) {
			t.Fatalf("BTW answer should render Markdown, found raw marker %q:\n%s", raw, view)
		}
	}
}

func TestSlashBtwDedicatedViewScrollsIndependently(t *testing.T) {
	var sideEvents []Event
	for i := 1; i <= 14; i++ {
		sideEvents = append(sideEvents, Event{Type: EventDeltaText, Text: fmt.Sprintf("- side-line-%02d\n", i)})
	}
	sideEvents = append(sideEvents, Event{Type: EventDone})
	runner := &scriptedRunner{sideEvents: sideEvents}
	model := NewModel(Options{
		Runner:        runner,
		Model:         "fake/test",
		Messages:      []InitialMessage{{Role: assistantRole, Text: "main-history-marker"}},
		initialWidth:  80,
		initialHeight: 10,
	})

	model = sendText(t, model, "/btw long answer")
	updated, cmd := model.Update(keyPress(tea.KeyEnter))
	model = assertModel(t, updated)
	if cmd == nil {
		t.Fatalf("/btw should return a command")
	}
	model = drainSideQuestion(t, model, cmd)

	view := viewString(model)
	if strings.Contains(view, "main-history-marker") {
		t.Fatalf("BTW view should replace the main transcript while open:\n%s", view)
	}
	if !strings.Contains(view, "side-line-14") {
		t.Fatalf("BTW view should start at the bottom of the side answer:\n%s", view)
	}
	if model.scroll != 0 || model.btw.scroll != 0 {
		t.Fatalf("initial scrolls = main:%d btw:%d, want 0/0", model.scroll, model.btw.scroll)
	}

	updated, _ = model.Update(keyPress(tea.KeyPgUp))
	model = assertModel(t, updated)
	if model.btw.scroll == 0 {
		t.Fatalf("PgUp should scroll BTW content")
	}
	if model.scroll != 0 {
		t.Fatalf("PgUp in BTW view scrolled main transcript to %d", model.scroll)
	}
	view = viewString(model)
	if strings.Contains(view, "main-history-marker") || strings.Contains(view, "side-line-14") {
		t.Fatalf("PgUp should reveal older BTW lines, not main transcript or bottom line:\n%s", view)
	}

	previousBTWScroll := model.btw.scroll
	updated, _ = model.Update(mouseWheel(tea.MouseWheelDown))
	model = assertModel(t, updated)
	if model.btw.scroll >= previousBTWScroll {
		t.Fatalf("mouse wheel down should scroll BTW content down, got %d from %d", model.btw.scroll, previousBTWScroll)
	}
	if model.scroll != 0 {
		t.Fatalf("mouse wheel in BTW view scrolled main transcript to %d", model.scroll)
	}

	updated, _ = model.Update(keyPress(tea.KeyEnd, tea.ModCtrl))
	model = assertModel(t, updated)
	if model.btw.scroll != 0 {
		t.Fatalf("Ctrl+End should return BTW content to bottom, got scroll %d", model.btw.scroll)
	}
	if !strings.Contains(viewString(model), "side-line-14") {
		t.Fatalf("Ctrl+End should show latest BTW output:\n%s", viewString(model))
	}
}

func TestSlashBtwViewCachesRenderedBodyWhileEditingAndScrolling(t *testing.T) {
	var lines []string
	for i := 1; i <= 80; i++ {
		lines = append(lines, fmt.Sprintf("- cached-line-%02d", i))
	}
	runner := &scriptedRunner{sideEvents: []Event{
		{Type: EventDeltaText, Text: "# Cached BTW\n\n" + strings.Join(lines, "\n")},
		{Type: EventDone},
	}}
	model := NewModel(Options{
		Runner:        runner,
		Model:         "fake/test",
		initialWidth:  100,
		initialHeight: 14,
	})

	model = sendText(t, model, "/btw long cached answer")
	updated, cmd := model.Update(keyPress(tea.KeyEnter))
	model = assertModel(t, updated)
	model = drainSideQuestion(t, model, cmd)
	_ = model.View()
	cacheKey := singleSideQuestionCacheKey(t, model)
	cacheVersion := model.btw.renderVersion

	model = sendText(t, model, "draft follow up")
	_ = model.View()
	if model.btw.renderVersion != cacheVersion {
		t.Fatalf("typing draft invalidated BTW body render cache: %d -> %d", cacheVersion, model.btw.renderVersion)
	}
	if _, ok := model.btw.bodyCache[cacheKey]; !ok || len(model.btw.bodyCache) != 1 {
		t.Fatalf("typing draft changed BTW body cache: key present=%v cache=%#v", ok, model.btw.bodyCache)
	}

	updated, _ = model.Update(keyPress(tea.KeyPgUp))
	model = assertModel(t, updated)
	_ = model.View()
	if model.btw.renderVersion != cacheVersion {
		t.Fatalf("scrolling invalidated BTW body render cache: %d -> %d", cacheVersion, model.btw.renderVersion)
	}
	if _, ok := model.btw.bodyCache[cacheKey]; !ok || len(model.btw.bodyCache) != 1 {
		t.Fatalf("scrolling changed BTW body cache: key present=%v cache=%#v", ok, model.btw.bodyCache)
	}
}

func TestCopyIndexOnlyNumbersTextMessages(t *testing.T) {
	var list messageList
	list.append(userRole, "user prompt")
	list.appendOrUpdateActivity(Event{Type: EventActivity, ActivityKind: "tool", ToolName: "bash", Status: "running", Summary: "ran cmd"})
	list.append(assistantRole, "assistant answer")
	list.appendOrUpdateActivity(Event{Type: EventActivity, ActivityKind: "tool", ToolName: "read", Status: "done", Summary: "read file"})
	list.append(userRole, "second prompt")
	list.append(assistantRole, "second answer")

	// Only text messages (user/assistant) get copy indices.
	for _, item := range list.items {
		if item.kind == textMessage {
			if item.copyIndex <= 0 {
				t.Errorf("text message %q has copyIndex=%d, want >0", item.text, item.copyIndex)
			}
		} else {
			if item.copyIndex != 0 {
				t.Errorf("non-text message %q has copyIndex=%d, want 0", item.title, item.copyIndex)
			}
		}
	}

	// /copy 1 → user prompt, /copy 2 → assistant answer, /copy 3 → second prompt, /copy 4 → second answer
	cases := []struct {
		n    int
		want string
	}{
		{1, "user prompt"},
		{2, "assistant answer"},
		{3, "second prompt"},
		{4, "second answer"},
	}
	for _, tc := range cases {
		text, ok := list.copyText(tc.n)
		if !ok || text != tc.want {
			t.Errorf("copyText(%d) = %q, ok=%v; want %q, true", tc.n, text, ok, tc.want)
		}
	}
	if _, ok := list.copyText(5); ok {
		t.Error("copyText(5) should fail, only 4 text messages")
	}
}

func TestCopyIndexReindexedAfterRemove(t *testing.T) {
	var list messageList
	list.append(userRole, "first")
	list.append(assistantRole, "second")
	list.append(userRole, "third")

	// Verify initial indices.
	if text, _ := list.copyText(2); text != "second" {
		t.Fatalf("copyText(2) = %q, want second", text)
	}

	// Remove the assistant message.
	list.items = append(list.items[:1], list.items[2:]...)
	list.reindexCopy()

	// Now there are 2 text messages: "first" (1), "third" (2).
	if text, _ := list.copyText(1); text != "first" {
		t.Errorf("copyText(1) = %q, want first", text)
	}
	if text, _ := list.copyText(2); text != "third" {
		t.Errorf("copyText(2) = %q, want third", text)
	}
}

func TestLastAssistantText(t *testing.T) {
	var list messageList
	if _, ok := list.lastAssistantText(); ok {
		t.Fatal("lastAssistantText on empty list should return false")
	}

	list.append(userRole, "prompt")
	if _, ok := list.lastAssistantText(); ok {
		t.Fatal("lastAssistantText with only user message should return false")
	}

	list.append(assistantRole, "first answer")
	if text, ok := list.lastAssistantText(); !ok || text != "first answer" {
		t.Fatalf("lastAssistantText = %q, ok=%v; want first answer, true", text, ok)
	}

	list.appendOrUpdateActivity(Event{Type: EventActivity, ActivityKind: "tool", ToolName: "bash", Status: "done", Summary: "ran cmd"})
	list.append(userRole, "second prompt")
	list.append(assistantRole, "second answer")

	if text, ok := list.lastAssistantText(); !ok || text != "second answer" {
		t.Fatalf("lastAssistantText = %q, ok=%v; want second answer, true", text, ok)
	}
}

func TestSlashCopyReportsInvalidIndex(t *testing.T) {
	clipboard := &recordingClipboard{}
	model := NewModel(Options{Clipboard: clipboard, Messages: []InitialMessage{{Role: userRole, Text: "only"}}})
	model = sendText(t, model, "/copy 2")

	updated, cmd := model.Update(keyPress(tea.KeyEnter))
	if cmd != nil {
		t.Fatalf("slash copy returned unexpected command")
	}
	model = assertModel(t, updated)
	if clipboard.calls != 0 {
		t.Fatalf("clipboard calls = %d, want 0", clipboard.calls)
	}
	if got := model.MessageTexts(); len(got) != 2 || !strings.Contains(got[1], "message 2 not found") {
		t.Fatalf("messages = %#v", got)
	}
}

func TestSlashCopyRejectsExtraArgs(t *testing.T) {
	clipboard := &recordingClipboard{}
	model := NewModel(Options{
		Clipboard: clipboard,
		Messages: []InitialMessage{
			{Role: userRole, Text: "first prompt"},
			{Role: assistantRole, Text: "first answer"},
		},
	})
	model = sendText(t, model, "/copy 2 extra")

	updated, cmd := model.Update(keyPress(tea.KeyEnter))
	if cmd != nil {
		t.Fatalf("slash copy returned unexpected command")
	}
	model = assertModel(t, updated)
	if clipboard.calls != 0 {
		t.Fatalf("clipboard calls = %d, want 0", clipboard.calls)
	}
	if got := model.MessageTexts(); len(got) != 3 || !strings.Contains(got[2], "usage: /copy [N]") {
		t.Fatalf("messages = %#v", got)
	}
}

func TestSlashClear(t *testing.T) {
	model := NewModel(Options{})
	model = sendText(t, model, "hello")
	updated, _ := model.Update(keyPress(tea.KeyEnter))
	model = assertModel(t, updated)
	model = sendText(t, model, "/clear")

	updated, cmd := model.Update(keyPress(tea.KeyEnter))
	if cmd != nil {
		t.Fatalf("slash clear returned unexpected command")
	}
	model = assertModel(t, updated)
	if got := model.MessageTexts(); len(got) != 0 {
		t.Fatalf("messages = %#v, want cleared", got)
	}
}

func TestSlashNewStartsEmptySession(t *testing.T) {
	runner := &scriptedRunner{
		newSessionState: SessionState{ID: "s-new", Model: "fake/new"},
	}
	model := NewModel(Options{Runner: runner, Model: "fake/old"})
	model.messages.append(userRole, "old prompt")
	model.queuedPrompts = []string{"queued prompt"}
	model.status.turn = 3
	model.status.contextUsedTokens = 1200
	model.status.contextMaxTokens = 8000
	model.status.contextRatio = 0.15
	model = sendText(t, model, "/new")

	updated, cmd := model.Update(keyPress(tea.KeyEnter))
	if cmd != nil {
		t.Fatalf("slash new returned unexpected command")
	}
	model = assertModel(t, updated)

	if runner.newSessionCalls != 1 || runner.currentSessionID != "s-new" {
		t.Fatalf("new session calls/current = %d/%q, want 1/s-new", runner.newSessionCalls, runner.currentSessionID)
	}
	if got := model.MessageTexts(); !reflect.DeepEqual(got, []string{"new session s-new"}) {
		t.Fatalf("messages = %#v, want new-session confirmation only", got)
	}
	if got := model.QueuedPrompts(); len(got) != 0 {
		t.Fatalf("queued prompts = %#v, want cleared", got)
	}
	view := viewString(model)
	for _, unexpected := range []string{"old prompt", "ctx est:", "turn: 3", "model: fake/old"} {
		if strings.Contains(view, unexpected) {
			t.Fatalf("new session view still contains %q:\n%s", unexpected, view)
		}
	}
	if !strings.Contains(view, "model: fake/new") {
		t.Fatalf("new session view missing new model:\n%s", view)
	}
}

func TestSlashNewUnavailable(t *testing.T) {
	model := NewModel(Options{Runner: &promptOnlyRunner{}})
	model = sendText(t, model, "/new")

	updated, cmd := model.Update(keyPress(tea.KeyEnter))
	if cmd != nil {
		t.Fatalf("slash new returned unexpected command")
	}
	model = assertModel(t, updated)
	if got := model.MessageTexts(); len(got) != 1 || !strings.Contains(got[0], "new session is unavailable") {
		t.Fatalf("messages = %#v", got)
	}
}

func TestSlashQuit(t *testing.T) {
	for _, input := range []string{"/quit", "/exit"} {
		t.Run(input, func(t *testing.T) {
			model := NewModel(Options{})
			model = sendText(t, model, input)

			updated, cmd := model.Update(keyPress(tea.KeyEnter))
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
	updated, _ := model.Update(keyPress(tea.KeyEnter))
	model = assertModel(t, updated)
	if runner.model != "fake/new" || !strings.Contains(viewString(model), "model: fake/new") {
		t.Fatalf("model update failed: runner=%q view=\n%s", runner.model, viewString(model))
	}

	model = sendText(t, model, "/mode plan")
	updated, _ = model.Update(keyPress(tea.KeyEnter))
	model = assertModel(t, updated)
	if runner.mode != "plan" || !strings.Contains(viewString(model), "mode: plan") {
		t.Fatalf("mode update failed: runner=%q view=\n%s", runner.mode, viewString(model))
	}
	model = sendText(t, model, "/mode full-access")
	updated, _ = model.Update(keyPress(tea.KeyEnter))
	model = assertModel(t, updated)
	if runner.mode != "full-access" || !strings.Contains(viewString(model), "mode: full-access") {
		t.Fatalf("mode update failed: runner=%q view=\n%s", runner.mode, viewString(model))
	}
	if got, want := model.MessageTexts(), []string{"model set to fake/new"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("messages = %#v, want %#v", got, want)
	}
}

func TestSlashProviderUpdatesRunner(t *testing.T) {
	runner := &scriptedRunner{
		provider:  "first",
		providers: []string{"first", "second"},
		model:     "first/model",
		models:    []string{"first/model"},
		providerModels: map[string][]string{
			"first":  {"first/model"},
			"second": {"second/model", "second/other"},
		},
		effort:  "low",
		efforts: []string{"none", "low", "high"},
	}
	model := NewModel(Options{Runner: runner})
	model = sendText(t, model, "/provider second")

	updated, _ := model.Update(keyPress(tea.KeyEnter))
	model = assertModel(t, updated)
	view := viewString(model)
	if runner.provider != "second" || runner.model != "second/model" {
		t.Fatalf("runner provider/model = %q/%q, want second/second/model", runner.provider, runner.model)
	}
	for _, want := range []string{"model: second/model", "provider set to second model second/model"} {
		if !strings.Contains(view, want) {
			t.Fatalf("provider switch view missing %q:\n%s", want, view)
		}
	}
}

func TestSlashProviderWithExplicitModel(t *testing.T) {
	runner := &scriptedRunner{
		provider:  "first",
		providers: []string{"first", "second"},
		model:     "first/model",
		models:    []string{"first/model"},
		providerModels: map[string][]string{
			"first":  {"first/model"},
			"second": {"second/model", "second/other"},
		},
	}
	model := NewModel(Options{Runner: runner})
	model = sendText(t, model, "/provider second second/other")

	updated, _ := model.Update(keyPress(tea.KeyEnter))
	model = assertModel(t, updated)
	if runner.provider != "second" || runner.model != "second/other" || !strings.Contains(viewString(model), "model: second/other") {
		t.Fatalf("explicit provider model failed: runner=%q/%q view=\n%s", runner.provider, runner.model, viewString(model))
	}
}

func TestSlashProviderWithoutArgsListsCandidates(t *testing.T) {
	runner := &scriptedRunner{
		provider:  "first",
		providers: []string{"first", "second"},
		model:     "first/model",
		models:    []string{"first/model"},
		providerModels: map[string][]string{
			"first":  {"first/model"},
			"second": {"second/model"},
		},
	}
	model := NewModel(Options{Runner: runner})
	model = sendText(t, model, "/provider")

	updated, cmd := model.Update(keyPress(tea.KeyEnter))
	if cmd != nil {
		t.Fatalf("slash provider returned unexpected command")
	}
	model = assertModel(t, updated)
	view := viewString(model)
	for _, want := range []string{"select provider", "> first", "  second"} {
		if !strings.Contains(view, want) {
			t.Fatalf("provider picker missing %q:\n%s", want, view)
		}
	}

	updated, _ = model.Update(keyPress(tea.KeyDown))
	model = assertModel(t, updated)
	updated, _ = model.Update(keyPress(tea.KeyEnter))
	model = assertModel(t, updated)
	if runner.provider != "second" || runner.model != "second/model" || !strings.Contains(viewString(model), "model: second/model") {
		t.Fatalf("provider picker selection failed: runner=%q/%q view=\n%s", runner.provider, runner.model, viewString(model))
	}
}

func TestSlashEffortUpdatesRunner(t *testing.T) {
	runner := &scriptedRunner{effort: "low", efforts: []string{"none", "low", "high"}}
	model := NewModel(Options{Runner: runner, Model: "fake/model", Effort: runner.effort, Efforts: runner.efforts})
	model = sendText(t, model, "/effort high")

	updated, _ := model.Update(keyPress(tea.KeyEnter))
	model = assertModel(t, updated)
	if runner.effort != "high" || !strings.Contains(viewString(model), "effort: high") {
		t.Fatalf("effort update failed: runner=%q view=\n%s", runner.effort, viewString(model))
	}
	if got := model.MessageTexts(); len(got) != 1 || got[0] != "effort set to high" {
		t.Fatalf("messages = %#v", got)
	}
}

func TestSlashEffortWithoutArgsListsCandidates(t *testing.T) {
	runner := &scriptedRunner{effort: "low", efforts: []string{"none", "low", "high"}}
	model := NewModel(Options{Runner: runner, Model: "fake/model", Effort: runner.effort, Efforts: runner.efforts})
	model = sendText(t, model, "/effort")

	updated, cmd := model.Update(keyPress(tea.KeyEnter))
	if cmd != nil {
		t.Fatalf("slash effort returned unexpected command")
	}
	model = assertModel(t, updated)
	view := viewString(model)
	for _, want := range []string{"select effort", "> low", "  high"} {
		if !strings.Contains(view, want) {
			t.Fatalf("effort picker missing %q:\n%s", want, view)
		}
	}

	updated, _ = model.Update(keyPress(tea.KeyDown))
	model = assertModel(t, updated)
	updated, _ = model.Update(keyPress(tea.KeyEnter))
	model = assertModel(t, updated)
	if runner.effort != "high" || !strings.Contains(viewString(model), "effort: high") {
		t.Fatalf("effort picker selection failed: runner=%q view=\n%s", runner.effort, viewString(model))
	}
}

func TestSlashEffortRejectsUnsupportedCandidate(t *testing.T) {
	runner := &scriptedRunner{effort: "low", efforts: []string{"none", "low"}}
	model := NewModel(Options{Runner: runner, Model: "fake/model", Effort: runner.effort, Efforts: runner.efforts})
	model = sendText(t, model, "/effort high")

	updated, _ := model.Update(keyPress(tea.KeyEnter))
	model = assertModel(t, updated)
	if runner.effort != "low" {
		t.Fatalf("runner effort changed to %q, want low", runner.effort)
	}
	view := viewString(model)
	if !strings.Contains(view, "effort: low") || !strings.Contains(view, "not available") {
		t.Fatalf("invalid effort handling failed:\n%s", view)
	}
}

func TestSlashModelWithoutArgsListsCandidates(t *testing.T) {
	runner := &scriptedRunner{models: []string{"fake/old", "fake/new"}}
	model := NewModel(Options{Runner: runner, Model: "fake/old", Models: runner.models})
	model = sendText(t, model, "/model")

	updated, cmd := model.Update(keyPress(tea.KeyEnter))
	if cmd != nil {
		t.Fatalf("slash model returned unexpected command")
	}
	model = assertModel(t, updated)
	if runner.model != "" {
		t.Fatalf("runner model changed to %q, want unchanged", runner.model)
	}
	view := viewString(model)
	for _, want := range []string{"select model", "> fake/old", "  fake/new"} {
		if !strings.Contains(view, want) {
			t.Fatalf("model picker missing %q:\n%s", want, view)
		}
	}

	updated, _ = model.Update(keyPress(tea.KeyDown))
	model = assertModel(t, updated)
	updated, _ = model.Update(keyPress(tea.KeyEnter))
	model = assertModel(t, updated)
	if runner.model != "fake/new" || !strings.Contains(viewString(model), "model: fake/new") {
		t.Fatalf("model picker selection failed: runner=%q view=\n%s", runner.model, viewString(model))
	}
}

func TestAsyncModelRefreshUpdatesOpenPicker(t *testing.T) {
	runner := &scriptedRunner{
		model:         "fake/current",
		models:        []string{"fake/current"},
		refreshModels: []string{"fake/current", "fake/remote"},
	}
	model := NewModel(Options{Runner: runner, Model: "fake/current", Models: runner.models})
	refreshMsg := initModelRefreshMsg(t, model)
	if runner.refreshModelCalls != 1 {
		t.Fatalf("refresh model calls = %d, want 1", runner.refreshModelCalls)
	}

	model = sendText(t, model, "/model")

	updated, cmd := model.Update(keyPress(tea.KeyEnter))
	if cmd != nil {
		t.Fatalf("slash model returned unexpected command")
	}
	model = assertModel(t, updated)
	if runner.refreshModelCalls != 1 {
		t.Fatalf("slash model triggered refresh calls = %d, want 1", runner.refreshModelCalls)
	}
	view := viewString(model)
	if strings.Contains(view, "fake/remote") {
		t.Fatalf("model picker included async candidate before refresh msg:\n%s", view)
	}

	updated, cmd = model.Update(refreshMsg)
	if cmd != nil {
		t.Fatalf("refresh returned unexpected command")
	}
	model = assertModel(t, updated)
	view = viewString(model)
	for _, want := range []string{"> fake/current", "  fake/remote"} {
		if !strings.Contains(view, want) {
			t.Fatalf("model picker missing refreshed candidate %q:\n%s", want, view)
		}
	}

	updated, _ = model.Update(keyPress(tea.KeyDown))
	model = assertModel(t, updated)
	updated, _ = model.Update(keyPress(tea.KeyEnter))
	model = assertModel(t, updated)
	if runner.model != "fake/remote" || !strings.Contains(viewString(model), "model: fake/remote") {
		t.Fatalf("refreshed model selection failed: runner=%q view=\n%s", runner.model, viewString(model))
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

	updated, _ := model.Update(keyPress(tea.KeyEnter))
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

	updated, cmd := model.Update(keyPress(tea.KeyEnter))
	if cmd != nil {
		t.Fatalf("slash approval-model returned unexpected command")
	}
	model = assertModel(t, updated)
	view := viewString(model)
	for _, want := range []string{"select model", "> fake/review-old", "  fake/review-new"} {
		if !strings.Contains(view, want) {
			t.Fatalf("approval model picker missing %q:\n%s", want, view)
		}
	}
	updated, _ = model.Update(keyPress(tea.KeyDown))
	model = assertModel(t, updated)
	updated, _ = model.Update(keyPress(tea.KeyEnter))
	model = assertModel(t, updated)
	if runner.approvalModel != "fake/review-new" {
		t.Fatalf("approval model = %q, want fake/review-new", runner.approvalModel)
	}
}

func TestAsyncApprovalModelRefreshUpdatesOpenPicker(t *testing.T) {
	runner := &scriptedRunner{
		approvalModel:         "fake/review-current",
		approvalModels:        []string{"fake/review-current"},
		refreshApprovalModels: []string{"fake/review-current", "fake/review-remote"},
	}
	model := NewModel(Options{
		Runner:         runner,
		ApprovalModel:  runner.approvalModel,
		ApprovalModels: runner.approvalModels,
	})
	refreshMsg := initModelRefreshMsg(t, model)
	if runner.refreshApprovalCalls != 1 {
		t.Fatalf("refresh approval calls = %d, want 1", runner.refreshApprovalCalls)
	}

	model = sendText(t, model, "/approval-model")

	updated, cmd := model.Update(keyPress(tea.KeyEnter))
	if cmd != nil {
		t.Fatalf("slash approval-model returned unexpected command")
	}
	model = assertModel(t, updated)
	if runner.refreshApprovalCalls != 1 {
		t.Fatalf("slash approval-model triggered refresh calls = %d, want 1", runner.refreshApprovalCalls)
	}
	view := viewString(model)
	if strings.Contains(view, "fake/review-remote") {
		t.Fatalf("approval model picker included async candidate before refresh msg:\n%s", view)
	}

	updated, cmd = model.Update(refreshMsg)
	if cmd != nil {
		t.Fatalf("refresh returned unexpected command")
	}
	model = assertModel(t, updated)
	view = viewString(model)
	for _, want := range []string{"> fake/review-current", "  fake/review-remote"} {
		if !strings.Contains(view, want) {
			t.Fatalf("approval model picker missing refreshed candidate %q:\n%s", want, view)
		}
	}

	updated, _ = model.Update(keyPress(tea.KeyDown))
	model = assertModel(t, updated)
	updated, _ = model.Update(keyPress(tea.KeyEnter))
	model = assertModel(t, updated)
	if runner.approvalModel != "fake/review-remote" {
		t.Fatalf("approval model = %q, want fake/review-remote", runner.approvalModel)
	}
}

func TestSlashSmallModelUpdatesRunner(t *testing.T) {
	runner := &scriptedRunner{
		smallModel:  "fake/small-old",
		smallModels: []string{"fake/small-old", "fake/small-new"},
	}
	model := NewModel(Options{
		Runner:      runner,
		SmallModel:  runner.smallModel,
		SmallModels: runner.smallModels,
	})
	model = sendText(t, model, "/small-model fake/small-new")

	updated, _ := model.Update(keyPress(tea.KeyEnter))
	model = assertModel(t, updated)
	if runner.smallModel != "fake/small-new" {
		t.Fatalf("small model = %q, want fake/small-new", runner.smallModel)
	}
	if got := model.MessageTexts(); len(got) != 1 || got[0] != "small model set to fake/small-new" {
		t.Fatalf("messages = %#v", got)
	}
}

func TestSlashSmallModelWithoutArgsListsCandidates(t *testing.T) {
	runner := &scriptedRunner{
		smallModel:  "fake/small-old",
		smallModels: []string{"fake/small-old", "fake/small-new"},
	}
	model := NewModel(Options{
		Runner:      runner,
		SmallModel:  runner.smallModel,
		SmallModels: runner.smallModels,
	})
	model = sendText(t, model, "/small-model")

	updated, cmd := model.Update(keyPress(tea.KeyEnter))
	if cmd != nil {
		t.Fatalf("slash small-model returned unexpected command")
	}
	model = assertModel(t, updated)
	view := viewString(model)
	for _, want := range []string{"select model", "> fake/small-old", "  fake/small-new"} {
		if !strings.Contains(view, want) {
			t.Fatalf("small model picker missing %q:\n%s", want, view)
		}
	}
	updated, _ = model.Update(keyPress(tea.KeyDown))
	model = assertModel(t, updated)
	updated, _ = model.Update(keyPress(tea.KeyEnter))
	model = assertModel(t, updated)
	if runner.smallModel != "fake/small-new" {
		t.Fatalf("small model = %q, want fake/small-new", runner.smallModel)
	}
}

func TestAsyncSmallModelRefreshUpdatesOpenPicker(t *testing.T) {
	runner := &scriptedRunner{
		smallModel:         "fake/small-current",
		smallModels:        []string{"fake/small-current"},
		refreshSmallModels: []string{"fake/small-current", "fake/small-remote"},
	}
	model := NewModel(Options{
		Runner:      runner,
		SmallModel:  runner.smallModel,
		SmallModels: runner.smallModels,
	})
	refreshMsg := initModelRefreshMsg(t, model)
	if runner.refreshSmallCalls != 1 {
		t.Fatalf("refresh small calls = %d, want 1", runner.refreshSmallCalls)
	}

	model = sendText(t, model, "/small-model")

	updated, cmd := model.Update(keyPress(tea.KeyEnter))
	if cmd != nil {
		t.Fatalf("slash small-model returned unexpected command")
	}
	model = assertModel(t, updated)
	if runner.refreshSmallCalls != 1 {
		t.Fatalf("slash small-model triggered refresh calls = %d, want 1", runner.refreshSmallCalls)
	}
	view := viewString(model)
	if strings.Contains(view, "fake/small-remote") {
		t.Fatalf("small model picker included async candidate before refresh msg:\n%s", view)
	}

	updated, cmd = model.Update(refreshMsg)
	if cmd != nil {
		t.Fatalf("refresh returned unexpected command")
	}
	model = assertModel(t, updated)
	view = viewString(model)
	for _, want := range []string{"> fake/small-current", "  fake/small-remote"} {
		if !strings.Contains(view, want) {
			t.Fatalf("small model picker missing refreshed candidate %q:\n%s", want, view)
		}
	}

	updated, _ = model.Update(keyPress(tea.KeyDown))
	model = assertModel(t, updated)
	updated, _ = model.Update(keyPress(tea.KeyEnter))
	model = assertModel(t, updated)
	if runner.smallModel != "fake/small-remote" {
		t.Fatalf("small model = %q, want fake/small-remote", runner.smallModel)
	}
}

func TestSlashProviderSyncsApprovalAndSmallModelState(t *testing.T) {
	runner := &scriptedRunner{
		provider: "old",
		providers: []string{
			"old",
			"new",
		},
		providerModels: map[string][]string{
			"old": {"old/main"},
			"new": {"new/main"},
		},
		providerApprovalModels: map[string][]string{
			"old": {"old/review"},
			"new": {"new/review", "new/review-plus"},
		},
		providerSmallModels: map[string][]string{
			"old": {"old/small"},
			"new": {"new/small"},
		},
		model:          "old/main",
		models:         []string{"old/main"},
		approvalModel:  "old/review",
		approvalModels: []string{"old/review"},
		smallModel:     "old/small",
		smallModels:    []string{"old/small"},
	}
	model := NewModel(Options{
		Runner:         runner,
		Provider:       runner.provider,
		Providers:      runner.providers,
		Model:          runner.model,
		Models:         runner.models,
		ApprovalModel:  runner.approvalModel,
		ApprovalModels: runner.approvalModels,
		SmallModel:     runner.smallModel,
		SmallModels:    runner.smallModels,
	})

	model = sendText(t, model, "/provider new")
	updated, _ := model.Update(keyPress(tea.KeyEnter))
	model = assertModel(t, updated)
	model = sendText(t, model, "/config")
	updated, _ = model.Update(keyPress(tea.KeyEnter))
	model = assertModel(t, updated)
	texts := model.MessageTexts()
	if len(texts) < 2 || !strings.Contains(texts[len(texts)-1], "approval_model=new/review small_model=new/small") {
		t.Fatalf("config did not sync approval/small models: %#v", texts)
	}

	model = sendText(t, model, "/approval-model")
	updated, _ = model.Update(keyPress(tea.KeyEnter))
	model = assertModel(t, updated)
	view := viewString(model)
	if !strings.Contains(view, "> new/review") || !strings.Contains(view, "  new/review-plus") || strings.Contains(view, "old/review") {
		t.Fatalf("approval picker not synced to new provider:\n%s", view)
	}
	updated, _ = model.Update(keyPress(tea.KeyEsc))
	model = assertModel(t, updated)

	model = sendText(t, model, "/small-model")
	updated, _ = model.Update(keyPress(tea.KeyEnter))
	model = assertModel(t, updated)
	view = viewString(model)
	if !strings.Contains(view, "> new/small") || strings.Contains(view, "old/small") {
		t.Fatalf("small picker not synced to new provider:\n%s", view)
	}
}

func TestSlashModelRejectsUnsupportedCandidate(t *testing.T) {
	runner := &scriptedRunner{models: []string{"fake/old", "fake/new"}}
	model := NewModel(Options{Runner: runner, Model: "fake/old", Models: runner.models})
	model = sendText(t, model, "/model fake/missing")

	updated, _ := model.Update(keyPress(tea.KeyEnter))
	model = assertModel(t, updated)
	if runner.model != "" {
		t.Fatalf("runner model changed to %q, want unchanged", runner.model)
	}
	view := viewString(model)
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
	if got := model.MessageTexts(); len(got) != 1 || got[0] != "Permission approval_agent allow bash: read-only command" {
		t.Fatalf("messages = %#v, want permission item text", got)
	}
	updated, cmd = model.Update(mouseClick(0, 0))
	if cmd != nil {
		t.Fatalf("mouse click returned unexpected command")
	}
	model = assertModel(t, updated)
	view := viewString(model)
	if !strings.Contains(view, "approval_agent") || !strings.Contains(view, "read-only command") {
		t.Fatalf("expanded permission view missing detail:\n%s", view)
	}
}

func TestPermissionActivityChipUsesBlockTitleCase(t *testing.T) {
	item := activityMessage(Event{
		Type:         EventActivity,
		ActivityKind: "permission",
		ToolName:     "bash",
		Source:       "approval_agent",
		Decision:     "allow",
		Allowed:      true,
		Reason:       "read-only command",
	})
	got := activityChipText(item, 80)
	if !strings.Contains(got, "Permission allow bash") {
		t.Fatalf("chip title = %q, want Permission allow bash", got)
	}
}

func TestShiftTabCyclesMode(t *testing.T) {
	runner := &scriptedRunner{}
	model := NewModel(Options{Runner: runner, ExecutionMode: "work"})

	updated, cmd := model.Update(keyPress(tea.KeyTab, tea.ModShift))
	if cmd != nil {
		t.Fatalf("shift+tab returned unexpected command")
	}
	model = assertModel(t, updated)
	if runner.mode != "plan" || !strings.Contains(viewString(model), "mode: plan") {
		t.Fatalf("first shift+tab failed: runner=%q view=\n%s", runner.mode, viewString(model))
	}
	if got := model.MessageTexts(); len(got) != 0 {
		t.Fatalf("messages = %#v, want no mode switch log", got)
	}

	updated, _ = model.Update(keyPress(tea.KeyTab, tea.ModShift))
	model = assertModel(t, updated)
	if runner.mode != "auto" || !strings.Contains(viewString(model), "mode: auto") {
		t.Fatalf("second shift+tab failed: runner=%q view=\n%s", runner.mode, viewString(model))
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

	updated, cmd := model.Update(keyPress(tea.KeyTab, tea.ModShift))
	if cmd != nil {
		t.Fatalf("shift+tab returned unexpected command")
	}
	model = assertModel(t, updated)
	if runner.mode != "plan" || !strings.Contains(viewString(model), "mode: plan") || !model.Running() {
		t.Fatalf("running mode switch failed: runner=%q running=%v view=\n%s", runner.mode, model.Running(), viewString(model))
	}
}

func TestNextExecutionModeIncludesFullAccess(t *testing.T) {
	cases := []struct {
		current string
		want    string
	}{
		{current: string(execution.ModeWork), want: string(execution.ModePlan)},
		{current: string(execution.ModePlan), want: string(execution.ModeAuto)},
		{current: string(execution.ModeAuto), want: string(execution.ModeFullAccess)},
		{current: string(execution.ModeFullAccess), want: string(execution.ModeWork)},
	}
	for _, tc := range cases {
		if got := nextExecutionMode(tc.current); got != tc.want {
			t.Fatalf("nextExecutionMode(%q) = %q, want %q", tc.current, got, tc.want)
		}
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

	updated, cmd := model.Update(keyPress(tea.KeyTab, tea.ModShift))
	if cmd != nil {
		t.Fatalf("shift+tab returned unexpected command")
	}
	model = assertModel(t, updated)
	if runner.mode != "plan" || !strings.Contains(viewString(model), "mode: plan") {
		t.Fatalf("permission mode switch failed: runner=%q view=\n%s", runner.mode, viewString(model))
	}
	if !strings.Contains(viewString(model), "mode: plan") {
		t.Fatalf("permission modal did not reflect mode switch:\n%s", viewString(model))
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

	updated, cmd = model.Update(keyPress(tea.KeyTab, tea.ModShift))
	if cmd != nil {
		t.Fatalf("shift+tab returned unexpected command")
	}
	model = assertModel(t, updated)
	if runner.mode != "auto" || !strings.Contains(viewString(model), "mode: auto") {
		t.Fatalf("permission mode switch failed: runner=%q view=\n%s", runner.mode, viewString(model))
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
	if got := model.MessageTexts(); len(got) != 1 || got[0] != "Ran cmd=go test ./..." {
		t.Fatalf("messages = %#v, want done tool item while permission is open", got)
	}
	if model.pending == nil {
		t.Fatal("stream event should not close pending permission modal")
	}

	updated, cmd = model.Update(keyPress(tea.KeyEnter))
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

	updated, cmd := model.Update(keyPress(tea.KeyTab))
	if cmd != nil {
		t.Fatalf("tab returned unexpected command")
	}
	model = assertModel(t, updated)
	if got := model.InputValue(); got != "/model " {
		t.Fatalf("input = %q, want /model ", got)
	}
	if runner.mode != "" || !strings.Contains(viewString(model), "mode: work") {
		t.Fatalf("tab unexpectedly changed mode: runner=%q view=\n%s", runner.mode, viewString(model))
	}
}

func TestTabCompletesSlashValueCandidate(t *testing.T) {
	model := NewModel(Options{Model: "fake/current", Effort: "low", Efforts: []string{"none", "low", "high"}})
	model = sendText(t, model, "/effort h")

	updated, cmd := model.Update(keyPress(tea.KeyTab))
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

	updated, cmd := model.Update(keyPress(tea.KeyEnter))
	if cmd != nil {
		t.Fatalf("enter returned unexpected command")
	}
	model = assertModel(t, updated)
	if runner.effort != "high" || model.InputValue() != "" {
		t.Fatalf("effort/input = %q/%q, want high/empty", runner.effort, model.InputValue())
	}
	if !strings.Contains(viewString(model), "effort set to high") || !strings.Contains(viewString(model), "effort: high") {
		t.Fatalf("enter did not apply selected effort:\n%s", viewString(model))
	}
}

func TestArrowSelectsSlashValueCandidate(t *testing.T) {
	runner := &scriptedRunner{effort: "none", efforts: []string{"none", "low", "high"}}
	model := NewModel(Options{Runner: runner, Model: "fake/current", Effort: "none", Efforts: runner.efforts})
	model = sendText(t, model, "/effort ")

	updated, cmd := model.Update(keyPress(tea.KeyDown))
	if cmd != nil {
		t.Fatalf("down returned unexpected command")
	}
	model = assertModel(t, updated)
	if view := viewString(model); !strings.Contains(view, "  none") || !strings.Contains(view, "> low") {
		t.Fatalf("down did not move effort selection:\n%s", view)
	}

	updated, cmd = model.Update(keyPress(tea.KeyEnter))
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

	view := viewString(model)
	if !strings.Contains(view, "> /model [model]") || !strings.Contains(view, "  /mode <work|plan|auto|full-access>") {
		t.Fatalf("initial slash selection missing:\n%s", view)
	}
	updated, cmd := model.Update(keyPress(tea.KeyDown))
	if cmd != nil {
		t.Fatalf("down returned unexpected command")
	}
	model = assertModel(t, updated)
	view = viewString(model)
	if !strings.Contains(view, "  /model [model]") || !strings.Contains(view, "> /mode <work|plan|auto|full-access>") {
		t.Fatalf("down did not move slash selection:\n%s", view)
	}

	updated, cmd = model.Update(keyPress(tea.KeyEnter))
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
	updated, _ := model.Update(keyPress(tea.KeyEnter))
	model = assertModel(t, updated)
	model = sendText(t, model, "second")
	updated, _ = model.Update(keyPress(tea.KeyEnter))
	model = assertModel(t, updated)

	updated, _ = model.Update(keyPress(tea.KeyUp))
	model = assertModel(t, updated)
	if got := model.InputValue(); got != "second" {
		t.Fatalf("first up input = %q, want second", got)
	}
	updated, _ = model.Update(keyPress(tea.KeyUp))
	model = assertModel(t, updated)
	if got := model.InputValue(); got != "first" {
		t.Fatalf("second up input = %q, want first", got)
	}
	updated, _ = model.Update(keyPress(tea.KeyDown))
	model = assertModel(t, updated)
	if got := model.InputValue(); got != "second" {
		t.Fatalf("first down input = %q, want second", got)
	}
	updated, _ = model.Update(keyPress(tea.KeyDown))
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

	updated, _ := model.Update(keyPress(tea.KeyUp))
	model = assertModel(t, updated)
	if got := model.InputValue(); got != "past prompt" {
		t.Fatalf("input = %q, want restored prompt", got)
	}
}

func TestAsyncInitialMessagesLoadAfterStartup(t *testing.T) {
	loads := 0
	model := NewModel(Options{
		LoadMessages: func(context.Context) ([]InitialMessage, error) {
			loads++
			return []InitialMessage{
				{Role: userRole, Text: "restored prompt"},
				{Role: assistantRole, Text: "restored answer"},
			}, nil
		},
	})
	if !model.loadingMessages {
		t.Fatal("model should start with async message loading enabled")
	}
	if got := model.MessageTexts(); len(got) != 1 || got[0] != "loading session history..." {
		t.Fatalf("messages = %#v, want loading placeholder", got)
	}

	cmd := loadMessagesCmd(context.Background(), model.loadMessages)
	updated, next := model.Update(cmd())
	if next != nil {
		t.Fatalf("message load returned unexpected command")
	}
	model = assertModel(t, updated)
	if loads != 1 {
		t.Fatalf("loads = %d, want 1", loads)
	}
	if model.loadingMessages {
		t.Fatal("model should stop loading after messages load")
	}
	if got, want := model.MessageTexts(), []string{"restored prompt", "restored answer"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("messages = %#v, want %#v", got, want)
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

	view := viewString(model)
	if !strings.Contains(view, "message-06") || strings.Contains(view, "message-01") {
		t.Fatalf("initial view should show bottom of message area:\n%s", view)
	}

	updated, _ = model.Update(keyPress(tea.KeyPgUp))
	model = assertModel(t, updated)
	view = viewString(model)
	if !strings.Contains(view, "message-03") || strings.Contains(view, "message-06") {
		t.Fatalf("pgup did not scroll message area up:\n%s", view)
	}

	updated, _ = model.Update(mouseWheel(tea.MouseWheelDown))
	model = assertModel(t, updated)
	view = viewString(model)
	if !strings.Contains(view, "message-06") {
		t.Fatalf("mouse wheel down did not return to bottom:\n%s", view)
	}
}

func TestMessageAreaJumpsToStartAndEnd(t *testing.T) {
	var messages []InitialMessage
	for _, text := range []string{"message-01", "message-02", "message-03", "message-04", "message-05", "message-06"} {
		messages = append(messages, InitialMessage{Role: assistantRole, Text: text})
	}
	model := NewModel(Options{Messages: messages})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 8})
	model = assertModel(t, updated)

	updated, cmd := model.Update(keyPress(tea.KeyHome, tea.ModCtrl))
	if cmd != nil {
		t.Fatalf("ctrl+home returned unexpected command")
	}
	model = assertModel(t, updated)
	view := viewString(model)
	if !strings.Contains(view, "message-01") || strings.Contains(view, "message-06") {
		t.Fatalf("ctrl+home did not jump to transcript start:\n%s", view)
	}

	updated, cmd = model.Update(keyPress(tea.KeyEnd, tea.ModCtrl))
	if cmd != nil {
		t.Fatalf("ctrl+end returned unexpected command")
	}
	model = assertModel(t, updated)
	view = viewString(model)
	if !strings.Contains(view, "message-06") || strings.Contains(view, "message-01") {
		t.Fatalf("ctrl+end did not jump to transcript end:\n%s", view)
	}
}

func TestMessageAreaJumpsToStartWithStyledMarkdownLineCount(t *testing.T) {
	markdown := strings.Repeat("# Heading\n\n", 24)
	model := NewModel(Options{
		Theme: "pink",
		Messages: []InitialMessage{
			{Role: userRole, Text: "first prompt"},
			{Role: assistantRole, Text: markdown},
		},
	})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 10})
	model = assertModel(t, updated)

	width := contentWidth(model.width)
	plainLines := len(model.messages.render(width, tuitheme.Plain()).lines)
	styledLines := len(model.messages.render(width, model.styles).lines)
	if styledLines <= plainLines {
		t.Fatalf("test setup needs styled markdown to be taller: styled=%d plain=%d", styledLines, plainLines)
	}

	updated, cmd := model.Update(keyPress(tea.KeyHome, tea.ModCtrl))
	if cmd != nil {
		t.Fatalf("ctrl+home returned unexpected command")
	}
	model = assertModel(t, updated)
	view := viewString(model)
	if !strings.Contains(view, "first prompt") {
		t.Fatalf("ctrl+home should reach the first prompt with styled markdown:\n%s", view)
	}
}

func TestFramePadsToTerminalSize(t *testing.T) {
	model := NewModel(Options{Messages: []InitialMessage{{Role: assistantRole, Text: "short"}}})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 72, Height: 16})
	model = assertModel(t, updated)

	lines := strings.Split(model.View().Content, "\n")
	if len(lines) != 16 {
		t.Fatalf("frame lines = %d, want 16:\n%s", len(lines), model.View().Content)
	}
	for i, line := range lines {
		if got := xansi.StringWidth(line); got != 72 {
			t.Fatalf("line %d width = %d, want 72: %q", i, got, xansi.Strip(line))
		}
	}
}

func TestInitialViewUsesFallbackTerminalSize(t *testing.T) {
	var messages []InitialMessage
	for i := 1; i <= 40; i++ {
		messages = append(messages, InitialMessage{Role: assistantRole, Text: fmt.Sprintf("message-%02d", i)})
	}
	model := NewModel(Options{Messages: messages})

	view := viewString(model)
	lines := strings.Split(view, "\n")
	if len(lines) != defaultViewHeight {
		t.Fatalf("initial frame lines = %d, want fallback height %d:\n%s", len(lines), defaultViewHeight, view)
	}
	if !strings.Contains(view, "state: idle") {
		t.Fatalf("fallback frame should keep the status footer visible:\n%s", view)
	}
	if !strings.Contains(view, "message-40") || strings.Contains(view, "message-01") {
		t.Fatalf("fallback frame should constrain the message pane to the bottom:\n%s", view)
	}
}

func TestActivityMessagesScrollWithinWindow(t *testing.T) {
	model := NewModel(Options{})
	for _, text := range []string{"activity-01", "activity-02", "activity-03", "activity-04", "activity-05", "activity-06"} {
		model.messages.append(activityRole, text)
	}
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 8})
	model = assertModel(t, updated)

	view := viewString(model)
	if !strings.Contains(view, "activity-06") || strings.Contains(view, "activity-01") {
		t.Fatalf("initial view should show bottom of activity messages:\n%s", view)
	}
	updated, _ = model.Update(keyPress(tea.KeyPgUp))
	model = assertModel(t, updated)
	view = viewString(model)
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

	updated, cmd := model.Update(keyPress(tea.KeyPgUp))
	if cmd != nil {
		t.Fatalf("pgup returned unexpected command")
	}
	model = assertModel(t, updated)
	view := viewString(model)
	if !strings.Contains(view, "message-03") {
		t.Fatalf("pgup did not scroll while permission modal is open:\n%s", view)
	}
}

func TestSlashSuggestionsRenderUsage(t *testing.T) {
	model := NewModel(Options{})
	model = sendText(t, model, "/m")

	view := viewString(model)
	if !strings.Contains(view, "/model [model]") || !strings.Contains(view, "supported model") {
		t.Fatalf("view missing slash suggestions:\n%s", view)
	}
}

func TestSlashModelSuggestionsRenderCandidates(t *testing.T) {
	model := NewModel(Options{Model: "fake/current", Models: []string{"fake/new", "fake/other"}})
	model = sendText(t, model, "/model new")

	view := viewString(model)
	if !strings.Contains(view, "> fake/new") || strings.Contains(view, "fake/other") {
		t.Fatalf("view missing filtered model suggestions:\n%s", view)
	}
}

func TestSlashEffortSuggestionsRenderCandidates(t *testing.T) {
	model := NewModel(Options{Model: "fake/current", Effort: "low", Efforts: []string{"none", "low", "high"}})
	model = sendText(t, model, "/effort h")

	view := viewString(model)
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

	updated, cmd := model.Update(keyPress(tea.KeyEnter))
	if cmd != nil {
		t.Fatalf("slash sessions returned unexpected command")
	}
	model = assertModel(t, updated)
	if !strings.Contains(viewString(model), "select session") || !strings.Contains(viewString(model), "s2") {
		t.Fatalf("sessions picker missing:\n%s", viewString(model))
	}

	updated, _ = model.Update(keyPress(tea.KeyDown))
	model = assertModel(t, updated)
	updated, _ = model.Update(keyPress(tea.KeyEnter))
	model = assertModel(t, updated)
	if runner.currentSessionID != "s2" {
		t.Fatalf("current session = %q, want s2", runner.currentSessionID)
	}
	if got := model.MessageTexts(); !reflect.DeepEqual(got, []string{"old prompt", "old answer", "session set to s2"}) {
		t.Fatalf("messages = %#v", got)
	}
	if !strings.Contains(viewString(model), "model: fake/two") || !strings.Contains(viewString(model), "mode: work") || !strings.Contains(viewString(model), "turn: 3") {
		t.Fatalf("view missing restored state:\n%s", viewString(model))
	}
}

func TestSlashSessionsPickerFiltersAndSwitchesSession(t *testing.T) {
	runner := &scriptedRunner{
		sessions: []SessionInfo{
			{ID: "s1", Title: "Planning", Model: "fake/one", Current: true},
			{ID: "s2", Title: "Release Notes", Model: "fake/two"},
		},
		sessionStates: map[string]SessionState{
			"s2": {
				ID:    "s2",
				Model: "fake/two",
			},
		},
	}
	model := NewModel(Options{Runner: runner, Model: "fake/one"})
	model = sendText(t, model, "/sessions")

	updated, _ := model.Update(keyPress(tea.KeyEnter))
	model = assertModel(t, updated)
	updated, _ = model.Update(runePress('r'))
	model = assertModel(t, updated)
	updated, _ = model.Update(runePress('n'))
	model = assertModel(t, updated)

	view := viewString(model)
	if !strings.Contains(view, "filter: rn") || !strings.Contains(view, "Release Notes") || strings.Contains(view, "Planning") {
		t.Fatalf("sessions picker did not filter by typed query:\n%s", view)
	}

	updated, _ = model.Update(keyPress(tea.KeyEnter))
	model = assertModel(t, updated)
	if runner.currentSessionID != "s2" {
		t.Fatalf("current session = %q, want s2", runner.currentSessionID)
	}
}

func TestSlashResumeOpensSessionPicker(t *testing.T) {
	runner := &scriptedRunner{
		sessions: []SessionInfo{
			{ID: "s1", Title: "First", Model: "fake/one", Current: true},
			{ID: "s2", Title: "Second", Model: "fake/two"},
		},
	}
	model := NewModel(Options{Runner: runner, Model: "fake/one"})
	model = sendText(t, model, "/resume")

	updated, cmd := model.Update(keyPress(tea.KeyEnter))
	if cmd != nil {
		t.Fatalf("slash resume returned unexpected command")
	}
	model = assertModel(t, updated)
	if !strings.Contains(viewString(model), "select session") || !strings.Contains(viewString(model), "s2") {
		t.Fatalf("resume picker missing:\n%s", viewString(model))
	}
}

func TestSlashResumeSwitchesExplicitSession(t *testing.T) {
	runner := &scriptedRunner{
		sessionStates: map[string]SessionState{
			"s2": {
				ID:    "s2",
				Model: "fake/two",
				Messages: []InitialMessage{
					{Role: userRole, Text: "restored prompt"},
				},
			},
		},
	}
	model := NewModel(Options{Runner: runner, Model: "fake/one"})
	model = sendText(t, model, "/resume s2")

	updated, cmd := model.Update(keyPress(tea.KeyEnter))
	if cmd != nil {
		t.Fatalf("slash resume id returned unexpected command")
	}
	model = assertModel(t, updated)
	if runner.currentSessionID != "s2" {
		t.Fatalf("current session = %q, want s2", runner.currentSessionID)
	}
	if got := model.MessageTexts(); !reflect.DeepEqual(got, []string{"restored prompt", "session set to s2"}) {
		t.Fatalf("messages = %#v", got)
	}
}

func TestSlashResumeDoesNotSearchSessions(t *testing.T) {
	runner := &scriptedRunner{}
	model := NewModel(Options{Runner: runner})
	model = sendText(t, model, "/resume search test")

	updated, cmd := model.Update(keyPress(tea.KeyEnter))
	if cmd != nil {
		t.Fatalf("slash resume search returned unexpected command")
	}
	model = assertModel(t, updated)
	texts := model.MessageTexts()
	if len(texts) != 1 || !strings.Contains(texts[0], "usage: /resume [session-id]") {
		t.Fatalf("messages = %#v, want resume usage", texts)
	}
	if strings.Contains(strings.Join(texts, "\n"), "searching sessions") {
		t.Fatalf("resume should not trigger session search: %#v", texts)
	}
}

func TestSelectSessionOnStartOpensPicker(t *testing.T) {
	runner := &scriptedRunner{
		sessions: []SessionInfo{
			{ID: "s1", Title: "First", Model: "fake/one"},
			{ID: "s2", Title: "Second", Model: "fake/two"},
		},
		sessionStates: map[string]SessionState{
			"s1": {
				ID:    "s1",
				Model: "fake/one",
				Turn:  2,
				Messages: []InitialMessage{
					{Role: userRole, Text: "restored prompt"},
				},
			},
		},
	}
	model := NewModel(Options{Runner: runner, Model: "fake/default", SelectSession: true})
	if !strings.Contains(viewString(model), "select session") || !strings.Contains(viewString(model), "s1") {
		t.Fatalf("startup session picker missing:\n%s", viewString(model))
	}

	updated, _ := model.Update(keyPress(tea.KeyEnter))
	model = assertModel(t, updated)
	if runner.currentSessionID != "s1" {
		t.Fatalf("current session = %q, want s1", runner.currentSessionID)
	}
	if got := model.MessageTexts(); !reflect.DeepEqual(got, []string{"restored prompt", "session set to s1"}) {
		t.Fatalf("messages = %#v", got)
	}
}

func TestBangShellRunsLocallyWithoutAgentPrompt(t *testing.T) {
	runner := &scriptedRunner{
		shellEvents: []Event{
			{Type: EventShellOutput, Content: "hello"},
			{Type: EventDone},
		},
	}
	model := NewModel(Options{Runner: runner})
	model = sendText(t, model, "!echo hello")

	updated, cmd := model.Update(keyPress(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("bang shell returned nil command")
	}
	model = assertModel(t, updated)
	model = drainBatch(t, model, cmd)
	if runner.calls != 0 {
		t.Fatalf("agent calls = %d, want 0", runner.calls)
	}
	if runner.shellCalls != 1 || !reflect.DeepEqual(runner.shellCommands, []string{"echo hello"}) {
		t.Fatalf("shell calls/commands = %d/%#v, want one echo", runner.shellCalls, runner.shellCommands)
	}
	got := model.MessageTexts()
	if len(got) != 2 || got[0] != "!echo hello" || !strings.Contains(got[1], "hello") {
		t.Fatalf("messages = %#v, want local command and output", got)
	}
	if strings.Contains(viewString(model), "tool bash") || strings.Contains(viewString(model), "local_shell") {
		t.Fatalf("shell run rendered as tool activity:\n%s", viewString(model))
	}
}

func TestBangShellInputShowsShellModeHint(t *testing.T) {
	model := NewModel(Options{Runner: &scriptedRunner{}, Cwd: "/tmp/work"})
	model = sendText(t, model, "!")
	view := viewString(model)
	if !strings.Contains(view, "shell mode") || !strings.Contains(view, "enter runs locally") {
		t.Fatalf("shell hint missing:\n%s", view)
	}
	if !strings.Contains(view, "cwd /tmp/work") {
		t.Fatalf("shell hint missing cwd:\n%s", view)
	}
}

func TestBangShellWhileRunningStaysInInput(t *testing.T) {
	model := NewModel(Options{Runner: &scriptedRunner{}})
	model.running = true
	model = sendText(t, model, "!date")

	updated, cmd := model.Update(keyPress(tea.KeyEnter))
	model = assertModel(t, updated)
	if cmd != nil {
		t.Fatalf("running bang shell returned unexpected command")
	}
	if got := model.InputValue(); got != "!date" {
		t.Fatalf("input = %q, want command left in place", got)
	}
	if got := model.QueuedPrompts(); len(got) != 0 {
		t.Fatalf("queued prompts = %#v, want none for bang shell", got)
	}
}

func TestAtFilePickerInsertsMentionPath(t *testing.T) {
	runner := &scriptedRunner{
		workspaceFiles: []string{"internal/tui/model.go", "README.md"},
	}
	model := NewModel(Options{Runner: runner})
	model = sendText(t, model, "read @mod")
	if !strings.Contains(viewString(model), "attach file") || !strings.Contains(viewString(model), "internal/tui/model.go") {
		t.Fatalf("file picker missing:\n%s", viewString(model))
	}

	updated, cmd := model.Update(keyPress(tea.KeyTab))
	if cmd != nil {
		t.Fatalf("file picker tab returned unexpected command")
	}
	model = assertModel(t, updated)
	if got := model.InputValue(); got != "read @internal/tui/model.go " {
		t.Fatalf("input = %q, want inserted file mention", got)
	}
}

func TestAtFilePickerQuotesPathsWithSpaces(t *testing.T) {
	runner := &scriptedRunner{
		workspaceFiles: []string{"docs/my note.md"},
	}
	model := NewModel(Options{Runner: runner})
	model = sendText(t, model, "read @note")

	updated, _ := model.Update(keyPress(tea.KeyEnter))
	model = assertModel(t, updated)
	if got := model.InputValue(); got != `read @"docs/my note.md" ` {
		t.Fatalf("input = %q, want quoted file mention", got)
	}
}

func TestViewWrapsLongMessagesToWidth(t *testing.T) {
	model := NewModel(Options{})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 24, Height: 20})
	model = assertModel(t, updated)
	model = sendText(t, model, "abcdefghijklmnopqrstuvwxyz")
	updated, _ = model.Update(keyPress(tea.KeyEnter))
	model = assertModel(t, updated)

	view := viewString(model)
	if strings.Contains(view, "› abcdefghijklmnopqrstuvwxyz") {
		t.Fatalf("long line was not wrapped:\n%s", view)
	}
	if !strings.Contains(view, "[1]› abcdefghijklmno") || !strings.Contains(view, "     pqrstuvwxyz") {
		t.Fatalf("wrapped message missing expected fragments:\n%s", view)
	}
}

func TestTailViewMatchesFullRenderedBottom(t *testing.T) {
	var list messageList
	for i := 0; i < 20; i++ {
		list.append(userRole, fmt.Sprintf("prompt %02d", i))
		list.appendOrUpdateActivity(Event{
			Type:         EventActivity,
			ActivityKind: "tool",
			ToolUseID:    fmt.Sprintf("call_%02d", i),
			ToolName:     "read",
			Status:       "done",
			Summary:      fmt.Sprintf("path=file_%02d.go", i),
			Content:      fmt.Sprintf("detail %02d", i),
		})
		list.append(assistantRole, fmt.Sprintf("answer %02d\n\n- item", i))
	}

	const width = 80
	const height = 12
	full := list.render(width, tuitheme.Plain()).lines
	want := strings.Join(full[len(full)-height:], "\n")
	got := list.view(width, height, 0, tuitheme.Plain())
	if got != want {
		t.Fatalf("tail view mismatch\nwant:\n%s\n\ngot:\n%s", want, got)
	}
}

func TestExpandedLongToolDetailShowsViewportClipMarker(t *testing.T) {
	var list messageList
	list.appendOrUpdateActivity(Event{
		Type:         EventActivity,
		ActivityKind: "tool",
		ToolUseID:    "call_task",
		ToolName:     "task",
		Status:       "done",
		Summary:      "prompt=Research the provider design in the crush project (.references/crush/).",
		Content:      strings.Repeat("provider detail line\n", 80),
	})
	list.items[0].collapsed = false
	list.invalidateRender()

	view := list.view(100, 12, 0, tuitheme.Plain())
	lines := strings.Split(view, "\n")
	if !strings.Contains(lines[len(lines)-1], "more above") {
		t.Fatalf("expanded long tool detail should show viewport clipping marker:\n%s", view)
	}
	if strings.Contains(view, "activity detail truncated") {
		t.Fatalf("viewport clipping should not pretend the tool result was data-truncated:\n%s", view)
	}
}

func TestExpandedGroupedToolDetailShowsViewportClipMarker(t *testing.T) {
	var list messageList
	list.appendOrUpdateActivityInGroup("activity:tool:1", toolGroupName, Event{
		Type:         EventActivity,
		ActivityKind: "tool",
		ToolUseID:    "call_task",
		ToolName:     "task",
		Status:       "done",
		Summary:      "prompt=Research the provider design in the crush project (.references/crush/).",
		Content:      strings.Repeat("provider detail line\n", 80),
	})
	list.items[0].collapsed = false
	list.items[0].entries[0].collapsed = false
	list.invalidateRender()

	view := list.view(100, 12, 0, tuitheme.Plain())
	lines := strings.Split(view, "\n")
	if !strings.Contains(lines[len(lines)-1], "more above") {
		t.Fatalf("expanded grouped tool detail should show viewport clipping marker:\n%s", view)
	}
}

func TestExpandedThinkingDetailDoesNotShowToolClipMarker(t *testing.T) {
	var list messageList
	list.appendOrUpdateActivity(Event{
		Type:         EventActivity,
		ActivityKind: "thinking",
		Status:       "done",
		Summary:      "checking context",
		Content:      strings.Repeat("thinking detail line\n", 80),
	})
	list.items[0].collapsed = false
	list.invalidateRender()

	view := list.view(100, 12, 0, tuitheme.Plain())
	if strings.Contains(view, "tool detail clipped") {
		t.Fatalf("thinking detail should not show tool clipping marker:\n%s", view)
	}
}

func TestMouseExpandingGroupedToolDetailShowsBottomClipMarker(t *testing.T) {
	model := NewModel(Options{})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 14})
	model = assertModel(t, updated)
	model.messages.appendOrUpdateActivityInGroup("activity:tool:1", toolGroupName, Event{
		Type:         EventActivity,
		ActivityKind: "tool",
		ToolUseID:    "call_task",
		ToolName:     "task",
		Status:       "done",
		Summary:      "prompt=Research the provider design in the crush project (.references/crush/).",
		Content:      strings.Repeat("provider detail line\n", 80),
	})
	model.messages.items[0].collapsed = false
	model.messages.invalidateRender()

	updated, cmd := model.Update(mouseClick(0, 1))
	if cmd != nil {
		t.Fatalf("mouse click returned unexpected command")
	}
	model = assertModel(t, updated)
	view := viewString(model)
	lines := strings.Split(view, "\n")
	if !strings.Contains(lines[9], "more below") {
		t.Fatalf("mouse-expanded grouped tool detail should show bottom clipping marker:\n%s", view)
	}
}

func TestMouseExpandingLoadedTaskToolDetailShowsBottomClipMarker(t *testing.T) {
	model := NewModel(Options{Messages: []InitialMessage{
		{
			Turn:         1,
			ActivityKind: "tool",
			ToolUseID:    "call_task",
			ToolName:     "task",
			Status:       "queued",
			Summary:      "prompt=Research the provider design in the crush project (.references/crush/).",
		},
		{
			Turn:         1,
			ActivityKind: "tool",
			ToolUseID:    "call_task",
			ToolName:     "task",
			Status:       "done",
			Summary:      "prompt=Research the provider design in the crush project (.references/crush/).",
			Content:      strings.Repeat("provider detail line\n", 80),
		},
	}})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 14})
	model = assertModel(t, updated)

	updated, cmd := model.Update(mouseClick(0, 0))
	if cmd != nil {
		t.Fatalf("mouse click returned unexpected command")
	}
	model = assertModel(t, updated)
	view := viewString(model)
	lines := strings.Split(view, "\n")
	if !strings.Contains(lines[9], "more below") {
		t.Fatalf("mouse-expanded loaded task detail should show bottom clipping marker:\n%s", view)
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

	view := viewString(model)
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

	updated, cmd := model.Update(keyPress(tea.KeyEnter))
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

	updated, cmd := model.Update(keyPress('c', tea.ModCtrl))
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

	// First Esc should NOT interrupt; it should show a toast instead.
	updated, cmd := model.Update(keyPress(tea.KeyEsc))
	model = assertModel(t, updated)
	if cancelled {
		t.Fatalf("first esc should not cancel yet")
	}
	if !model.Running() {
		t.Fatalf("first esc should not stop running")
	}
	if model.toast.text == "" || model.toast.tone != toastNotice {
		t.Fatalf("first esc should show notice toast, got tone=%q text=%q", model.toast.tone, model.toast.text)
	}
	model.lastEscTime = time.Now().Add(-time.Second)

	// Second Esc while the hint is still visible should interrupt.
	updated, cmd = model.Update(keyPress(tea.KeyEsc))
	if cmd != nil {
		t.Fatalf("second esc returned unexpected command")
	}
	model = assertModel(t, updated)
	if !cancelled || model.Running() || !strings.Contains(viewString(model), "state: idle") {
		t.Fatalf("second esc did not interrupt: cancelled=%v running=%v view=\n%s", cancelled, model.Running(), viewString(model))
	}
	if model.runID != 4 {
		t.Fatalf("runID = %d, want 4", model.runID)
	}
}

func TestEscRepeatDoesNotConfirmInterrupt(t *testing.T) {
	cancelled := false
	model := NewModel(Options{})
	model.running = true
	model.status.state = statusStreaming
	model.runID = 3
	model.cancel = func() { cancelled = true }

	updated, _ := model.Update(keyPress(tea.KeyEsc))
	model = assertModel(t, updated)
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc, IsRepeat: true}))
	model = assertModel(t, updated)
	if cancelled || !model.Running() {
		t.Fatalf("repeat esc should not interrupt: cancelled=%v running=%v", cancelled, model.Running())
	}

	updated, _ = model.Update(keyPress(tea.KeyEsc))
	model = assertModel(t, updated)
	if !cancelled || model.Running() {
		t.Fatalf("explicit second esc should interrupt: cancelled=%v running=%v", cancelled, model.Running())
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

	// First Esc should show a toast, not interrupt.
	updated, cmd := model.Update(keyPress(tea.KeyEsc))
	model = assertModel(t, updated)
	if model.pending == nil || !model.Running() {
		t.Fatalf("first esc should not interrupt yet: pending=%v running=%v", model.pending == nil, model.Running())
	}

	// Second Esc within the double-tap window should deny and interrupt.
	updated, cmd = model.Update(keyPress(tea.KeyEsc))
	if cmd == nil {
		t.Fatalf("second esc returned nil command")
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
	if !strings.Contains(viewString(model), "state: idle") {
		t.Fatalf("view missing idle state:\n%s", viewString(model))
	}
}

type scriptedRunner struct {
	events                 []Event
	compactEvents          []Event
	calls                  int
	compactCalls           int
	prompts                []string
	provider               string
	providers              []string
	providerModels         map[string][]string
	providerApprovalModels map[string][]string
	providerSmallModels    map[string][]string
	model                  string
	models                 []string
	refreshModels          []string
	refreshModelErr        error
	refreshModelCalls      int
	effort                 string
	efforts                []string
	approvalModel          string
	approvalModels         []string
	refreshApprovalModels  []string
	refreshApprovalErr     error
	refreshApprovalCalls   int
	smallModel             string
	smallModels            []string
	refreshSmallModels     []string
	refreshSmallErr        error
	refreshSmallCalls      int
	mode                   string
	sessions               []SessionInfo
	sessionStates          map[string]SessionState
	currentSessionID       string
	newSessionState        SessionState
	newSessionCalls        int
	shellEvents            []Event
	shellCalls             int
	shellCommands          []string
	workspaceFiles         []string
	doctorReport           string
	doctorErr              error
	doctorCalls            int
	sideEvents             []Event
	sideEventScripts       [][]Event
	sideCalls              int
	sideQuestions          []string
	sideRequests           []SideQuestionRequest
	rewindTargets          []RewindTarget
	rewindRequests         []RewindRequest
	rewindState            SessionState
	rewindResult           RewindResult
	rewindErr              error
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

func (r *scriptedRunner) Doctor(context.Context) (string, error) {
	r.doctorCalls++
	return r.doctorReport, r.doctorErr
}

func (r *scriptedRunner) AnswerSideQuestion(_ context.Context, req SideQuestionRequest, events chan<- Event) error {
	callIndex := r.sideCalls
	r.sideCalls++
	r.sideQuestions = append(r.sideQuestions, req.Question)
	r.sideRequests = append(r.sideRequests, req)
	script := r.sideEvents
	if len(r.sideEventScripts) > 0 {
		if callIndex < len(r.sideEventScripts) {
			script = r.sideEventScripts[callIndex]
		} else {
			script = r.sideEventScripts[len(r.sideEventScripts)-1]
		}
	}
	for _, event := range script {
		events <- event
	}
	return nil
}

func (r *scriptedRunner) RunShell(_ context.Context, command string, events chan<- Event) error {
	r.shellCalls++
	r.shellCommands = append(r.shellCommands, command)
	if len(r.shellEvents) == 0 {
		events <- Event{Type: EventShellOutput, Content: "(no output)"}
		events <- Event{Type: EventDone}
		return nil
	}
	for _, event := range r.shellEvents {
		events <- event
	}
	return nil
}

func (r *scriptedRunner) ListWorkspaceFiles(_ context.Context, query string, limit int) ([]string, error) {
	query = strings.ToLower(strings.TrimSpace(query))
	var out []string
	for _, path := range r.workspaceFiles {
		if query != "" && !strings.Contains(strings.ToLower(path), query) {
			continue
		}
		out = append(out, path)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (r *scriptedRunner) SetModel(model string) error {
	if len(r.models) > 0 && !modelAllowed(r.models, model) {
		return fmt.Errorf("model %q is not available", model)
	}
	r.model = model
	return nil
}

func (r *scriptedRunner) SetProvider(providerName, model string) (ProviderSelection, error) {
	if len(r.providers) > 0 && !modelAllowed(r.providers, providerName) {
		return ProviderSelection{}, fmt.Errorf("provider %q is not available", providerName)
	}
	r.provider = providerName
	models := append([]string(nil), r.providerModels[providerName]...)
	if model == "" && len(models) > 0 {
		model = models[0]
	}
	if model != "" {
		models = normalizeModels(models, model)
	}
	r.model = model
	r.models = models
	if candidates, ok := r.providerApprovalModels[providerName]; ok {
		r.approvalModels = append([]string(nil), candidates...)
		r.approvalModel = ""
		if len(r.approvalModels) > 0 {
			r.approvalModel = r.approvalModels[0]
		}
	}
	if candidates, ok := r.providerSmallModels[providerName]; ok {
		r.smallModels = append([]string(nil), candidates...)
		r.smallModel = ""
		if len(r.smallModels) > 0 {
			r.smallModel = r.smallModels[0]
		}
	}
	return ProviderSelection{
		Provider:  r.provider,
		Providers: r.Providers(),
		Model:     r.model,
		Models:    r.Models(),
		Effort:    r.Effort(),
		Efforts:   r.Efforts(),
	}, nil
}

func (r *scriptedRunner) Provider() string {
	return r.provider
}

func (r *scriptedRunner) Providers() []string {
	return append([]string(nil), r.providers...)
}

func (r *scriptedRunner) SetMode(mode string) error {
	r.mode = mode
	return nil
}

func (r *scriptedRunner) Models() []string {
	return append([]string(nil), r.models...)
}

func (r *scriptedRunner) RefreshModels(context.Context) ([]string, error) {
	r.refreshModelCalls++
	if r.refreshModelErr != nil {
		return nil, r.refreshModelErr
	}
	if r.refreshModels != nil {
		r.models = append([]string(nil), r.refreshModels...)
	}
	return r.Models(), nil
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
	if len(r.approvalModels) > 0 && !modelAllowed(r.approvalModels, model) {
		return fmt.Errorf("approval model %q is not available", model)
	}
	r.approvalModel = model
	return nil
}

func (r *scriptedRunner) ApprovalModel() string {
	return r.approvalModel
}

func (r *scriptedRunner) ApprovalModels() []string {
	return append([]string(nil), r.approvalModels...)
}

func (r *scriptedRunner) RefreshApprovalModels(context.Context) ([]string, error) {
	r.refreshApprovalCalls++
	if r.refreshApprovalErr != nil {
		return nil, r.refreshApprovalErr
	}
	if r.refreshApprovalModels != nil {
		r.approvalModels = append([]string(nil), r.refreshApprovalModels...)
	}
	return r.ApprovalModels(), nil
}

func (r *scriptedRunner) SetSmallModel(model string) error {
	if len(r.smallModels) > 0 && !modelAllowed(r.smallModels, model) {
		return fmt.Errorf("small model %q is not available", model)
	}
	r.smallModel = model
	return nil
}

func (r *scriptedRunner) SmallModel() string {
	return r.smallModel
}

func (r *scriptedRunner) SmallModels() []string {
	return append([]string(nil), r.smallModels...)
}

func (r *scriptedRunner) RefreshSmallModels(context.Context) ([]string, error) {
	r.refreshSmallCalls++
	if r.refreshSmallErr != nil {
		return nil, r.refreshSmallErr
	}
	if r.refreshSmallModels != nil {
		r.smallModels = append([]string(nil), r.refreshSmallModels...)
	}
	return r.SmallModels(), nil
}

func (r *scriptedRunner) ListSessions(context.Context) ([]SessionInfo, error) {
	return append([]SessionInfo(nil), r.sessions...), nil
}

func (r *scriptedRunner) NewSession(context.Context) (SessionState, error) {
	r.newSessionCalls++
	state := r.newSessionState
	if state.ID == "" {
		state.ID = "new-session"
	}
	r.currentSessionID = state.ID
	if state.Provider != "" {
		r.provider = state.Provider
	}
	if state.Model != "" {
		r.model = state.Model
	}
	return state, nil
}

func (r *scriptedRunner) SwitchSession(_ context.Context, id string) (SessionState, error) {
	state := r.sessionStates[id]
	r.currentSessionID = id
	if state.Provider != "" {
		r.provider = state.Provider
	}
	if state.Model != "" {
		r.model = state.Model
	}
	return state, nil
}

func (r *scriptedRunner) CurrentSessionID() string {
	return r.currentSessionID
}

func (r *scriptedRunner) SearchSessions(_ context.Context, query string, limit int) (string, error) {
	return fmt.Sprintf("search results for %q (limit %d)", query, limit), nil
}

func (r *scriptedRunner) ListRewindTargets(context.Context) ([]RewindTarget, error) {
	return append([]RewindTarget(nil), r.rewindTargets...), nil
}

func (r *scriptedRunner) Rewind(_ context.Context, req RewindRequest) (SessionState, RewindResult, error) {
	r.rewindRequests = append(r.rewindRequests, req)
	if r.rewindErr != nil {
		return SessionState{}, RewindResult{}, r.rewindErr
	}
	state := r.rewindState
	result := r.rewindResult
	if result.Target.Turn == 0 {
		for _, target := range r.rewindTargets {
			if target.Turn == req.Turn {
				result.Target = target
				break
			}
		}
	}
	return state, result, nil
}

type promptOnlyRunner struct {
	calls int
}

func (r *promptOnlyRunner) Run(_ context.Context, _ string, _ chan<- Event) error {
	r.calls++
	return nil
}

type recordingClipboard struct {
	text  string
	err   error
	calls int
}

func (c *recordingClipboard) WriteText(_ context.Context, stringValue string) error {
	c.calls++
	c.text = stringValue
	return c.err
}

// pickStreamMsg unwraps a tea.BatchMsg by evaluating only the head sub-cmd.
// waitForEventFromUpdate puts the next-stream cmd first when it also batches a
// toast tick, so this avoids blocking on tea.Tick(toastTTL).
func pickStreamMsg(msg tea.Msg) tea.Msg {
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return msg
	}
	if len(batch) == 0 {
		return nil
	}
	return batch[0]()
}

func drainBatch(t *testing.T, model Model, cmd tea.Cmd) Model {
	t.Helper()
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("cmd returned %T, want tea.BatchMsg", cmd())
	}
	if len(batch) != 3 {
		t.Fatalf("batch len = %d, want 3", len(batch))
	}
	_ = batch[0]()
	msg := batch[1]()
	// batch[2] is the spinner tick (tea.Tick) — skip; calling it would block 80ms
	for steps := 0; steps < 32; steps++ {
		msg = pickStreamMsg(msg)
		if msg == nil {
			return model
		}
		updated, next := model.Update(msg)
		model = assertModel(t, updated)
		if next == nil {
			return model
		}
		msg = next()
	}
	t.Fatalf("drainBatch did not settle after 32 stream messages; last message %T", msg)
	return model
}

func drainSideQuestion(t *testing.T, model Model, cmd tea.Cmd) Model {
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
	for steps := 0; steps < 32; steps++ {
		if msg == nil {
			return model
		}
		updated, next := model.Update(msg)
		model = assertModel(t, updated)
		if next == nil {
			return model
		}
		msg = next()
	}
	t.Fatalf("drainSideQuestion did not settle after 32 stream messages; last message %T", msg)
	return model
}

func sendText(t *testing.T, model Model, text string) Model {
	t.Helper()
	for _, r := range text {
		updated, _ := model.Update(runePress(r))
		model = assertModel(t, updated)
	}
	return model
}

func viewString(model Model) string {
	return xansi.Strip(model.View().Content)
}

func writePlanFile(t *testing.T, path, title, status string, steps []string) {
	t.Helper()
	var b strings.Builder
	b.WriteString("# " + title + "\n\n")
	b.WriteString("Created: 2026-06-08T12:00:00Z\n")
	b.WriteString("Status: " + status + "\n\n")
	b.WriteString("## Steps\n\n")
	for i, step := range steps {
		fmt.Fprintf(&b, "- [ ] %d. %s\n", i+1, step)
	}
	b.WriteString("\n## Notes\n\n\n## Log\n\n")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir plan dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write plan: %v", err)
	}
}

func assertCursorOnInputLine(t *testing.T, model Model, marker string) {
	t.Helper()
	view := model.View()
	inputLine := lineContaining(strings.Split(view.Content, "\n"), marker)
	if inputLine < 0 {
		t.Fatalf("input marker %q missing:\n%s", marker, view.Content)
	}
	if view.Cursor == nil || view.Cursor.Y != inputLine {
		t.Fatalf("cursor = %+v, want Y %d\n%s", view.Cursor, inputLine, view.Content)
	}
}

func assertCursorOnBTWLine(t *testing.T, model Model) {
	t.Helper()
	view := model.View()
	inputLine := lineContaining(strings.Split(view.Content, "\n"), "BTW>")
	if inputLine < 0 {
		t.Fatalf("BTW input line missing:\n%s", view.Content)
	}
	if view.Cursor == nil || view.Cursor.Y != inputLine {
		t.Fatalf("cursor = %+v, want BTW input Y %d\n%s", view.Cursor, inputLine, view.Content)
	}
}

func assertBTWStatusLine(t *testing.T, model Model, state string) {
	t.Helper()
	lines := strings.Split(viewString(model), "\n")
	if len(lines) == 0 {
		t.Fatalf("BTW view is empty")
	}
	status := lines[len(lines)-1]
	for _, want := range []string{"view: btw", "state: " + state, "esc: return & clear"} {
		if !strings.Contains(status, want) {
			t.Fatalf("BTW status missing %q:\n%s", want, strings.Join(lines, "\n"))
		}
	}
	for _, unexpected := range []string{"cwd:", "turn:", "?"} {
		if strings.Contains(status, unexpected) {
			t.Fatalf("BTW status should not show main status segment %q:\n%s", unexpected, strings.Join(lines, "\n"))
		}
	}
}

func singleSideQuestionCacheKey(t *testing.T, model Model) string {
	t.Helper()
	if len(model.btw.bodyCache) != 1 {
		t.Fatalf("BTW body cache len = %d, want 1: %#v", len(model.btw.bodyCache), model.btw.bodyCache)
	}
	for key := range model.btw.bodyCache {
		return key
	}
	t.Fatalf("unreachable empty BTW body cache")
	return ""
}

func lineContaining(lines []string, needle string) int {
	for i, line := range lines {
		if strings.Contains(xansi.Strip(line), needle) {
			return i
		}
	}
	return -1
}

func keyPress(code rune, mods ...tea.KeyMod) tea.KeyPressMsg {
	var mod tea.KeyMod
	for _, next := range mods {
		mod |= next
	}
	return tea.KeyPressMsg(tea.Key{Code: code, Mod: mod})
}

func runePress(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: r, Text: string(r)})
}

func TestLimitPromptYesSendsExtension(t *testing.T) {
	resp := make(chan agent.LimitExtensionResponse, 1)
	model := NewModel(Options{})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	model = assertModel(t, updated)
	updated, _ = model.Update(limitRequestMsg{
		request: LimitRequest{
			Request:  agent.LimitExtensionRequest{UsedTurns: 50},
			Response: resp,
		},
		ok: true,
	})
	model = assertModel(t, updated)
	if model.pendingLimit == nil {
		t.Fatalf("pendingLimit should be set after limitRequestMsg")
	}
	if !strings.Contains(viewString(model), "reached the tool-loop cap") {
		t.Fatalf("limit prompt not visible:\n%s", viewString(model))
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	model = assertModel(t, updated)
	if model.pendingLimit != nil {
		t.Fatalf("pendingLimit should clear after decision")
	}
	select {
	case got := <-resp:
		if got.ExtraTurns != defaultLimitExtension {
			t.Fatalf("ExtraTurns = %d, want %d", got.ExtraTurns, defaultLimitExtension)
		}
	default:
		t.Fatalf("y did not send response on channel")
	}
}

func TestLimitPromptNoFallsThroughToFinalize(t *testing.T) {
	resp := make(chan agent.LimitExtensionResponse, 1)
	model := NewModel(Options{})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	model = assertModel(t, updated)
	updated, _ = model.Update(limitRequestMsg{
		request: LimitRequest{Response: resp},
		ok:      true,
	})
	model = assertModel(t, updated)
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	model = assertModel(t, updated)
	select {
	case got := <-resp:
		if got.ExtraTurns != 0 {
			t.Fatalf("ExtraTurns = %d, want 0 (declined)", got.ExtraTurns)
		}
	default:
		t.Fatalf("n did not send response on channel")
	}
}

func TestStructuredAskPromptSubmitsSelectionAndTranscriptSummary(t *testing.T) {
	model := NewModel(Options{})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	model = assertModel(t, updated)
	response := make(chan agent.AskResponse, 1)
	updated, cmd := model.Update(askRequestMsg{request: AskRequest{
		Request: agent.AskRequest{Questions: []agent.AskQuestion{{
			Header:   "Backend",
			Question: "Which store should ub use?",
			Options: []agent.AskOption{
				{Label: "SQLite", Description: "local durable store"},
				{Label: "Postgres", Description: "shared server"},
			},
		}}},
		Response: response,
	}, ok: true})
	model = assertModel(t, updated)
	if cmd != nil {
		t.Fatalf("ask request returned unexpected command")
	}
	view := viewString(model)
	if !strings.Contains(view, "Agent question") || !strings.Contains(view, "SQLite") {
		t.Fatalf("ask prompt not visible:\n%s", view)
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: '2', Text: "2"})
	model = assertModel(t, updated)
	updated, cmd = model.Update(keyPress(tea.KeyEnter))
	model = assertModel(t, updated)
	if cmd != nil {
		t.Fatalf("ask submit returned unexpected command")
	}
	if model.pendingAsk != nil {
		t.Fatal("pendingAsk should clear after submit")
	}
	select {
	case got := <-response:
		if got.Skipped || len(got.Answers) != 1 || len(got.Answers[0].Selected) != 1 || got.Answers[0].Selected[0].Label != "Postgres" {
			t.Fatalf("ask response = %#v, want Postgres", got)
		}
	default:
		t.Fatal("no ask response returned")
	}
	if got := model.MessageTexts(); len(got) == 0 || !strings.Contains(got[len(got)-1], "ask answered: Backend: Postgres") {
		t.Fatalf("messages = %#v, want ask summary", got)
	}
}

func mouseClick(x, y int) tea.MouseClickMsg {
	return tea.MouseClickMsg(tea.Mouse{X: x, Y: y, Button: tea.MouseLeft})
}

func mouseRelease(x, y int) tea.MouseReleaseMsg {
	return tea.MouseReleaseMsg(tea.Mouse{X: x, Y: y, Button: tea.MouseLeft})
}

func mouseWheel(button tea.MouseButton) tea.MouseWheelMsg {
	return tea.MouseWheelMsg(tea.Mouse{Button: button})
}

func assertModel(t *testing.T, model tea.Model) Model {
	t.Helper()
	m, ok := model.(Model)
	if !ok {
		t.Fatalf("model = %T, want tui.Model", model)
	}
	return m
}

func assertInitRequestsWindowSizes(t *testing.T, model Model, wantWidth, wantHeight int) {
	t.Helper()
	cmd := model.Init()
	if cmd == nil {
		t.Fatalf("Init returned nil")
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("Init returned %T, want tea.BatchMsg", cmd())
	}
	if len(batch) != 2 {
		t.Fatalf("Init batch len = %d, want 2", len(batch))
	}
	var gotSynthetic, gotRequest bool
	for _, cmd := range batch {
		msg := cmd()
		switch msg := msg.(type) {
		case tea.WindowSizeMsg:
			gotSynthetic = true
			if msg.Width != wantWidth || msg.Height != wantHeight {
				t.Fatalf("synthetic WindowSizeMsg = %dx%d, want %dx%d", msg.Width, msg.Height, wantWidth, wantHeight)
			}
		default:
			// tea.RequestWindowSize() returns an unexported tea.windowSizeMsg
			// (lowercase) that the runtime intercepts to fire a real size query.
			// We can't type-assert on it, so we match by name. Brittle to
			// upstream renames — revisit if Bubble Tea exports a public marker.
			if fmt.Sprintf("%T", msg) == "tea.windowSizeMsg" {
				gotRequest = true
			}
		}
	}
	if !gotSynthetic || !gotRequest {
		t.Fatalf("Init batch synthetic=%v request=%v, want both", gotSynthetic, gotRequest)
	}
}

func initModelRefreshMsg(t *testing.T, model Model) modelRefreshResultMsg {
	t.Helper()
	cmd := model.Init()
	if cmd == nil {
		t.Fatalf("Init returned nil")
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("Init returned %T, want tea.BatchMsg", cmd())
	}
	for _, cmd := range batch {
		msg := cmd()
		if refresh, ok := msg.(modelRefreshResultMsg); ok {
			return refresh
		}
	}
	t.Fatalf("Init batch did not include model refresh command")
	return modelRefreshResultMsg{}
}
