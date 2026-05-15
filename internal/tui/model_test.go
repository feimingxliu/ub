package tui

import (
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
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
