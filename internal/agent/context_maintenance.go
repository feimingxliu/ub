package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	contextwindow "github.com/feimingxliu/ub/internal/context"
	"github.com/feimingxliu/ub/internal/message"
	"github.com/feimingxliu/ub/internal/provider"
	"github.com/feimingxliu/ub/internal/rollout"
	contextmgr "github.com/feimingxliu/ub/internal/tokenizer"
	"github.com/feimingxliu/ub/internal/workspace/tooloutput"
)

const prunedToolResultPlaceholder = "[context pruned: superseded tool result; full output remains in rollout]"

type contextMaintenanceResult struct {
	messages          []message.Message
	decision          contextwindow.ContextDecision
	estimatedTokens   int
	changed           bool
	summary           string
	compactedMessages int
	keptMessages      int
}

type contextMaintenanceAudit struct {
	decision contextwindow.ContextDecision
	before   int
	started  time.Time
}

type contextToolRecord struct {
	id          string
	name        string
	input       json.RawMessage
	useIndex    int
	resultIndex int
	output      string
	isError     bool
	prunable    bool
	protected   bool
}

func (a *Agent) maintainContext(ctx context.Context, sessionID string, turn int, messages []message.Message, estimated int, tools []provider.ToolDefinition, reason contextwindow.DecisionReason) (contextMaintenanceResult, error) {
	snapshot := a.contextSnapshot(messages, estimated, reason)
	decision := contextwindow.Plan(snapshot)
	if decision.Action == contextwindow.ActionKeep {
		return contextMaintenanceResult{messages: messages, decision: decision, estimatedTokens: estimated}, nil
	}

	audit := contextMaintenanceAudit{before: estimated, started: time.Now()}
	prunedMessages, pruned := pruneContextToolResults(messages, decision.PrunedToolUseIDs)
	decision.PrunedToolUseIDs = pruned
	prunedEstimated := contextmgr.EstimateRequest(a.withRuntimeContext(prunedMessages), tools, a.model)
	if shouldCompactAfterPrune(decision, snapshot.CanCompact, prunedEstimated) {
		decision.Action = contextwindow.ActionCompact
	}
	audit.decision = decision
	a.emitContextMaintenanceStart(decision)

	switch decision.Action {
	case contextwindow.ActionPrune:
		if len(pruned) == 0 {
			a.emitContextMaintenanceDone(decision, 0, false, estimated, estimated)
			return contextMaintenanceResult{messages: messages, decision: decision, estimatedTokens: estimated}, nil
		}
		if err := a.appendPruneMaintenance(ctx, sessionID, turn, prunedMessages, prunedEstimated, audit); err != nil {
			a.emitContextMaintenanceFailed(err)
			return contextMaintenanceResult{}, err
		}
		a.emitContextMaintenanceDone(decision, len(pruned), false, estimated, prunedEstimated)
		return contextMaintenanceResult{
			messages:        prunedMessages,
			decision:        decision,
			estimatedTokens: prunedEstimated,
			changed:         true,
			summary:         fmt.Sprintf("pruned %d superseded tool result(s)", len(pruned)),
		}, nil
	case contextwindow.ActionCompact, contextwindow.ActionCompactAndRetry:
		compacted, ok, err := a.compactMessages(ctx, sessionID, turn, prunedMessages, estimated, tools, audit)
		if err != nil {
			a.emitContextMaintenanceFailed(err)
			return contextMaintenanceResult{}, err
		}
		if !ok {
			if len(pruned) == 0 {
				a.emitContextMaintenanceDone(decision, 0, false, estimated, estimated)
				return contextMaintenanceResult{messages: messages, decision: decision, estimatedTokens: estimated}, nil
			}
			decision.Action = contextwindow.ActionPrune
			audit.decision = decision
			if err := a.appendPruneMaintenance(ctx, sessionID, turn, prunedMessages, prunedEstimated, audit); err != nil {
				a.emitContextMaintenanceFailed(err)
				return contextMaintenanceResult{}, err
			}
			a.emitContextMaintenanceDone(decision, len(pruned), false, estimated, prunedEstimated)
			return contextMaintenanceResult{
				messages:        prunedMessages,
				decision:        decision,
				estimatedTokens: prunedEstimated,
				changed:         true,
				summary:         fmt.Sprintf("pruned %d superseded tool result(s)", len(pruned)),
			}, nil
		}
		a.emitContextMaintenanceDone(decision, compacted.compactedMessages, true, estimated, compacted.estimatedTokens)
		return contextMaintenanceResult{
			messages:          compacted.messages,
			decision:          decision,
			estimatedTokens:   compacted.estimatedTokens,
			changed:           true,
			summary:           compacted.summary,
			compactedMessages: compacted.compactedMessages,
			keptMessages:      compacted.keptMessages,
		}, nil
	default:
		return contextMaintenanceResult{messages: messages, decision: decision, estimatedTokens: estimated}, nil
	}
}

// shouldCompactAfterPrune uses the actual post-prune request estimate rather
// than the planner's sum of per-result savings, which can differ after
// request-level rounding and calibration.
func shouldCompactAfterPrune(decision contextwindow.ContextDecision, canCompact bool, estimatedTokens int) bool {
	return decision.Action == contextwindow.ActionPrune &&
		decision.Reason == contextwindow.ReasonThreshold &&
		canCompact &&
		estimatedTokens > decision.TargetTokens
}

func (a *Agent) contextSnapshot(messages []message.Message, estimated int, reason contextwindow.DecisionReason) contextwindow.ContextSnapshot {
	window := a.effectiveContextWindow()
	prefix, _, canCompact := splitSummaryWindow(messages, summaryWindowOptions{
		KeepRecentTurns: effectiveKeepRecentTurns(a.contextCfg),
		MaxContext:      window.MaxTokens,
		Model:           a.model,
	})
	allowed := toolUseIDs(prefix)
	candidates, incomplete := a.contextToolCandidates(messages, allowed)
	return contextwindow.ContextSnapshot{
		EstimatedTokens:    estimated,
		MaxTokens:          window.MaxTokens,
		ReserveTokens:      tooloutput.ReserveOutputTokens(a.contextCfg),
		TriggerRatio:       effectiveTriggerRatio(a.contextCfg),
		Reason:             reason,
		CanCompact:         canCompact,
		IncompleteToolPair: incomplete,
		Candidates:         candidates,
	}
}

func toolUseIDs(messages []message.Message) map[string]struct{} {
	ids := make(map[string]struct{})
	for _, msg := range messages {
		for _, block := range msg.Content {
			if block.Type == message.BlockToolUse && strings.TrimSpace(block.ToolUseID) != "" {
				ids[block.ToolUseID] = struct{}{}
			}
		}
	}
	return ids
}

func (a *Agent) contextToolCandidates(messages []message.Message, allowed map[string]struct{}) ([]contextwindow.ToolResultCandidate, bool) {
	records := make([]*contextToolRecord, 0)
	byID := make(map[string]*contextToolRecord)
	incomplete := false
	for messageIndex, msg := range messages {
		for _, block := range msg.Content {
			switch block.Type {
			case message.BlockToolUse:
				id := strings.TrimSpace(block.ToolUseID)
				if id == "" {
					incomplete = true
					continue
				}
				if _, exists := byID[id]; exists {
					incomplete = true
					continue
				}
				record := &contextToolRecord{id: id, name: strings.TrimSpace(block.ToolName), input: append(json.RawMessage(nil), block.Input...), useIndex: messageIndex, resultIndex: -1}
				byID[id] = record
				records = append(records, record)
			case message.BlockToolResult:
				id := strings.TrimSpace(block.ToolUseID)
				record, ok := byID[id]
				if !ok || record.resultIndex >= 0 {
					incomplete = true
					continue
				}
				record.resultIndex = messageIndex
				record.output = block.Output
				record.isError = block.IsError
			}
		}
	}
	for _, record := range records {
		if record.resultIndex < 0 {
			incomplete = true
		}
	}

	latestByInput := make(map[string]*contextToolRecord)
	for _, record := range records {
		if !record.complete() || record.isError || isProtectedContextTool(record.name) {
			continue
		}
		if previous, ok := latestByInput[contextToolKey(record.name, record.input)]; ok && previous.canPrune(allowed) {
			previous.prunable = true
		}
		if isPrunableContextTool(record.name) {
			latestByInput[contextToolKey(record.name, record.input)] = record
		}
		if isEmptyContextToolOutput(record.name, record.output) && record.canPrune(allowed) {
			record.prunable = true
		}
	}

	candidates := make([]contextwindow.ToolResultCandidate, 0, len(records))
	for _, record := range records {
		if record.resultIndex < 0 {
			continue
		}
		_, inPrefix := allowed[record.id]
		protected := !inPrefix || record.isError || isProtectedContextTool(record.name) || !record.prunable
		candidate := contextwindow.ToolResultCandidate{ToolUseID: record.id, Protected: protected}
		if record.prunable && !protected {
			candidate.Prunable = true
			candidate.EstimatedSavings = a.contextToolResultSavings(record.id, record.output)
			if candidate.EstimatedSavings <= 0 {
				candidate.Prunable = false
				candidate.Protected = true
			}
		}
		candidates = append(candidates, candidate)
	}
	return candidates, incomplete
}

func (record *contextToolRecord) complete() bool {
	return record != nil && record.resultIndex >= 0 && strings.TrimSpace(record.id) != ""
}

func (record *contextToolRecord) canPrune(allowed map[string]struct{}) bool {
	if !record.complete() || record.isError || isProtectedContextTool(record.name) {
		return false
	}
	_, ok := allowed[record.id]
	return ok
}

func contextToolKey(name string, input json.RawMessage) string {
	var compact bytes.Buffer
	if json.Compact(&compact, input) == nil {
		return strings.ToLower(strings.TrimSpace(name)) + "\x00" + compact.String()
	}
	return strings.ToLower(strings.TrimSpace(name)) + "\x00" + string(input)
}

func isPrunableContextTool(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "read", "grep":
		return true
	default:
		return false
	}
}

func isProtectedContextTool(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "apply_patch", "write", "edit", "multiedit", "bash", "plan_write", "plan_update", "todo_update", "create_goal", "update_goal", "remember":
		return true
	default:
		return false
	}
}

func isEmptyContextToolOutput(name, output string) bool {
	if !isPrunableContextTool(name) {
		return false
	}
	normalized := strings.ToLower(strings.TrimSpace(output))
	switch normalized {
	case "", "no results", "no results found", "no matches", "no matches found", "[]":
		return true
	default:
		return false
	}
}

func (a *Agent) contextToolResultSavings(toolUseID, output string) int {
	before := contextmgr.Estimate([]message.Message{message.New(message.RoleTool, message.ToolResultBlock(toolUseID, output, false))}, a.model)
	after := contextmgr.Estimate([]message.Message{message.New(message.RoleTool, message.ToolResultBlock(toolUseID, prunedToolResultPlaceholder, false))}, a.model)
	if before <= after {
		return 0
	}
	return before - after
}

func pruneContextToolResults(messages []message.Message, toolUseIDs []string) ([]message.Message, []string) {
	if len(toolUseIDs) == 0 {
		return messages, nil
	}
	prune := make(map[string]struct{}, len(toolUseIDs))
	for _, id := range toolUseIDs {
		if strings.TrimSpace(id) != "" {
			prune[id] = struct{}{}
		}
	}
	out := cloneMessages(messages)
	applied := make([]string, 0, len(prune))
	seen := make(map[string]struct{}, len(prune))
	for messageIndex := range out {
		for blockIndex := range out[messageIndex].Content {
			block := &out[messageIndex].Content[blockIndex]
			if block.Type != message.BlockToolResult || block.IsError {
				continue
			}
			if _, ok := prune[block.ToolUseID]; !ok || block.Output == prunedToolResultPlaceholder {
				continue
			}
			block.Output = prunedToolResultPlaceholder
			if _, ok := seen[block.ToolUseID]; !ok {
				seen[block.ToolUseID] = struct{}{}
				applied = append(applied, block.ToolUseID)
			}
		}
	}
	return out, applied
}

func (a *Agent) appendPruneMaintenance(ctx context.Context, sessionID string, turn int, messages []message.Message, after int, audit contextMaintenanceAudit) error {
	return a.append(ctx, sessionID, func() (rollout.Event, error) {
		return rollout.SummaryWithMessagesAndMaintenance(
			sessionID,
			turn,
			fmt.Sprintf("Context maintenance pruned %d superseded tool result(s).", len(audit.decision.PrunedToolUseIDs)),
			messages,
			0,
			len(messages),
			audit.before,
			a.rolloutContextMaintenance(audit.decision, audit.before, after, 0, audit.started),
		)
	})
}

func (a *Agent) rolloutContextMaintenance(decision contextwindow.ContextDecision, before, after, cutoff int, started time.Time) *rollout.ContextMaintenance {
	return &rollout.ContextMaintenance{
		Decision:            string(decision.Action),
		Reason:              string(decision.Reason),
		TokensBefore:        before,
		TokensAfter:         after,
		CutoffMessages:      cutoff,
		PrunedToolUseIDs:    append([]string(nil), decision.PrunedToolUseIDs...),
		ProtectedToolUseIDs: append([]string(nil), decision.ProtectedToolUseIDs...),
		SummaryModel:        a.effectiveSummaryModel(),
		DurationMillis:      time.Since(started).Milliseconds(),
		Retry:               decision.Retry,
	}
}

func (a *Agent) effectiveSummaryModel() string {
	if model := strings.TrimSpace(a.summaryModel); model != "" {
		return model
	}
	return strings.TrimSpace(a.model)
}

func (a *Agent) emitContextMaintenanceStart(decision contextwindow.ContextDecision) {
	summary := "pruning superseded tool results"
	if decision.Action == contextwindow.ActionCompact || decision.Action == contextwindow.ActionCompactAndRetry {
		summary = "compacting context"
	}
	if decision.Reason == contextwindow.ReasonOverflow {
		summary = "provider context limit exceeded; " + summary
	}
	a.emit(Event{
		Type:         EventActivity,
		ActivityKind: ActivityNotice,
		Notice:       NoticeCompacting,
		Status:       "running",
		Summary:      summary,
		Decision:     string(decision.Action),
		Reason:       string(decision.Reason),
	})
}

func (a *Agent) emitContextMaintenanceDone(decision contextwindow.ContextDecision, affected int, compacted bool, before, after int) {
	summary := fmt.Sprintf("pruned %d superseded tool result(s)", affected)
	if compacted {
		summary = fmt.Sprintf("compacted %d earlier messages", affected)
	}
	if decision.Reason == contextwindow.ReasonOverflow {
		summary = "provider context limit exceeded; " + summary
	}
	a.emit(Event{
		Type:         EventActivity,
		ActivityKind: ActivityNotice,
		Notice:       NoticeCompacting,
		Status:       "done",
		Summary:      summary,
		Decision:     string(decision.Action),
		Reason:       contextDecisionDetail(decision, before, after, a.effectiveSummaryModel()),
	})
}

func (a *Agent) emitContextMaintenanceFailed(err error) {
	a.emit(Event{Type: EventActivity, ActivityKind: ActivityNotice, Notice: NoticeCompacting, Status: "failed", Summary: fmt.Sprintf("context maintenance failed: %v", err)})
}

func contextDecisionDetail(decision contextwindow.ContextDecision, before, after int, summaryModel string) string {
	parts := []string{
		"reason=" + string(decision.Reason),
		fmt.Sprintf("tokens=%d->%d", before, after),
	}
	if len(decision.PrunedToolUseIDs) > 0 {
		parts = append(parts, "pruned="+strings.Join(decision.PrunedToolUseIDs, ","))
	}
	if len(decision.ProtectedToolUseIDs) > 0 {
		parts = append(parts, "protected="+strings.Join(decision.ProtectedToolUseIDs, ","))
	}
	if decision.Action == contextwindow.ActionCompact || decision.Action == contextwindow.ActionCompactAndRetry {
		parts = append(parts, "summary_model="+summaryModel)
	}
	if decision.Retry {
		parts = append(parts, "retry=true")
	}
	return strings.Join(parts, " ")
}
