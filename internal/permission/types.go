// Package permission implements execution-mode-aware tool approval.
package permission

import (
	"context"
	"encoding/json"

	"github.com/feimingxliu/ub/internal/execution"
	"github.com/feimingxliu/ub/internal/tool"
)

// Decision is the human approval decision.
type Decision string

const (
	DecisionAllow                Decision = "allow"
	DecisionDeny                 Decision = "deny"
	DecisionAlwaysCmd            Decision = "always_cmd"
	DecisionAlwaysTool           Decision = "always_tool"
	DecisionAlwaysProjectCmd     Decision = "always_project_cmd"
	DecisionAlwaysProjectPattern Decision = "always_project_pattern"
)

// Source identifies where a final permission result came from.
type Source string

const (
	SourceMode          Source = "mode"
	SourceAuto          Source = "auto"
	SourceRule          Source = "rule"
	SourceApprovalAgent Source = "approval_agent"
	SourceHuman         Source = "human"
)

// Request is the permission manager input for one tool call.
type Request struct {
	Tool             string
	Args             json.RawMessage
	Risk             tool.Risk
	Mode             execution.Mode
	Preview          *tool.Preview
	Command          string
	Cwd              string
	Workspace        string
	ContextSummary   string
	ApprovalReason   string
	ApprovalObserver func(ApprovalObservation)
}

// Result is the permission manager output.
type Result struct {
	Decision Decision
	Allowed  bool
	Source   Source
	Reason   string
}

// ApprovalObservation reports an auto-mode approval agent result before any
// fallback to human approval.
type ApprovalObservation struct {
	Decision string
	Reason   string
	Err      error
}

// Asker asks the human approval UI.
type Asker interface {
	Ask(ctx context.Context, req Request) (Decision, error)
}
