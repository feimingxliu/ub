package tui

import (
	"context"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/feimingxliu/ub/internal/execution"
	"github.com/feimingxliu/ub/internal/permission"
	"github.com/feimingxliu/ub/internal/tool"
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
	for _, want := range []string{"You: hello", "model: fake/test", "mode: plan", "cwd: /work"} {
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
	if got, want := model.MessageTexts(), []string{"first", ""}; !reflect.DeepEqual(got, want) {
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

func TestModelPermissionRequestReturnsDecision(t *testing.T) {
	response := make(chan permission.Decision, 1)
	model := NewModel(Options{})
	req := PermissionRequest{
		Request: permission.Request{
			Tool: "bash",
			Risk: tool.RiskExec,
			Mode: execution.ModeDefault,
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

type scriptedRunner struct {
	events  []Event
	calls   int
	prompts []string
}

func (r *scriptedRunner) Run(_ context.Context, prompt string, events chan<- Event) error {
	r.calls++
	r.prompts = append(r.prompts, prompt)
	for _, event := range r.events {
		events <- event
	}
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
