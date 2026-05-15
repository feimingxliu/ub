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
	Context       context.Context
	Runner        Runner
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
	opts.Context = ctx
	_, err := tea.NewProgram(NewModel(opts), programOpts...).Run()
	return err
}

// Model is the root Bubble Tea model for the chat shell.
type Model struct {
	input    textinput.Model
	messages messageList
	status   statusBar
	runner   Runner
	ctx      context.Context
	cancel   context.CancelFunc
	running  bool
	events   <-chan Event
	width    int
	height   int
}

// NewModel creates the root TUI model.
func NewModel(opts Options) Model {
	input := textinput.New()
	input.Placeholder = "Type a message"
	input.Prompt = "> "
	input.Focus()
	ctx := opts.Context
	if ctx == nil {
		ctx = context.Background()
	}

	return Model{
		input:    input,
		messages: newMessageList(),
		runner:   opts.Runner,
		ctx:      ctx,
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
			if m.cancel != nil {
				m.cancel()
			}
			return m, tea.Quit
		case "enter":
			if m.running {
				return m, nil
			}
			if text := strings.TrimSpace(m.input.Value()); text != "" {
				m.messages.append(userRole, text)
				m.input.SetValue("")
				if m.runner == nil {
					return m, nil
				}
				m.messages.startAssistant()
				m.running = true
				m.status.running = true
				m.status.turn++
				ctx, cancel := context.WithCancel(m.ctx)
				m.cancel = cancel
				events := make(chan Event, 64)
				m.events = events
				return m, tea.Batch(runPrompt(ctx, m.runner, text, events), waitForEvent(events))
			}
			return m, nil
		}
	case streamEventMsg:
		if !msg.ok {
			m.running = false
			m.status.running = false
			m.cancel = nil
			return m, nil
		}
		cmd := waitForEventFromUpdate(msg.event, &m)
		return m, cmd
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

// Running reports whether an Agent turn is in progress.
func (m Model) Running() bool {
	return m.running
}

// Turn returns the current TUI turn number.
func (m Model) Turn() int {
	return m.status.turn
}

func waitForEventFromUpdate(event Event, m *Model) tea.Cmd {
	switch event.Type {
	case EventDeltaText:
		m.messages.appendAssistantDelta(event.Text)
		return waitForEvent(m.events)
	case EventToolCallStart:
		m.messages.appendToolStatus(event.ToolName, "started")
		return waitForEvent(m.events)
	case EventToolCallEnd:
		m.messages.appendToolStatus(event.ToolName, "finished")
		return waitForEvent(m.events)
	case EventDone:
		m.running = false
		m.status.running = false
		m.cancel = nil
		return nil
	case EventError:
		m.messages.append("Error", defaultString(event.Content, "agent failed"))
		m.running = false
		m.status.running = false
		m.cancel = nil
		return nil
	default:
		return waitForEvent(m.events)
	}
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
