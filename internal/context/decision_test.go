package contextwindow

import (
	"reflect"
	"testing"
)

func TestPlanContextMaintenance(t *testing.T) {
	tests := []struct {
		name     string
		snapshot ContextSnapshot
		want     ContextDecision
	}{
		{
			name:     "unknown window stays safe",
			snapshot: ContextSnapshot{EstimatedTokens: 90, Reason: ReasonThreshold},
			want:     ContextDecision{Action: ActionKeep, Reason: ReasonThreshold},
		},
		{
			name: "threshold prunes when enough",
			snapshot: ContextSnapshot{
				EstimatedTokens: 81, MaxTokens: 100, TriggerRatio: 0.8, Reason: ReasonThreshold,
				Candidates: []ToolResultCandidate{{ToolUseID: "old-read", Prunable: true, EstimatedSavings: 4}, {ToolUseID: "recent", Protected: true}},
			},
			want: ContextDecision{Action: ActionPrune, Reason: ReasonThreshold, TargetTokens: 80, PrunedToolUseIDs: []string{"old-read"}, ProtectedToolUseIDs: []string{"recent"}},
		},
		{
			name: "threshold does not double count reserve",
			snapshot: ContextSnapshot{
				EstimatedTokens: 55, MaxTokens: 100, ReserveTokens: 20, TriggerRatio: 0.8, Reason: ReasonThreshold,
			},
			want: ContextDecision{Action: ActionKeep, Reason: ReasonThreshold, TargetTokens: 60},
		},
		{
			name: "threshold compacts when pruning is insufficient",
			snapshot: ContextSnapshot{
				EstimatedTokens: 100, MaxTokens: 100, TriggerRatio: 0.8, Reason: ReasonThreshold, CanCompact: true,
				Candidates: []ToolResultCandidate{{ToolUseID: "old-read", Prunable: true, EstimatedSavings: 4}},
			},
			want: ContextDecision{Action: ActionCompact, Reason: ReasonThreshold, TargetTokens: 80, PrunedToolUseIDs: []string{"old-read"}},
		},
		{
			name: "manual compacts and includes safe pruning",
			snapshot: ContextSnapshot{
				Reason: ReasonManual, CanCompact: true,
				Candidates: []ToolResultCandidate{{ToolUseID: "read", Prunable: true, EstimatedSavings: 10}},
			},
			want: ContextDecision{Action: ActionCompact, Reason: ReasonManual, PrunedToolUseIDs: []string{"read"}},
		},
		{
			name: "mid turn preserves pairing",
			snapshot: ContextSnapshot{
				EstimatedTokens: 100, MaxTokens: 100, TriggerRatio: 0.8, Reason: ReasonThreshold,
				IncompleteToolPair: true, Candidates: []ToolResultCandidate{{ToolUseID: "call", Prunable: true, EstimatedSavings: 20}},
			},
			want: ContextDecision{Action: ActionKeep, Reason: ReasonMidTurn, TargetTokens: 80},
		},
		{
			name: "overflow compact retry once",
			snapshot: ContextSnapshot{
				Reason: ReasonOverflow, CanCompact: true,
				Candidates: []ToolResultCandidate{{ToolUseID: "old", Prunable: true, EstimatedSavings: 1}},
			},
			want: ContextDecision{Action: ActionCompactAndRetry, Reason: ReasonOverflow, PrunedToolUseIDs: []string{"old"}, Retry: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Plan(tt.snapshot); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Plan(%#v) = %#v, want %#v", tt.snapshot, got, tt.want)
			}
		})
	}
}

func TestPlanIsDeterministicAndSortsIDs(t *testing.T) {
	snapshot := ContextSnapshot{
		EstimatedTokens: 100, MaxTokens: 100, TriggerRatio: 0.8, Reason: ReasonThreshold,
		Candidates: []ToolResultCandidate{
			{ToolUseID: "z", Prunable: true, EstimatedSavings: 11},
			{ToolUseID: "a", Prunable: true, EstimatedSavings: 11},
			{ToolUseID: "protected", Protected: true},
		},
	}
	first := Plan(snapshot)
	second := Plan(snapshot)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("Plan must be deterministic: %#v != %#v", first, second)
	}
	if got, want := first.PrunedToolUseIDs, []string{"a", "z"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("pruned IDs = %#v, want %#v", got, want)
	}
}
