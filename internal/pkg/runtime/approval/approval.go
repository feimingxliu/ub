// Package approval defines the secondary approval-agent interface used by
// auto execution mode.
package approval

import (
	"context"
	"encoding/json"

	"github.com/feimingxliu/ub/internal/pkg/core/execution"
	"github.com/feimingxliu/ub/internal/pkg/tool"
)

// Decision is the approval agent's limited answer space.
type Decision string

const (
	DecisionAllow  Decision = "allow"
	DecisionDeny   Decision = "deny"
	DecisionUnsure Decision = "unsure"
)

// Request is the sanitized information sent to the approval agent.
type Request struct {
	Tool           string          `json:"tool"`
	Args           json.RawMessage `json:"args,omitempty"`
	Risk           tool.Risk       `json:"risk"`
	Mode           execution.Mode  `json:"mode"`
	Command        string          `json:"command,omitempty"`
	Cwd            string          `json:"cwd,omitempty"`
	ContextSummary string          `json:"context_summary,omitempty"`
	RuleMatched    string          `json:"rule_matched,omitempty"`
}

// Result is the approval agent's decision plus a short explanation.
type Result struct {
	Decision Decision `json:"decision"`
	Reason   string   `json:"reason,omitempty"`
}

// Agent reviews command execution requests. It must not execute tools.
type Agent interface {
	ReviewCommand(ctx context.Context, req Request) (Result, error)
}
