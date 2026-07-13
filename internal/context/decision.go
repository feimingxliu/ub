// Package contextwindow resolves provider context windows and plans safe
// maintenance of provider-facing conversation context.
package contextwindow

import "sort"

// DecisionAction identifies the context maintenance operation selected for a
// provider request. The planner never mutates messages; Agent executes the
// selected action after it has verified message boundaries and tool pairing.
type DecisionAction string

const (
	ActionKeep            DecisionAction = "keep"
	ActionPrune           DecisionAction = "prune"
	ActionCompact         DecisionAction = "compact"
	ActionCompactAndRetry DecisionAction = "compact-and-retry"
)

// DecisionReason identifies why a context decision was evaluated.
type DecisionReason string

const (
	ReasonThreshold  DecisionReason = "threshold"
	ReasonManual     DecisionReason = "manual"
	ReasonOverflow   DecisionReason = "overflow"
	ReasonIncomplete DecisionReason = "incomplete"
	ReasonMidTurn    DecisionReason = "mid_turn"
)

// ToolResultCandidate describes one complete tool call/result pair in the
// provider context. The Agent determines eligibility; the planner only uses
// the stable, serializable fields below to choose an action.
type ToolResultCandidate struct {
	ToolUseID        string `json:"tool_use_id"`
	Prunable         bool   `json:"prunable,omitempty"`
	Protected        bool   `json:"protected,omitempty"`
	EstimatedSavings int    `json:"estimated_savings,omitempty"`
}

// ContextSnapshot is the deterministic input to Plan. Token counts describe
// the whole provider request, including static runtime messages and tools.
type ContextSnapshot struct {
	EstimatedTokens    int                   `json:"estimated_tokens"`
	MaxTokens          int                   `json:"max_tokens,omitempty"`
	ReserveTokens      int                   `json:"reserve_tokens,omitempty"`
	TriggerRatio       float64               `json:"trigger_ratio,omitempty"`
	Reason             DecisionReason        `json:"reason"`
	CanCompact         bool                  `json:"can_compact,omitempty"`
	IncompleteToolPair bool                  `json:"incomplete_tool_pair,omitempty"`
	Candidates         []ToolResultCandidate `json:"candidates,omitempty"`
}

// ContextDecision records the selected operation together with the candidates
// that must stay visible to the provider. It is safe to persist in rollout
// metadata because it contains no prompt or tool-result content.
type ContextDecision struct {
	Action              DecisionAction `json:"action"`
	Reason              DecisionReason `json:"reason"`
	TargetTokens        int            `json:"target_tokens,omitempty"`
	PrunedToolUseIDs    []string       `json:"pruned_tool_use_ids,omitempty"`
	ProtectedToolUseIDs []string       `json:"protected_tool_use_ids,omitempty"`
	Retry               bool           `json:"retry,omitempty"`
}

// Plan returns the same decision for the same snapshot. Unknown context
// windows are intentionally safe: threshold maintenance is not attempted.
func Plan(snapshot ContextSnapshot) ContextDecision {
	reason := snapshot.Reason
	if reason == "" {
		reason = ReasonThreshold
	}
	protected := candidateIDs(snapshot.Candidates, func(candidate ToolResultCandidate) bool {
		return candidate.Protected
	})
	decision := ContextDecision{
		Action:              ActionKeep,
		Reason:              reason,
		TargetTokens:        targetTokens(snapshot),
		ProtectedToolUseIDs: protected,
	}
	if snapshot.IncompleteToolPair {
		decision.Reason = ReasonMidTurn
		return decision
	}

	prunable := candidateIDs(snapshot.Candidates, func(candidate ToolResultCandidate) bool {
		return candidate.Prunable && !candidate.Protected
	})
	savings := candidateSavings(snapshot.Candidates)

	switch reason {
	case ReasonManual:
		if snapshot.CanCompact {
			decision.Action = ActionCompact
			decision.PrunedToolUseIDs = prunable
			return decision
		}
		if len(prunable) > 0 {
			decision.Action = ActionPrune
			decision.PrunedToolUseIDs = prunable
		}
		return decision
	case ReasonOverflow:
		decision.Retry = true
		if snapshot.CanCompact {
			decision.Action = ActionCompactAndRetry
			decision.PrunedToolUseIDs = prunable
			return decision
		}
		if len(prunable) > 0 {
			decision.Action = ActionPrune
			decision.PrunedToolUseIDs = prunable
		}
		return decision
	case ReasonIncomplete, ReasonMidTurn:
		decision.Reason = ReasonIncomplete
		return decision
	case ReasonThreshold:
		if !overBudget(snapshot) {
			return decision
		}
		if len(prunable) > 0 && snapshot.EstimatedTokens-savings <= decision.TargetTokens {
			decision.Action = ActionPrune
			decision.PrunedToolUseIDs = prunable
			return decision
		}
		if snapshot.CanCompact {
			decision.Action = ActionCompact
			decision.PrunedToolUseIDs = prunable
			return decision
		}
		if len(prunable) > 0 {
			decision.Action = ActionPrune
			decision.PrunedToolUseIDs = prunable
		}
		return decision
	default:
		return decision
	}
}

func overBudget(snapshot ContextSnapshot) bool {
	if snapshot.EstimatedTokens <= 0 || snapshot.MaxTokens <= 0 {
		return false
	}
	// targetTokens already reserves the requested output budget. Comparing the
	// input estimate directly avoids subtracting ReserveTokens twice.
	return snapshot.EstimatedTokens > targetTokens(snapshot)
}

func targetTokens(snapshot ContextSnapshot) int {
	if snapshot.MaxTokens <= 0 {
		return 0
	}
	ratio := snapshot.TriggerRatio
	if ratio <= 0 {
		ratio = 0.8
	}
	target := int(float64(snapshot.MaxTokens) * ratio)
	target -= max(snapshot.ReserveTokens, 0)
	if target < 0 {
		return 0
	}
	return target
}

func candidateSavings(candidates []ToolResultCandidate) int {
	total := 0
	for _, candidate := range candidates {
		if candidate.Prunable && !candidate.Protected && candidate.EstimatedSavings > 0 {
			total += candidate.EstimatedSavings
		}
	}
	return total
}

func candidateIDs(candidates []ToolResultCandidate, include func(ToolResultCandidate) bool) []string {
	seen := make(map[string]struct{}, len(candidates))
	ids := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.ToolUseID == "" || !include(candidate) {
			continue
		}
		if _, ok := seen[candidate.ToolUseID]; ok {
			continue
		}
		seen[candidate.ToolUseID] = struct{}{}
		ids = append(ids, candidate.ToolUseID)
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		return nil
	}
	return ids
}
