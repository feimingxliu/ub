// Package tool defines ub's local tool surface: a Tool interface, risk
// taxonomy, preview protocol and a Registry for assembling the set of
// tools an agent loop can call.
//
// This package only carries interfaces, value types and a plain map-backed
// Registry. It does not implement any concrete tool, does not dispatch
// calls, does not enforce mode or permission policy, and is NOT safe for
// concurrent registration: callers are expected to wire the Registry
// during process startup before any goroutine consumes it.
package tool

import (
	"context"
	"encoding/json"

	"github.com/invopop/jsonschema"
)

// Risk classifies a tool's side-effect profile. The agent dispatcher uses
// it to decide whether a call goes through plan-mode gating, permission
// approval, or executes directly.
type Risk string

const (
	// RiskSafe tools are read-only over local state (read, ls, grep, glob).
	RiskSafe Risk = "safe"
	// RiskWrite tools mutate workspace files (write, edit).
	RiskWrite Risk = "write"
	// RiskExec tools spawn external processes (bash, job_run).
	RiskExec Risk = "exec"
)

// Tool is the contract every local or MCP-backed tool implements.
//
// Schema returns the JSON Schema of the tool's input arguments. The
// returned value MUST marshal to valid JSON; agent loops forward it to
// providers as part of the request's tool definitions.
type Tool interface {
	Name() string
	Description() string
	Schema() *jsonschema.Schema
	Risk() Risk
	Execute(ctx context.Context, args json.RawMessage) (Result, error)
}

// PreviewableTool is an optional interface implemented by write-class
// tools. The dispatcher detects it via a type assertion (not via Risk)
// and calls Preview before Execute so the permission UI can render a
// diff/summary for the user to approve.
type PreviewableTool interface {
	Tool
	Preview(ctx context.Context, args json.RawMessage) (Preview, error)
}

// StreamEventKind classifies a streamed chunk.
type StreamEventKind string

const (
	// StreamStdout marks a chunk that came out of the tool's stdout-like
	// channel.
	StreamStdout StreamEventKind = "stdout"
	// StreamStderr marks an stderr-like channel chunk.
	StreamStderr StreamEventKind = "stderr"
	// StreamInfo marks an out-of-band tool progress note (start, kill
	// reason, etc.).
	StreamInfo StreamEventKind = "info"
)

// StreamEvent is one incremental output chunk from a streaming tool. The
// dispatcher forwards each event into the agent EventSink as an
// EventToolPartialOutput so the TUI can render running progress.
type StreamEvent struct {
	Kind StreamEventKind
	Data string
}

// StreamingTool is an optional interface implemented by long-running tools
// (bash, future job_output --follow) that want to push partial output to
// the TUI before Execute finishes. Implementations MUST still implement
// Execute as a fallback for callers that don't drive streaming. The
// dispatcher detects StreamingTool via a type assertion; tools that don't
// implement it keep their existing blocking Execute semantics.
type StreamingTool interface {
	Tool
	ExecuteStream(ctx context.Context, args json.RawMessage, events chan<- StreamEvent) (Result, error)
}

// Preview is the human-facing description of what a write-class tool is
// about to do. It is computed without touching disk state.
type Preview struct {
	Summary string     `json:"summary"`
	Files   []FileDiff `json:"files,omitempty"`
}

// FileDiff describes one file's planned change. Kind is one of the
// Kind* constants below.
type FileDiff struct {
	Path        string `json:"path"`
	Kind        string `json:"kind"`
	UnifiedDiff string `json:"unified_diff,omitempty"`
}

const (
	// KindCreate marks a file that will be (or was) newly created.
	KindCreate = "create"
	// KindModify marks an in-place modification of an existing file.
	KindModify = "modify"
	// KindDelete marks a file that will be (or was) removed.
	KindDelete = "delete"
)

// Result is the value a tool returns from Execute. Content is the text
// that will be fed back to the model as the tool_result. IsError lets a
// tool report a business-level failure without raising a Go error, so
// the dispatcher still routes the message to the model. Files is an
// optional summary of disk changes (post-execution) that the dispatcher
// may surface in the UI.
type Result struct {
	Content        string            `json:"content"`
	IsError        bool              `json:"is_error,omitempty"`
	Files          []FileChange      `json:"files,omitempty"`
	FullContent    string            `json:"-"`
	Truncated      bool              `json:"truncated,omitempty"`
	OriginalBytes  int               `json:"original_bytes,omitempty"`
	FullOutputPath string            `json:"full_output_path,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
}

// FileChange mirrors FileDiff at execution time. UnifiedDiff is optional
// because non-file tools, read-only tools, or external integrations may only
// be able to report a path/kind summary.
type FileChange struct {
	Path        string `json:"path"`
	Kind        string `json:"kind"`
	UnifiedDiff string `json:"unified_diff,omitempty"`
}
