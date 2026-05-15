package tui

import (
	"context"
	"fmt"
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

// ApprovalControlRunner optionally lets slash commands update the approval
// model used by auto mode.
type ApprovalControlRunner interface {
	SetApprovalModel(model string) error
	ApprovalModel() string
	ApprovalModels() []string
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
	EventPermission    EventType = "permission"
	EventDone          EventType = "done"
	EventError         EventType = "error"
)

// Event is one Agent-to-TUI progress message.
type Event struct {
	Type     EventType
	Text     string
	ToolName string
	Content  string
	Decision string
	Source   string
	Reason   string
	Allowed  bool
	IsError  bool
	Err      error
}

type streamEventMsg struct {
	event Event
	ok    bool
	runID int
}

func waitForEvent(events <-chan Event, runID int) tea.Cmd {
	return waitForEventWithTimeout(events, runID, 0)
}

func waitForEventWithTimeout(events <-chan Event, runID int, timeout time.Duration) tea.Cmd {
	return func() tea.Msg {
		if timeout <= 0 {
			event, ok := <-events
			return streamEventMsg{event: event, ok: ok, runID: runID}
		}
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		select {
		case event, ok := <-events:
			return streamEventMsg{event: event, ok: ok, runID: runID}
		case <-timer.C:
			err := fmt.Errorf("agent turn timed out after %s without progress", timeout)
			return streamEventMsg{
				event: Event{Type: EventError, Content: err.Error(), IsError: true, Err: err},
				ok:    true,
				runID: runID,
			}
		}
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
