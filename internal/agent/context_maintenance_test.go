package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/feimingxliu/ub/internal/config"
	contextwindow "github.com/feimingxliu/ub/internal/context"
	"github.com/feimingxliu/ub/internal/message"
	execmode "github.com/feimingxliu/ub/internal/mode"
	"github.com/feimingxliu/ub/internal/provider"
	"github.com/feimingxliu/ub/internal/provider/fake"
	"github.com/feimingxliu/ub/internal/rollout"
	"github.com/feimingxliu/ub/internal/tool"
)

func TestContextToolCandidatesPruneOnlySupersededPrefixRead(t *testing.T) {
	oldInput := json.RawMessage(`{"path":"README.md"}`)
	messages := []message.Message{
		message.Text(message.RoleUser, "first request"),
		message.New(message.RoleAssistant, message.ToolUseBlock("read-old", "read", oldInput)),
		message.New(message.RoleTool, message.ToolResultBlock("read-old", strings.Repeat("old file contents ", 64), false)),
		message.Text(message.RoleUser, "second request"),
		message.New(message.RoleAssistant, message.ToolUseBlock("read-new", "read", oldInput)),
		message.New(message.RoleTool, message.ToolResultBlock("read-new", "new file contents", false)),
	}
	a := &Agent{model: "fake/model"}
	candidates, incomplete := a.contextToolCandidates(messages, map[string]struct{}{"read-old": {}})
	if incomplete {
		t.Fatalf("complete pairs reported incomplete: %#v", candidates)
	}
	byID := make(map[string]bool)
	for _, candidate := range candidates {
		byID[candidate.ToolUseID] = candidate.Prunable && !candidate.Protected
	}
	if !byID["read-old"] {
		t.Fatalf("old duplicate read was not prunable: %#v", candidates)
	}
	if byID["read-new"] {
		t.Fatalf("retained suffix read must stay protected: %#v", candidates)
	}
}

func TestContextToolCandidatesPruneSupersededPrefixGrep(t *testing.T) {
	input := json.RawMessage(`{"pattern":"ContextDecision","path":"internal"}`)
	messages := []message.Message{
		message.Text(message.RoleUser, "first request"),
		message.New(message.RoleAssistant, message.ToolUseBlock("grep-old", "grep", input)),
		message.New(message.RoleTool, message.ToolResultBlock("grep-old", strings.Repeat("internal/context/decision.go:1:ContextDecision\n", 32), false)),
		message.Text(message.RoleUser, "second request"),
		message.New(message.RoleAssistant, message.ToolUseBlock("grep-new", "grep", input)),
		message.New(message.RoleTool, message.ToolResultBlock("grep-new", "internal/context/decision.go:1:ContextDecision", false)),
	}
	a := &Agent{model: "fake/model"}
	candidates, incomplete := a.contextToolCandidates(messages, map[string]struct{}{"grep-old": {}})
	if incomplete {
		t.Fatalf("complete pairs reported incomplete: %#v", candidates)
	}
	for _, candidate := range candidates {
		if candidate.ToolUseID == "grep-old" && candidate.Prunable && !candidate.Protected {
			return
		}
	}
	t.Fatalf("old duplicate grep was not prunable: %#v", candidates)
}

func TestShouldCompactAfterPrune(t *testing.T) {
	decision := contextwindow.ContextDecision{
		Action:       contextwindow.ActionPrune,
		Reason:       contextwindow.ReasonThreshold,
		TargetTokens: 80,
	}
	if !shouldCompactAfterPrune(decision, true, 81) {
		t.Fatal("post-prune estimate above target must compact")
	}
	if shouldCompactAfterPrune(decision, true, 80) {
		t.Fatal("post-prune estimate at target must not compact")
	}
	if shouldCompactAfterPrune(decision, false, 81) {
		t.Fatal("no compact prefix must not compact")
	}
	decision.Reason = contextwindow.ReasonManual
	if shouldCompactAfterPrune(decision, true, 81) {
		t.Fatal("manual prune fallback must not use threshold compact")
	}
}

func TestPruneContextToolResultsPreservesPairingAndTranscript(t *testing.T) {
	input := json.RawMessage(`{"path":"README.md"}`)
	original := []message.Message{
		message.New(message.RoleAssistant, message.ToolUseBlock("read-old", "read", input)),
		message.New(message.RoleTool, message.ToolResultBlock("read-old", "old file contents", false)),
	}
	pruned, ids := pruneContextToolResults(original, []string{"read-old"})
	if got, want := ids, []string{"read-old"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("applied IDs = %#v, want %#v", got, want)
	}
	if got := pruned[0].Content[0]; got.Type != message.BlockToolUse || got.ToolUseID != "read-old" {
		t.Fatalf("tool use pairing changed: %#v", got)
	}
	if got := pruned[1].Content[0]; got.Type != message.BlockToolResult || got.ToolUseID != "read-old" || got.Output != prunedToolResultPlaceholder {
		t.Fatalf("tool result was not replaced safely: %#v", got)
	}
	if got := original[1].Content[0].Output; got != "old file contents" {
		t.Fatalf("original transcript mutated: %q", got)
	}
}

func TestContextToolCandidatesProtectErrorsAndMutationTools(t *testing.T) {
	messages := []message.Message{
		message.New(
			message.RoleAssistant,
			message.ToolUseBlock("failed-search", "search", json.RawMessage(`{"query":"missing"}`)),
			message.ToolUseBlock("write-file", "write", json.RawMessage(`{"path":"main.go"}`)),
		),
		message.New(
			message.RoleTool,
			message.ToolResultBlock("failed-search", "permission denied", true),
			message.ToolResultBlock("write-file", "wrote main.go", false),
		),
	}
	a := &Agent{model: "fake/model"}
	candidates, incomplete := a.contextToolCandidates(messages, map[string]struct{}{"failed-search": {}, "write-file": {}})
	if incomplete {
		t.Fatalf("complete pairs reported incomplete: %#v", candidates)
	}
	for _, candidate := range candidates {
		if candidate.Prunable || !candidate.Protected {
			t.Fatalf("unsafe candidate classification: %#v", candidates)
		}
	}
}

func TestContextToolCandidatesKeepWebResultsProtected(t *testing.T) {
	messages := []message.Message{
		message.New(message.RoleAssistant, message.ToolUseBlock("web-empty", "web_search", json.RawMessage(`{"query":"ub context management"}`))),
		message.New(message.RoleTool, message.ToolResultBlock("web-empty", "[]", false)),
	}
	a := &Agent{model: "fake/model"}
	candidates, incomplete := a.contextToolCandidates(messages, map[string]struct{}{"web-empty": {}})
	if incomplete {
		t.Fatalf("complete pair reported incomplete: %#v", candidates)
	}
	if len(candidates) != 1 || candidates[0].Prunable || !candidates[0].Protected {
		t.Fatalf("web result must stay protected: %#v", candidates)
	}
}

func TestAgentPrunesSupersededReadBeforeSummary(t *testing.T) {
	reg := tool.New()
	perm := newPermissionManager(t, nil)
	main := &scriptProvider{
		caps:    provider.Caps{MaxContextTokens: 100_000},
		scripts: []fake.Script{{fake.TextDelta("final"), fake.Done()}},
	}
	summary := &scriptProvider{scripts: []fake.Script{{fake.Error("summary must not run")}}}
	writer := &recordingRollout{}
	var events []Event
	a, err := New(Options{
		Provider:        main,
		SummaryProvider: summary,
		SummaryModel:    "summary/model",
		Tools:           reg,
		Permission:      perm,
		Rollout:         writer,
		Model:           "fake/model",
		Mode:            execmode.ModeWork,
		Context: config.ContextConfig{
			TriggerRatio:    0.2,
			KeepRecentTurns: 3,
		},
		Events: func(event Event) {
			events = append(events, event)
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	readInput := json.RawMessage(`{"path":"README.md"}`)
	history := []message.Message{
		message.Text(message.RoleUser, "first request"),
		message.New(message.RoleAssistant, message.ToolUseBlock("read-old", "read", readInput)),
		message.New(message.RoleTool, message.ToolResultBlock("read-old", strings.Repeat("old contents ", 8000), false)),
		message.Text(message.RoleUser, "second request"),
		message.New(message.RoleAssistant, message.ToolUseBlock("read-new", "read", readInput)),
		message.New(message.RoleTool, message.ToolResultBlock("read-new", "new contents", false)),
		message.Text(message.RoleUser, "third request"),
		message.Text(message.RoleAssistant, "third answer"),
	}
	result, err := a.Run(context.Background(), Request{SessionID: "sess_prune", Turn: 4, History: history, Prompt: "continue"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(summary.requests) != 0 {
		t.Fatalf("summary requests = %d, want 0", len(summary.requests))
	}
	if len(main.requests) != 1 {
		t.Fatalf("main requests = %d, want 1", len(main.requests))
	}
	if !containsToolResultOutput(result.ContextMessages, "read-old", prunedToolResultPlaceholder) {
		t.Fatalf("provider context did not retain pruned pairing: %#v", result.ContextMessages)
	}
	if !containsToolResultOutput(result.Messages, "read-old", strings.Repeat("old contents ", 8000)) {
		t.Fatalf("full transcript lost original tool output: %#v", result.Messages)
	}
	if !hasEventType(writer.events, rollout.TypeSummary) {
		t.Fatalf("prune checkpoint missing from rollout: %#v", writer.events)
	}
	var payload rollout.SummaryPayload
	for _, event := range writer.events {
		if event.Type == rollout.TypeSummary {
			if err := json.Unmarshal(event.Payload, &payload); err != nil {
				t.Fatalf("decode maintenance payload: %v", err)
			}
			break
		}
	}
	if payload.Maintenance == nil || payload.Maintenance.Decision != "prune" || payload.Maintenance.Reason != "threshold" {
		t.Fatalf("maintenance audit = %#v", payload.Maintenance)
	}
	foundDecision := false
	for _, event := range events {
		if event.Type == EventActivity && event.ActivityKind == ActivityNotice && event.Status == "done" && event.Decision == "prune" {
			if !strings.Contains(event.Reason, "reason=threshold") || !strings.Contains(event.Reason, "tokens=") || !strings.Contains(event.Reason, "pruned=read-old") {
				t.Fatalf("maintenance activity detail = %q", event.Reason)
			}
			foundDecision = true
		}
	}
	if !foundDecision {
		t.Fatalf("events did not expose pruning decision: %#v", events)
	}
}

func containsToolResultOutput(messages []message.Message, toolUseID, output string) bool {
	for _, msg := range messages {
		for _, block := range msg.Content {
			if block.Type == message.BlockToolResult && block.ToolUseID == toolUseID && block.Output == output {
				return true
			}
		}
	}
	return false
}
