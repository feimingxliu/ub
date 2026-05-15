// Package tui contains the Bubble Tea terminal interface for ub.
package tui

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/feimingxliu/ub/internal/execution"
	permissiondialog "github.com/feimingxliu/ub/internal/tui/dialog/permission"
	"github.com/feimingxliu/ub/internal/tui/slash"
)

// Options configures the initial TUI shell.
type Options struct {
	Input         io.Reader
	Output        io.Writer
	Context       context.Context
	Runner        Runner
	Permissions   <-chan PermissionRequest
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
	permReqs <-chan PermissionRequest
	pending  *PermissionRequest
	modal    permissiondialog.Model
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
		permReqs: opts.Permissions,
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
	return tea.Batch(textinput.Blink, waitForPermission(m.permReqs))
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.pending != nil {
		if key, ok := msg.(tea.KeyMsg); ok {
			switch key.String() {
			case "d":
				m.modal = m.modal.ToggleDiff()
				return m, nil
			default:
				if m.modal.HandleKey(key.String()) {
					return m, nil
				}
				if decision, ok := permissiondialog.DecisionForKey(key.String()); ok {
					m.pending.Response <- decision
					m.pending = nil
					return m, waitForPermission(m.permReqs)
				}
			}
		}
		return m, nil
	}

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
				if strings.HasPrefix(text, "/") {
					m.input.SetValue("")
					return m.executeSlash(text)
				}
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
	case permissionRequestMsg:
		if !msg.ok {
			return m, nil
		}
		m.pending = &msg.request
		m.modal = permissiondialog.New(msg.request.Request)
		return m, nil
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
	if m.pending != nil {
		b.WriteString("\n\n")
		b.WriteString(m.modal.View())
	}
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

func (m Model) executeSlash(input string) (tea.Model, tea.Cmd) {
	cmd, err := slash.Parse(input)
	if err != nil {
		m.messages.append(systemRole, err.Error())
		return m, nil
	}
	switch cmd.Name {
	case "clear":
		m.messages.clear()
		return m, nil
	case "help":
		m.messages.append(systemRole, "commands: /"+strings.Join(slash.Supported(), ", /"))
		return m, nil
	case "quit":
		if m.cancel != nil {
			m.cancel()
		}
		return m, tea.Quit
	case "config":
		m.messages.append(systemRole, fmt.Sprintf("model=%s mode=%s cwd=%s", m.status.model, m.status.executionMode, m.status.cwd))
		return m, nil
	case "sessions":
		m.messages.append(systemRole, "sessions: use `ub sessions ls` for the current workspace")
		return m, nil
	case "profile":
		if len(cmd.Args) == 0 {
			m.messages.append(systemRole, "profile: use `/profile <name>` to show restart guidance")
		} else {
			m.messages.append(systemRole, fmt.Sprintf("profile %q requires restart via `ub --profile %s` or UB_PROFILE=%s", cmd.Args[0], cmd.Args[0], cmd.Args[0]))
		}
		return m, nil
	case "model":
		if len(cmd.Args) == 0 {
			m.messages.append(systemRole, "model: "+m.status.model)
			return m, nil
		}
		model := strings.Join(cmd.Args, " ")
		m.status.model = model
		if runner, ok := m.runner.(ControlRunner); ok {
			runner.SetModel(model)
		}
		m.messages.append(systemRole, "model set to "+model)
		return m, nil
	case "mode":
		if len(cmd.Args) == 0 {
			m.messages.append(systemRole, "mode: "+m.status.executionMode)
			return m, nil
		}
		mode, err := execution.ParseMode(cmd.Args[0])
		if err != nil {
			m.messages.append(systemRole, err.Error())
			return m, nil
		}
		if runner, ok := m.runner.(ControlRunner); ok {
			if err := runner.SetMode(string(mode)); err != nil {
				m.messages.append(systemRole, err.Error())
				return m, nil
			}
		}
		m.status.executionMode = string(mode)
		m.messages.append(systemRole, "mode set to "+string(mode))
		return m, nil
	default:
		m.messages.append(systemRole, "unknown slash command "+cmd.Name)
		return m, nil
	}
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
