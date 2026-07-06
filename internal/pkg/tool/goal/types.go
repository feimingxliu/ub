// Package goal implements the goal-mode tools that let the agent work
// autonomously on a long-running objective across multiple turns.
//
// Goal state is session-scoped and persisted as a JSON file under the state
// root (mirroring the todo storage pattern). Three model-callable tools —
// create_goal, update_goal, and get_goal — let the agent manage the active
// goal. The host (TUI runner or CLI) checks the goal status after each
// Agent.Run completes and, if the goal is still Active, injects a
// continuation prompt and starts a new Run automatically.
package goal

import "time"

// Status is the lifecycle state of a goal.
type Status string

const (
	// StatusActive means the goal is in progress; the host should
	// auto-continue after each turn.
	StatusActive Status = "active"
	// StatusPaused means the goal is temporarily suspended; the host
	// waits for user action before resuming.
	StatusPaused Status = "paused"
	// StatusBlocked means the agent cannot make progress (same blocker
	// repeated 3+ consecutive turns). The host stops auto-continuation.
	StatusBlocked Status = "blocked"
	// StatusComplete means the objective is fully achieved and verified.
	// The host stops auto-continuation.
	StatusComplete Status = "complete"
	// StatusBudgetLimited means the token or turn budget is exhausted.
	// The host stops auto-continuation.
	StatusBudgetLimited Status = "budget_limited"
)

// Goal is the persisted goal state for one session.
type Goal struct {
	// SessionID is the session this goal belongs to.
	SessionID string `json:"session_id"`
	// Objective is the natural-language description of what the agent
	// should achieve. Capped at 4000 characters.
	Objective string `json:"objective"`
	// Status is the current lifecycle state.
	Status Status `json:"status"`
	// TokenBudget is the optional maximum total tokens the goal may
	// consume. Zero means no token budget.
	TokenBudget int `json:"token_budget,omitempty"`
	// TokensUsed is the accumulated token usage across all turns.
	TokensUsed int `json:"tokens_used,omitempty"`
	// TurnBudget is the optional maximum number of agent turns the goal
	// may run. Zero means no turn budget.
	TurnBudget int `json:"turn_budget,omitempty"`
	// TurnsUsed is the number of agent turns consumed so far.
	TurnsUsed int `json:"turns_used,omitempty"`
	// CreatedAt is when the goal was created.
	CreatedAt time.Time `json:"created_at"`
	// UpdatedAt is when the goal was last modified.
	UpdatedAt time.Time `json:"updated_at"`
	// BlockReason records the reason for the last blocked status, used
	// to detect repeated blockers.
	BlockReason string `json:"block_reason,omitempty"`
	// consecutiveBlockCount tracks how many consecutive turns reported
	// the same blocking condition. Internal; not meaningful to the model.
	ConsecutiveBlockCount int `json:"consecutive_block_count,omitempty"`
}

// IsTerminal reports whether the status indicates the goal should stop
// auto-continuing.
func IsTerminal(s Status) bool {
	switch s {
	case StatusComplete, StatusBlocked, StatusBudgetLimited:
		return true
	default:
		return false
	}
}

// maxObjectiveChars caps the objective length. Oversized objectives should
// be broken into smaller sub-goals by the agent.
const maxObjectiveChars = 4000
