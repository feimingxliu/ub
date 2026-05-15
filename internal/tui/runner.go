package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Runner executes one user prompt and streams progress events to the TUI.
type Runner interface {
	Run(ctx context.Context, prompt string, events chan<- Event) error
}

// ControlRunner optionally lets slash commands update future runs.
type ControlRunner interface {
	SetModel(model string) error
	SetMode(mode string) error
	Models() []string
}

// InitialMessage is a persisted message rendered when a TUI session is loaded.
type InitialMessage struct {
	Role string
	Text string
}

// SessionInfo is one selectable persisted session.
type SessionInfo struct {
	ID        string
	Title     string
	Model     string
	UpdatedAt time.Time
	Current   bool
}

// SessionState is the restored state for a selected session.
type SessionState struct {
	ID       string
	Model    string
	Turn     int
	Messages []InitialMessage
}

// SessionRunner optionally lets slash commands list and switch persisted sessions.
type SessionRunner interface {
	ListSessions(ctx context.Context) ([]SessionInfo, error)
	SwitchSession(ctx context.Context, id string) (SessionState, error)
	CurrentSessionID() string
}

// EventType identifies a TUI stream event.
type EventType string

const (
	EventDeltaText     EventType = "delta_text"
	EventToolCallStart EventType = "tool_call_start"
	EventToolCallEnd   EventType = "tool_call_end"
	EventDone          EventType = "done"
	EventError         EventType = "error"
)

// Event is one Agent-to-TUI progress message.
type Event struct {
	Type     EventType
	Text     string
	ToolName string
	Content  string
	IsError  bool
	Err      error
}

type streamEventMsg struct {
	event Event
	ok    bool
}

func waitForEvent(events <-chan Event) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-events
		return streamEventMsg{event: event, ok: ok}
	}
}

func runPrompt(ctx context.Context, runner Runner, prompt string, events chan<- Event) tea.Cmd {
	return func() tea.Msg {
		defer close(events)
		if err := runner.Run(ctx, prompt, events); err != nil {
			events <- Event{Type: EventError, Err: err, Content: err.Error(), IsError: true}
		}
		return nil
	}
}
