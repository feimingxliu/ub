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

// ShellRunner optionally lets the TUI run a local shell command directly.
type ShellRunner interface {
	RunShell(ctx context.Context, command string, events chan<- Event) error
}

// WorkspaceFileRunner optionally lets the TUI list workspace files for @ mentions.
type WorkspaceFileRunner interface {
	ListWorkspaceFiles(ctx context.Context, query string, limit int) ([]string, error)
}

// CompactRunner optionally lets slash commands compact the current session.
type CompactRunner interface {
	Compact(ctx context.Context, events chan<- Event) error
}

// ControlRunner optionally lets slash commands update future runs.
type ControlRunner interface {
	SetModel(model string) error
	SetMode(mode string) error
	Models() []string
}

// EffortControlRunner optionally lets slash commands update reasoning effort.
type EffortControlRunner interface {
	SetEffort(effort string) error
	Effort() string
	Efforts() []string
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
	NewSession(ctx context.Context) (SessionState, error)
	SwitchSession(ctx context.Context, id string) (SessionState, error)
	CurrentSessionID() string
}

// EventType identifies a TUI stream event.
type EventType string

const (
	EventDeltaText     EventType = "delta_text"
	EventActivity      EventType = "activity"
	EventContext       EventType = "context"
	EventToolCallStart EventType = "tool_call_start"
	EventToolCallEnd   EventType = "tool_call_end"
	EventPermission    EventType = "permission"
	EventShellOutput   EventType = "shell_output"
	EventDone          EventType = "done"
	EventError         EventType = "error"
)

// Event is one Agent-to-TUI progress message.
type Event struct {
	Type         EventType
	Text         string
	ToolUseID    string
	ToolName     string
	Content      string
	ActivityKind string
	Status       string
	Summary      string
	Decision     string
	Source       string
	Reason       string
	Allowed      bool
	IsError      bool
	Err          error

	ContextUsedTokens int
	ContextMaxTokens  int
	ContextRatio      float64
	ContextReset      bool
	ContextKind       string
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

func runShell(ctx context.Context, runner ShellRunner, command string, events chan<- Event) tea.Cmd {
	return func() tea.Msg {
		defer close(events)
		if err := runner.RunShell(ctx, command, events); err != nil {
			events <- Event{Type: EventError, Err: err, Content: err.Error(), IsError: true}
		}
		return nil
	}
}

func runCompact(ctx context.Context, runner CompactRunner, events chan<- Event) tea.Cmd {
	return func() tea.Msg {
		defer close(events)
		if err := runner.Compact(ctx, events); err != nil {
			events <- Event{Type: EventError, Err: err, Content: err.Error(), IsError: true}
		}
		return nil
	}
}
