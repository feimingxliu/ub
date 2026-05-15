// Package tui contains the Bubble Tea terminal interface for ub.
package tui

import (
	"context"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// Options configures the initial TUI shell.
type Options struct {
	Input         io.Reader
	Output        io.Writer
	Model         string
	ExecutionMode string
	Cwd           string
}

// Run starts the terminal UI and blocks until it exits.
func Run(ctx context.Context, opts Options) error {
	if ctx == nil {
		ctx = context.Background()
	}
	programOpts := []tea.ProgramOption{tea.WithContext(ctx)}
	if opts.Input != nil {
		programOpts = append(programOpts, tea.WithInput(opts.Input))
	}
	if opts.Output != nil {
		programOpts = append(programOpts, tea.WithOutput(opts.Output))
	}
	_, err := tea.NewProgram(NewModel(opts), programOpts...).Run()
	return err
}

// Model is the root Bubble Tea model for the chat shell.
type Model struct {
	input    textinput.Model
	messages messageList
	status   statusBar
	width    int
	height   int
}

// NewModel creates the root TUI model.
func NewModel(opts Options) Model {
	input := textinput.New()
	input.Placeholder = "Type a message"
	input.Prompt = "> "
	input.Focus()

	return Model{
		input:    input,
		messages: newMessageList(),
		status: statusBar{
			model:         defaultString(opts.Model, "unknown"),
			executionMode: defaultString(opts.ExecutionMode, "default"),
			cwd:           defaultString(opts.Cwd, "."),
		},
	}
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return textinput.Blink
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.status.width = msg.Width
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			return m, tea.Quit
		case "enter":
			if text := strings.TrimSpace(m.input.Value()); text != "" {
				m.messages.append(userRole, text)
				m.input.SetValue("")
			}
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// View implements tea.Model.
func (m Model) View() string {
	var b strings.Builder
	b.WriteString(m.messages.view())
	b.WriteString("\n\n")
	b.WriteString(m.input.View())
	b.WriteByte('\n')
	b.WriteString(m.status.view())
	return b.String()
}

// MessageTexts returns the rendered message text values for tests.
func (m Model) MessageTexts() []string {
	return m.messages.texts()
}

// InputValue returns the current input value for tests.
func (m Model) InputValue() string {
	return m.input.Value()
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
