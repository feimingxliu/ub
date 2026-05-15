package agent

// EventType identifies an Agent runtime event.
type EventType string

const (
	EventDeltaText     EventType = "delta_text"
	EventToolCallStart EventType = "tool_call_start"
	EventToolCallEnd   EventType = "tool_call_end"
	EventDone          EventType = "done"
	EventError         EventType = "error"
)

// Event reports Agent progress to interactive callers such as the TUI.
type Event struct {
	Type      EventType
	Text      string
	ToolUseID string
	ToolName  string
	Content   string
	IsError   bool
	Err       error
}

// EventSink receives Agent runtime events in emission order.
type EventSink func(Event)
