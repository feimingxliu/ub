package goal

import (
	"os"
	"testing"
	"time"
)

func TestSaveAndLoad(t *testing.T) {
	sessionID := "test-session-1"
	g := &Goal{
		SessionID:   sessionID,
		Objective:   "Test objective",
		Status:      StatusActive,
		TokenBudget: 10000,
		TurnBudget:  50,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	if err := Save(sessionID, g); err != nil {
		t.Fatalf("Save: %v", err)
	}
	defer os.RemoveAll(stateDirForTest(t, sessionID))

	loaded, err := Load(sessionID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded == nil {
		t.Fatal("Load returned nil goal")
	}
	if loaded.Objective != "Test objective" {
		t.Fatalf("Objective = %q, want %q", loaded.Objective, "Test objective")
	}
	if loaded.Status != StatusActive {
		t.Fatalf("Status = %q, want %q", loaded.Status, StatusActive)
	}
	if loaded.TokenBudget != 10000 {
		t.Fatalf("TokenBudget = %d, want %d", loaded.TokenBudget, 10000)
	}
	if loaded.TurnBudget != 50 {
		t.Fatalf("TurnBudget = %d, want %d", loaded.TurnBudget, 50)
	}
}

func TestLoadNonExistent(t *testing.T) {
	loaded, err := Load("nonexistent-session")
	if err != nil {
		t.Fatalf("Load for nonexistent: %v", err)
	}
	if loaded != nil {
		t.Fatal("Expected nil for non-existent goal")
	}
}

func TestDelete(t *testing.T) {
	sessionID := "test-delete"
	g := &Goal{
		SessionID: sessionID,
		Objective: "to delete",
		Status:    StatusActive,
	}
	if err := Save(sessionID, g); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := Delete(sessionID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	loaded, _ := Load(sessionID)
	if loaded != nil {
		t.Fatal("Goal should be nil after delete")
	}
}

func TestIsTerminal(t *testing.T) {
	tests := []struct {
		status Status
		want   bool
	}{
		{StatusActive, false},
		{StatusPaused, false},
		{StatusBlocked, true},
		{StatusComplete, true},
		{StatusBudgetLimited, true},
	}
	for _, tt := range tests {
		got := IsTerminal(tt.status)
		if got != tt.want {
			t.Errorf("IsTerminal(%q) = %v, want %v", tt.status, got, tt.want)
		}
	}
}

func TestRecordUsageHitsTurnBudget(t *testing.T) {
	sessionID := "test-turn-budget"
	g := &Goal{
		SessionID:  sessionID,
		Objective:  "Test turn budget",
		Status:     StatusActive,
		TurnBudget: 3,
	}
	if err := Save(sessionID, g); err != nil {
		t.Fatalf("Save: %v", err)
	}
	defer os.RemoveAll(stateDirForTest(t, sessionID))

	// Record 2 turns; should still be active.
	if err := RecordUsage(sessionID, 100); err != nil {
		t.Fatalf("RecordUsage 1: %v", err)
	}
	if err := RecordUsage(sessionID, 200); err != nil {
		t.Fatalf("RecordUsage 2: %v", err)
	}
	loaded, _ := Load(sessionID)
	if loaded.Status != StatusActive {
		t.Fatalf("after 2 turns: status=%q, want active", loaded.Status)
	}
	if loaded.TokensUsed != 300 {
		t.Fatalf("TokensUsed = %d, want 300", loaded.TokensUsed)
	}
	if loaded.TurnsUsed != 2 {
		t.Fatalf("TurnsUsed = %d, want 2", loaded.TurnsUsed)
	}

	// Record turn 3; should hit budget.
	if err := RecordUsage(sessionID, 300); err != nil {
		t.Fatalf("RecordUsage 3: %v", err)
	}
	loaded, _ = Load(sessionID)
	if loaded.Status != StatusBudgetLimited {
		t.Fatalf("after 3 turns: status=%q, want budget_limited", loaded.Status)
	}
}

func TestRecordUsageHitsTokenBudget(t *testing.T) {
	sessionID := "test-token-budget"
	g := &Goal{
		SessionID:   sessionID,
		Objective:   "Test token budget",
		Status:      StatusActive,
		TokenBudget: 500,
	}
	if err := Save(sessionID, g); err != nil {
		t.Fatalf("Save: %v", err)
	}
	defer os.RemoveAll(stateDirForTest(t, sessionID))

	if err := RecordUsage(sessionID, 300); err != nil {
		t.Fatalf("RecordUsage 1: %v", err)
	}
	loaded, _ := Load(sessionID)
	if loaded.Status != StatusActive {
		t.Fatalf("after 300 tokens: status=%q, want active", loaded.Status)
	}

	if err := RecordUsage(sessionID, 300); err != nil {
		t.Fatalf("RecordUsage 2: %v", err)
	}
	loaded, _ = Load(sessionID)
	if loaded.Status != StatusBudgetLimited {
		t.Fatalf("after 600 tokens: status=%q, want budget_limited", loaded.Status)
	}
}

func TestNormalizeStatus(t *testing.T) {
	tests := []struct {
		input string
		want  Status
	}{
		{"complete", StatusComplete},
		{"done", StatusComplete},
		{"completed", StatusComplete},
		{"blocked", StatusBlocked},
		{"stuck", StatusBlocked},
		{"paused", StatusPaused},
		{"pause", StatusPaused},
		{"invalid", Status("")},
	}
	for _, tt := range tests {
		got := normalizeStatus(tt.input)
		if got != tt.want {
			t.Errorf("normalizeStatus(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestRenderGoal(t *testing.T) {
	g := Goal{
		SessionID:   "sess-1",
		Objective:   "Test objective",
		Status:      StatusActive,
		TokenBudget: 1000,
		TokensUsed:  200,
		CreatedAt:   time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:   time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	output := renderGoal(g)
	if output == "" {
		t.Fatal("renderGoal returned empty string")
	}
	if !contains(output, "Objective: Test objective") {
		t.Fatalf("renderGoal missing objective: %s", output)
	}
	if !contains(output, "Status: active") {
		t.Fatalf("renderGoal missing status: %s", output)
	}
	if !contains(output, "20%") {
		t.Fatalf("renderGoal missing percentage: %s", output)
	}
}

func stateDirForTest(t *testing.T, sessionID string) string {
	t.Helper()
	path, err := statePath(sessionID)
	if err != nil {
		t.Fatalf("statePath: %v", err)
	}
	// Return the parent "goals" directory for cleanup.
	dir := ""
	for i := 0; i < 3; i++ {
		dir = os.Getenv("HOME") + "/.local/state/ub/goals"
	}
	_ = path
	return dir
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && containsStr(s, substr)
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
