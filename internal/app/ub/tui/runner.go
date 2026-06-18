package tui

import (
	"context"
	"fmt"
	"runtime/debug"
	"time"

	tea "charm.land/bubbletea/v2"
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

// InjectRunner optionally lets the TUI inject user guidance text into the
// currently running agent loop without starting a new turn. Inject reports
// whether the guidance was actually delivered; callers should only reflect it
// in the UI when it returns true.
type InjectRunner interface {
	Inject(text string) bool
}

// DoctorRunner optionally lets slash commands run a local health check.
type DoctorRunner interface {
	Doctor(ctx context.Context) (string, error)
}

// SideQuestionMessage is one completed in-memory BTW exchange.
type SideQuestionMessage struct {
	Question string
	Answer   string
}

// SideQuestionRequest is one BTW side-chat turn. History contains only the BTW
// panel's in-memory thread, not the main transcript.
type SideQuestionRequest struct {
	Question string
	History  []SideQuestionMessage
}

// SideQuestionRunner optionally lets the TUI answer no-tool side-chat turns
// without recording them in the main conversation history.
type SideQuestionRunner interface {
	AnswerSideQuestion(ctx context.Context, req SideQuestionRequest, events chan<- Event) error
}

// ControlRunner optionally lets slash commands update future runs.
type ControlRunner interface {
	SetModel(model string) error
	SetMode(mode string) error
	Models() []string
}

// PlanModeControlRunner optionally lets slash commands and model-requested
// plan-mode tools share one in-memory transition path.
type PlanModeControlRunner interface {
	EnterPlanMode() (from, to string, err error)
	ExitPlanMode() (from, to string, err error)
}

// ModelRefreshRunner optionally lets the TUI load the current provider's
// complete model list on demand.
type ModelRefreshRunner interface {
	RefreshModels(ctx context.Context) ([]string, error)
}

// ProviderSelection is the effective provider/model state after switching.
type ProviderSelection struct {
	Provider  string
	Providers []string
	Model     string
	Models    []string
	Effort    string
	Efforts   []string
}

// ProviderControlRunner optionally lets slash commands update the chat provider.
type ProviderControlRunner interface {
	SetProvider(provider, model string) (ProviderSelection, error)
	Provider() string
	Providers() []string
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

// ApprovalModelRefreshRunner optionally lets the TUI load the approval
// provider's complete model list on demand.
type ApprovalModelRefreshRunner interface {
	RefreshApprovalModels(ctx context.Context) ([]string, error)
}

// SmallModelControlRunner optionally lets slash commands update the small
// model used by auto memory.
type SmallModelControlRunner interface {
	SetSmallModel(model string) error
	SmallModel() string
	SmallModels() []string
}

// SmallModelRefreshRunner optionally lets the TUI load the current provider's
// complete small-model candidate list on demand.
type SmallModelRefreshRunner interface {
	RefreshSmallModels(ctx context.Context) ([]string, error)
}

// InitialMessage is a persisted message rendered when a TUI session is loaded.
type InitialMessage struct {
	Role string
	// Turn is the agent loop turn this message belongs to. Used during resume
	// to namespace activity groups so tools/thinking from different turns do
	// not collapse into a single block.
	Turn            int
	Text            string
	ToolUseID       string
	ToolName        string
	ParentToolUseID string
	SubagentID      string
	Content         string
	ActivityKind    string
	Status          string
	Summary         string
	Decision        string
	Source          string
	Reason          string
	Allowed         bool
	IsError         bool
}

// SessionInfo is one selectable persisted session.
type SessionInfo struct {
	ID        string
	Title     string
	Provider  string
	Model     string
	UpdatedAt time.Time
	Current   bool
}

// SessionState is the restored state for a selected session.
type SessionState struct {
	ID        string
	Provider  string
	Providers []string
	Model     string
	Models    []string
	Effort    string
	Efforts   []string
	Turn      int
	Messages  []InitialMessage
}

// SessionRunner optionally lets slash commands list and switch persisted sessions.
type SessionRunner interface {
	ListSessions(ctx context.Context) ([]SessionInfo, error)
	NewSession(ctx context.Context) (SessionState, error)
	SwitchSession(ctx context.Context, id string) (SessionState, error)
	CurrentSessionID() string
}

// SessionSearchRunner optionally lets slash commands search across session
// rollout content.
type SessionSearchRunner interface {
	SearchSessions(ctx context.Context, query string, limit int) (string, error)
}

// RewindFileChange summarizes one checkpointed workspace file that would
// change when the target rewind is applied.
type RewindFileChange struct {
	Path string
	Kind string
}

// RewindTarget is one historical user turn the TUI can rewind to. Rewinding
// target turn N removes turn N and later from the session, then restores Text
// into the input box so the user can edit and resubmit it.
type RewindTarget struct {
	Turn          int
	Text          string
	Time          time.Time
	AffectedFiles []RewindFileChange
}

// RewindRequest is one explicit rewind action.
type RewindRequest struct {
	Turn        int
	RevertFiles bool
}

// RewindResult describes what happened while applying a rewind.
type RewindResult struct {
	Target        RewindTarget
	DeletedEvents int
	RevertedFiles []string
	SkippedFiles  []string
}

// RewindRunner optionally lets slash commands rewind the current session to
// before a selected historical user turn.
type RewindRunner interface {
	ListRewindTargets(ctx context.Context) ([]RewindTarget, error)
	Rewind(ctx context.Context, req RewindRequest) (SessionState, RewindResult, error)
}

// EventType identifies a TUI stream event.
type EventType string

const (
	EventDeltaText         EventType = "delta_text"
	EventActivity          EventType = "activity"
	EventContext           EventType = "context"
	EventToolPartialOutput EventType = "tool_partial_output"
	EventToolCallStart     EventType = "tool_call_start"
	EventToolCallEnd       EventType = "tool_call_end"
	EventPermission        EventType = "permission"
	EventShellOutput       EventType = "shell_output"
	EventDone              EventType = "done"
	EventError             EventType = "error"
)

// Event is one Agent-to-TUI progress message.
type Event struct {
	Type            EventType
	Text            string
	ToolUseID       string
	ToolName        string
	ParentToolUseID string
	SubagentID      string
	Content         string
	ActivityKind    string
	Status          string
	Summary         string
	Notice          string
	Decision        string
	Source          string
	Reason          string
	Allowed         bool
	IsError         bool
	Err             error

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

type sideQuestionEventMsg struct {
	event Event
	ok    bool
	runID int
}

type backgroundEventMsg struct {
	event Event
	ok    bool
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

func waitForSideQuestionEvent(events <-chan Event, runID int) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-events
		return sideQuestionEventMsg{event: event, ok: ok, runID: runID}
	}
}

func waitForBackgroundEvent(events <-chan Event) tea.Cmd {
	if events == nil {
		return nil
	}
	return func() tea.Msg {
		event, ok := <-events
		return backgroundEventMsg{event: event, ok: ok}
	}
}

func runPrompt(ctx context.Context, runner Runner, prompt string, events chan<- Event) tea.Cmd {
	return func() tea.Msg {
		defer close(events)
		defer recoverRunnerPanic(events, "agent run")
		if err := runner.Run(ctx, prompt, events); err != nil {
			sendRunnerEvent(events, Event{Type: EventError, Err: err, Content: err.Error(), IsError: true})
		}
		return nil
	}
}

func runShell(ctx context.Context, runner ShellRunner, command string, events chan<- Event) tea.Cmd {
	return func() tea.Msg {
		defer close(events)
		defer recoverRunnerPanic(events, "shell run")
		if err := runner.RunShell(ctx, command, events); err != nil {
			sendRunnerEvent(events, Event{Type: EventError, Err: err, Content: err.Error(), IsError: true})
		}
		return nil
	}
}

func runSideQuestion(ctx context.Context, runner SideQuestionRunner, req SideQuestionRequest, events chan<- Event) tea.Cmd {
	return func() tea.Msg {
		defer close(events)
		defer recoverRunnerPanic(events, "btw run")
		if err := runner.AnswerSideQuestion(ctx, req, events); err != nil {
			sendRunnerEvent(events, Event{Type: EventError, Err: err, Content: err.Error(), IsError: true})
		}
		return nil
	}
}

func runCompact(ctx context.Context, runner CompactRunner, events chan<- Event) tea.Cmd {
	return func() tea.Msg {
		defer close(events)
		defer recoverRunnerPanic(events, "compact run")
		if err := runner.Compact(ctx, events); err != nil {
			sendRunnerEvent(events, Event{Type: EventError, Err: err, Content: err.Error(), IsError: true})
		}
		return nil
	}
}

func recoverRunnerPanic(events chan<- Event, label string) {
	if r := recover(); r != nil {
		err := fmt.Errorf("%s panic: %v", label, r)
		sendRunnerEvent(events, Event{
			Type:    EventError,
			Content: fmt.Sprintf("%v\n%s", err, debug.Stack()),
			Err:     err,
			IsError: true,
		})
	}
}

func sendRunnerEvent(events chan<- Event, event Event) {
	defer func() { _ = recover() }()
	events <- event
}
