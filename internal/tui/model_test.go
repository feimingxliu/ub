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
	for _, want := range []string{"> hello", "model: fake/test", "mode: plan", "cwd: /work"} {
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

func TestModelPermissionSelectionReturnsDecision(t *testing.T) {
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

func TestShiftTabCyclesMode(t *testing.T) {
	runner := &scriptedRunner{}
	model := NewModel(Options{Runner: runner, ExecutionMode: "default"})

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if cmd != nil {
		t.Fatalf("shift+tab returned unexpected command")
	}
	model = assertModel(t, updated)
	if runner.mode != "plan" || !strings.Contains(model.View(), "mode: plan") {
		t.Fatalf("first shift+tab failed: runner=%q view=\n%s", runner.mode, model.View())
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	model = assertModel(t, updated)
	if runner.mode != "agent-approve" || !strings.Contains(model.View(), "mode: agent-approve") {
		t.Fatalf("second shift+tab failed: runner=%q view=\n%s", runner.mode, model.View())
	}
}

func TestTabCompletesSlashCommand(t *testing.T) {
	runner := &scriptedRunner{}
	model := NewModel(Options{Runner: runner, ExecutionMode: "default"})
	model = sendText(t, model, "/m")

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyTab})
	if cmd != nil {
		t.Fatalf("tab returned unexpected command")
	}
	model = assertModel(t, updated)
	if got := model.InputValue(); got != "/model " {
		t.Fatalf("input = %q, want /model ", got)
	}
	if runner.mode != "" || !strings.Contains(model.View(), "mode: default") {
		t.Fatalf("tab unexpectedly changed mode: runner=%q view=\n%s", runner.mode, model.View())
	}
}

func TestArrowSelectsSlashSuggestion(t *testing.T) {
	model := NewModel(Options{})
	model = sendText(t, model, "/m")

	view := model.View()
	if !strings.Contains(view, "> /model [model]") || !strings.Contains(view, "  /mode <default|plan|agent-approve>") {
		t.Fatalf("initial slash selection missing:\n%s", view)
	}
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyDown})
	if cmd != nil {
		t.Fatalf("down returned unexpected command")
	}
	model = assertModel(t, updated)
	view = model.View()
	if !strings.Contains(view, "  /model [model]") || !strings.Contains(view, "> /mode <default|plan|agent-approve>") {
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
	if !strings.Contains(view, "  fake/new") || strings.Contains(view, "  fake/other") {
		t.Fatalf("view missing filtered model suggestions:\n%s", view)
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
	if strings.Contains(view, "> abcdefghijklmnopqrstuvwxyz") {
		t.Fatalf("long line was not wrapped:\n%s", view)
	}
	if !strings.Contains(view, "> abcdefghijklmnopqrstuv") || !strings.Contains(view, "  wxyz") {
		t.Fatalf("wrapped message missing expected fragments:\n%s", view)
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

type scriptedRunner struct {
	events           []Event
	calls            int
	prompts          []string
	model            string
	models           []string
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
