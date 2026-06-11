package agent

// EventType identifies an Agent runtime event.
type EventType string

const (
	EventDeltaText         EventType = "delta_text"
	EventActivity          EventType = "activity"
	EventContext           EventType = "context"
	EventToolCallStart     EventType = "tool_call_start"
	EventToolCallEnd       EventType = "tool_call_end"
	EventToolPartialOutput EventType = "tool_partial_output"
	EventPermission        EventType = "permission"
	EventDone              EventType = "done"
	EventError             EventType = "error"
)

// ActivityKind identifies a structured Agent activity event.
type ActivityKind string

const (
	ActivityThinking   ActivityKind = "thinking"
	ActivityTool       ActivityKind = "tool"
	ActivityPermission ActivityKind = "permission"
	ActivityAsk        ActivityKind = "ask"
	ActivityNotice     ActivityKind = "notice"
	ActivityHook       ActivityKind = "hook"
)

// Event reports Agent progress to interactive callers such as the TUI.
type Event struct {
	Type            EventType
	Text            string
	ToolUseID       string
	ToolName        string
	ParentToolUseID string
	SubagentID      string
	Content         string
	ActivityKind    ActivityKind
	Status          string
	Summary         string
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

// EventSink receives Agent runtime events in emission order.
type EventSink func(Event)
