package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"

	_ "embed"
	"github.com/feimingxliu/ub/internal/pkg/core/config"
	"github.com/feimingxliu/ub/internal/pkg/core/message"
	contextmgr "github.com/feimingxliu/ub/internal/pkg/llm/context"
	"github.com/feimingxliu/ub/internal/pkg/llm/provider"
	"github.com/feimingxliu/ub/internal/pkg/workspace/rollout"
	"github.com/feimingxliu/ub/internal/pkg/workspace/tooloutput"
)

const (
	defaultTriggerRatio    = 0.8
	defaultKeepRecentTurns = 3
	summaryFallbackBudget  = 32000
	summaryMergeMaxDepth   = 4
)

//go:embed summary_prompt.txt
var summaryPromptTemplate string

//go:embed summary_short_prompt.txt
var summaryShortPromptTemplate string

// preparedMessages holds the message slices and token estimate produced by
// prepareMessages. messages is the compacted history (without runtime context);
// requestMessages is what gets sent to the provider (with runtime context prepended).
type preparedMessages struct {
	messages        []message.Message
	requestMessages []message.Message
	estimatedTokens int
}

// CompactRequest is one manual context compact request.
type CompactRequest struct {
	SessionID string
	Turn      int
	History   []message.Message
}

// CompactResult reports the result of a manual context compact request.
type CompactResult struct {
	Messages          []message.Message
	Summary           string
	CompactedMessages int
	KeptMessages      int
	EstimatedTokens   int
	Noop              bool
	Reason            string
}

// prepareMessages estimates the token count for the upcoming provider request
// and triggers context compaction if the estimate exceeds the configured
// threshold. It returns the (possibly compacted) messages, the provider-facing
// request messages with runtime context prepended, and the final token estimate.
func (a *Agent) prepareMessages(ctx context.Context, sessionID string, turn int, messages []message.Message, tools []provider.ToolDefinition) (preparedMessages, error) {
	requestMessages := cloneMessages(messages)
	providerMessages := a.withRuntimeContext(requestMessages)
	estimated := contextmgr.EstimateRequest(providerMessages, tools, a.model)
	if !a.shouldSummarize(estimated) {
		a.emitContextUsage(estimated, false)
		return preparedMessages{messages: requestMessages, requestMessages: providerMessages, estimatedTokens: estimated}, nil
	}
	a.emit(Event{
		Type:         EventActivity,
		ActivityKind: ActivityNotice,
		Notice:       NoticeCompacting,
		Status:       "running",
		Summary:      "compacting context",
	})
	compacted, ok, err := a.compactMessages(ctx, sessionID, turn, requestMessages, estimated, tools)
	if err != nil {
		slog.Warn("context compaction failed", "session", sessionID, "turn", turn, "err", err)
		a.emit(Event{
			Type:         EventActivity,
			ActivityKind: ActivityNotice,
			Notice:       NoticeCompacting,
			Status:       "failed",
			Summary:      fmt.Sprintf("compacting context failed: %v", err),
		})
		return preparedMessages{}, err
	}
	if !ok {
		a.emit(Event{
			Type:         EventActivity,
			ActivityKind: ActivityNotice,
			Notice:       NoticeCompacting,
			Status:       "done",
			Summary:      "nothing to compact yet",
		})
		a.emitContextUsage(estimated, false)
		return preparedMessages{messages: requestMessages, requestMessages: providerMessages, estimatedTokens: estimated}, nil
	}
	a.emit(Event{
		Type:         EventActivity,
		ActivityKind: ActivityNotice,
		Notice:       NoticeCompacting,
		Status:       "done",
		Summary:      fmt.Sprintf("compacted %d earlier messages", compacted.compactedMessages),
	})
	a.emitContextUsage(compacted.estimatedTokens, true)
	return preparedMessages{
		messages:        compacted.messages,
		requestMessages: a.withRuntimeContext(compacted.messages),
		estimatedTokens: compacted.estimatedTokens,
	}, nil
}

// Compact manually summarizes earlier history without checking the automatic
// trigger threshold.
func (a *Agent) Compact(ctx context.Context, req CompactRequest) (CompactResult, error) {
	if req.Turn <= 0 {
		req.Turn = 1
	}
	messages := cloneMessages(req.History)
	estimated := contextmgr.EstimateRequest(a.withRuntimeContext(messages), nil, a.model)
	a.emit(Event{
		Type:         EventActivity,
		ActivityKind: ActivityNotice,
		Notice:       NoticeCompacting,
		Status:       "running",
		Summary:      "compacting context",
	})
	compacted, ok, err := a.compactMessages(ctx, req.SessionID, req.Turn, messages, estimated, nil)
	if err != nil {
		a.emit(Event{
			Type:         EventActivity,
			ActivityKind: ActivityNotice,
			Notice:       NoticeCompacting,
			Status:       "failed",
			Summary:      fmt.Sprintf("compacting context failed: %v", err),
		})
		return CompactResult{}, a.recordError(ctx, req.SessionID, req.Turn, err)
	}
	if !ok {
		a.emitContextUsage(estimated, false)
		reason := "nothing to compact yet"
		a.emit(Event{
			Type:         EventActivity,
			ActivityKind: ActivityNotice,
			Notice:       NoticeCompacting,
			Status:       "done",
			Summary:      reason,
		})
		a.emit(Event{Type: EventDone, Text: reason})
		return CompactResult{
			Messages:        messages,
			EstimatedTokens: estimated,
			Noop:            true,
			Reason:          reason,
		}, nil
	}
	a.emit(Event{
		Type:         EventActivity,
		ActivityKind: ActivityNotice,
		Notice:       NoticeCompacting,
		Status:       "done",
		Summary:      fmt.Sprintf("compacted %d earlier messages", compacted.compactedMessages),
	})
	a.emitContextUsage(compacted.estimatedTokens, true)
	a.emit(Event{Type: EventDone, Text: compacted.summary})
	return CompactResult{
		Messages:          compacted.messages,
		Summary:           compacted.summary,
		CompactedMessages: compacted.compactedMessages,
		KeptMessages:      compacted.keptMessages,
		EstimatedTokens:   compacted.estimatedTokens,
	}, nil
}

type compactedMessages struct {
	messages          []message.Message
	summary           string
	compactedMessages int
	keptMessages      int
	estimatedTokens   int
}

type contextOverflowRecovery struct {
	messages  []message.Message
	recovered bool
}

func (a *Agent) recoverContextOverflow(ctx context.Context, sessionID string, turn int, messages []message.Message, estimated int, tools []provider.ToolDefinition, providerErr error, alreadyRecovered bool) (contextOverflowRecovery, error) {
	if alreadyRecovered || !isContextOverflowError(providerErr) {
		return contextOverflowRecovery{}, nil
	}
	a.emit(Event{
		Type:         EventActivity,
		ActivityKind: ActivityNotice,
		Notice:       NoticeCompacting,
		Status:       "running",
		Summary:      "provider context limit exceeded; compacting and retrying",
	})
	compacted, ok, err := a.compactMessages(ctx, sessionID, turn, messages, estimated, tools)
	if err != nil {
		return contextOverflowRecovery{}, fmt.Errorf("context overflow compact failed after provider error: %w", err)
	}
	if !ok {
		a.emit(Event{
			Type:         EventActivity,
			ActivityKind: ActivityNotice,
			Notice:       NoticeCompacting,
			Status:       "done",
			Summary:      "provider context limit exceeded; no earlier messages to compact",
		})
		return contextOverflowRecovery{}, nil
	}
	a.emit(Event{
		Type:         EventActivity,
		ActivityKind: ActivityNotice,
		Notice:       NoticeCompacting,
		Status:       "done",
		Summary:      fmt.Sprintf("provider context limit exceeded; compacted %d earlier messages and retrying", compacted.compactedMessages),
	})
	a.emitContextUsage(compacted.estimatedTokens, true)
	return contextOverflowRecovery{messages: compacted.messages, recovered: true}, nil
}

func (a *Agent) compactMessages(ctx context.Context, sessionID string, turn int, messages []message.Message, estimated int, tools []provider.ToolDefinition) (compactedMessages, bool, error) {
	prefix, suffix, ok := splitSummaryWindow(messages, summaryWindowOptions{
		KeepRecentTurns: effectiveKeepRecentTurns(a.contextCfg),
		MaxContext:      a.effectiveMaxContextTokens(),
		Model:           a.model,
	})
	if !ok {
		return compactedMessages{}, false, nil
	}
	summary, err := a.generateSummary(ctx, prefix)
	if err != nil {
		return compactedMessages{}, false, err
	}
	compacted := append([]message.Message{rollout.SummaryMessage(summary)}, suffix...)
	if err := a.append(ctx, sessionID, func() (rollout.Event, error) {
		return rollout.SummaryWithMessages(sessionID, turn, summary, compacted, len(prefix), len(suffix), estimated)
	}); err != nil {
		return compactedMessages{}, false, err
	}
	return compactedMessages{
		messages:          compacted,
		summary:           summary,
		compactedMessages: len(prefix),
		keptMessages:      len(suffix),
		estimatedTokens:   contextmgr.EstimateRequest(a.withRuntimeContext(compacted), tools, a.model),
	}, true, nil
}

func (a *Agent) shouldSummarize(estimated int) bool {
	if estimated <= 0 {
		return false
	}
	maxContext := a.effectiveMaxContextTokens()
	if maxContext <= 0 {
		return false
	}
	reserve := tooloutput.ReserveOutputTokens(a.contextCfg)
	return float64(estimated+reserve)/float64(maxContext) > effectiveTriggerRatio(a.contextCfg)
}

func (a *Agent) generateSummary(ctx context.Context, messages []message.Message) (string, error) {
	p := a.summaryProvider
	if p == nil {
		p = a.provider
	}
	model := strings.TrimSpace(a.summaryModel)
	if model == "" {
		model = a.model
	}
	return a.generateSummaryMessages(ctx, p, model, messages, a.summaryInputBudget(p, model), 0)
}

func (a *Agent) generateSummaryMessages(ctx context.Context, p provider.Provider, model string, messages []message.Message, budget, depth int) (string, error) {
	chunks, err := splitSummaryMessageChunks(a.summaryPromptTemplate(), messages, model, budget)
	if err != nil {
		return "", err
	}
	if len(chunks) == 0 {
		return "", errors.New("summary provider returned empty summary")
	}
	if len(chunks) == 1 {
		summary, err := a.requestSummary(ctx, p, model, renderMessages(chunks[0]))
		if err == nil || !isContextOverflowError(err) {
			return summary, err
		}
		return "", fmt.Errorf("summary input for one user turn exceeds the summary model context; configure a longer-context summary model or reduce large inputs/tool results: %w", err)
	}

	summaries := make([]string, 0, len(chunks))
	for i, chunk := range chunks {
		summary, err := a.generateSummaryMessages(ctx, p, model, chunk, budget, depth+1)
		if err != nil {
			return "", err
		}
		summaries = append(summaries, fmt.Sprintf("Chunk %d of %d:\n%s", i+1, len(chunks), summary))
	}
	if depth >= summaryMergeMaxDepth {
		return strings.Join(summaries, "\n\n"), nil
	}
	return a.generateSummaryUnits(ctx, p, model, summaries, budget, depth+1)
}

func (a *Agent) generateSummaryUnits(ctx context.Context, p provider.Provider, model string, units []string, budget, depth int) (string, error) {
	chunks, err := splitSummaryTextUnits(a.summaryPromptTemplate(), units, model, budget)
	if err != nil {
		return "", err
	}
	if len(chunks) == 0 {
		return "", errors.New("summary provider returned empty summary")
	}
	if len(chunks) == 1 {
		summary, err := a.requestSummary(ctx, p, model, strings.Join(chunks[0], "\n\n"))
		if err == nil || !isContextOverflowError(err) {
			return summary, err
		}
		return "", fmt.Errorf("summary merge input exceeds the summary model context; configure a longer-context summary model: %w", err)
	}
	merged := make([]string, 0, len(chunks))
	for i, chunk := range chunks {
		summary, err := a.generateSummaryUnits(ctx, p, model, chunk, budget, depth+1)
		if err != nil {
			return "", err
		}
		merged = append(merged, fmt.Sprintf("Merged chunk %d of %d:\n%s", i+1, len(chunks), summary))
	}
	if depth >= summaryMergeMaxDepth {
		return strings.Join(merged, "\n\n"), nil
	}
	return a.generateSummaryUnits(ctx, p, model, merged, budget, depth+1)
}

func (a *Agent) requestSummary(ctx context.Context, p provider.Provider, model, conversation string) (string, error) {
	prompt := strings.ReplaceAll(a.summaryPromptTemplate(), "{{conversation}}", conversation)
	request := []message.Message{message.Text(message.RoleUser, prompt)}
	estimated := contextmgr.Estimate(request, model)
	stream, err := p.Chat(ctx, provider.Request{
		Model:    model,
		Messages: request,
	})
	if err != nil {
		return "", fmt.Errorf("summary provider chat: %w", err)
	}
	defer stream.Close()

	var summary strings.Builder
	for {
		event, err := stream.Next(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", fmt.Errorf("summary provider stream: %w", err)
		}
		switch event.Type {
		case provider.EventTextDelta:
			summary.WriteString(event.Text)
		case provider.EventReasoningDelta:
			continue
		case provider.EventUsage:
			if event.Usage != nil {
				observeInputUsage(model, estimated, event.Usage.InputTokens)
			}
		case provider.EventDone:
			goto done
		case provider.EventError:
			if event.Err != nil {
				return "", fmt.Errorf("summary provider returned error: %w", event.Err)
			}
			return "", errors.New("summary provider returned error event")
		case provider.EventToolCall:
			return "", errors.New("summary provider returned unsupported tool call")
		default:
			return "", fmt.Errorf("summary provider returned unsupported event type %q", event.Type)
		}
	}
done:
	text := strings.TrimSpace(summary.String())
	if text == "" {
		return "", errors.New("summary provider returned empty summary")
	}
	return text, nil
}

func (a *Agent) summaryPromptTemplate() string {
	if strings.TrimSpace(a.promptCfg.CompactStyle) == config.CompactStyleShort {
		return summaryShortPromptTemplate
	}
	return summaryPromptTemplate
}

func observeInputUsage(model string, estimated, actual int) {
	if estimated <= 0 || actual <= 0 {
		return
	}
	contextmgr.ObserveUsage(model, estimated, actual)
}

func (a *Agent) emitContextUsage(used int, reset bool) {
	if used <= 0 {
		return
	}
	maxContext := a.effectiveMaxContextTokens()
	ratio := 0.0
	if maxContext > 0 {
		ratio = float64(used) / float64(maxContext)
	}
	a.emit(Event{
		Type:              EventContext,
		ContextUsedTokens: used,
		ContextMaxTokens:  maxContext,
		ContextRatio:      ratio,
		ContextReset:      reset,
		ContextKind:       "est",
	})
}

func (a *Agent) emitActualContextUsage(used int) {
	if used <= 0 {
		return
	}
	maxContext := a.effectiveMaxContextTokens()
	ratio := 0.0
	if maxContext > 0 {
		ratio = float64(used) / float64(maxContext)
	}
	a.emit(Event{
		Type:              EventContext,
		ContextUsedTokens: used,
		ContextMaxTokens:  maxContext,
		ContextRatio:      ratio,
		ContextKind:       "last",
	})
}

func (a *Agent) effectiveMaxContextTokens() int {
	if a.maxContextTokens > 0 {
		return a.maxContextTokens
	}
	return provider.CapsForModel(a.provider, a.model).MaxContextTokens
}

type summaryWindowOptions struct {
	KeepRecentTurns int
	MaxContext      int
	Model           string
}

func splitSummaryWindow(messages []message.Message, opts summaryWindowOptions) ([]message.Message, []message.Message, bool) {
	keepRecentTurns := effectiveKeepRecentTurns(config.ContextConfig{KeepRecentTurns: opts.KeepRecentTurns})
	turns := userTurnWindows(messages)
	if len(turns) <= 1 {
		return nil, nil, false
	}
	budget := summaryRecentBudget(opts.MaxContext)
	keepStart := len(turns) - 1
	for i := len(turns) - 2; i >= 0; i-- {
		if len(turns)-i > keepRecentTurns {
			break
		}
		candidate := cloneMessages(messages[turns[i].start:])
		if budget > 0 && contextmgr.Estimate(candidate, opts.Model) > budget {
			break
		}
		keepStart = i
	}
	cutoff := turns[keepStart].start
	if cutoff <= 0 {
		return nil, nil, false
	}
	prefix := cloneMessages(messages[:cutoff])
	suffix := cloneMessages(messages[cutoff:])
	if len(prefix) == 0 || len(suffix) == 0 {
		return nil, nil, false
	}
	return prefix, suffix, true
}

type turnWindow struct {
	start int
	end   int
}

func userTurnWindows(messages []message.Message) []turnWindow {
	var turns []turnWindow
	for i, msg := range messages {
		if msg.Role != message.RoleUser {
			continue
		}
		if len(turns) > 0 {
			turns[len(turns)-1].end = i
		}
		turns = append(turns, turnWindow{start: i, end: len(messages)})
	}
	if len(turns) > 0 {
		turns[len(turns)-1].end = len(messages)
	}
	return turns
}

func summaryRecentBudget(maxContext int) int {
	if maxContext <= 0 {
		return 32000
	}
	budget := int(float64(maxContext) * 0.15)
	if budget < 8000 {
		budget = 8000
	}
	if budget > 32000 {
		budget = 32000
	}
	return budget
}

func effectiveTriggerRatio(cfg config.ContextConfig) float64 {
	if cfg.TriggerRatio <= 0 {
		return defaultTriggerRatio
	}
	return cfg.TriggerRatio
}

func effectiveKeepRecentTurns(cfg config.ContextConfig) int {
	if cfg.KeepRecentTurns <= 0 {
		return defaultKeepRecentTurns
	}
	return cfg.KeepRecentTurns
}

func (a *Agent) summaryInputBudget(p provider.Provider, model string) int {
	maxContext := provider.CapsForModel(p, model).MaxContextTokens
	if maxContext <= 0 {
		return summaryFallbackBudget
	}
	reserve := tooloutput.ReserveOutputTokens(a.contextCfg)
	if reserve <= 0 {
		reserve = 4096
	}
	if reserve > maxContext/2 {
		reserve = maxContext / 4
	}
	budget := int(float64(maxContext-reserve) * 0.8)
	if budget <= 0 {
		return maxContext
	}
	if budget > maxContext {
		return maxContext
	}
	return budget
}

func splitSummaryMessageChunks(template string, messages []message.Message, model string, budget int) ([][]message.Message, error) {
	units := summaryMessageUnits(messages)
	if len(units) == 0 {
		return nil, nil
	}
	if budget <= 0 {
		budget = summaryFallbackBudget
	}
	emptyPrompt := summaryPromptEstimate(template, "", model)
	if budget <= emptyPrompt {
		return nil, fmt.Errorf("summary input budget (%d tokens) cannot fit summary prompt overhead (%d tokens)", budget, emptyPrompt)
	}

	chunks := make([][]message.Message, 0, 2)
	var current []message.Message
	for _, unit := range units {
		if len(unit) == 0 {
			continue
		}
		if summaryPromptEstimate(template, renderMessages(unit), model) > budget {
			return nil, fmt.Errorf("single user turn exceeds summary input budget (%d tokens)", budget)
		}
		candidate := append(cloneMessages(current), cloneMessages(unit)...)
		if len(current) > 0 && summaryPromptEstimate(template, renderMessages(candidate), model) > budget {
			chunks = append(chunks, current)
			current = cloneMessages(unit)
			continue
		}
		current = candidate
	}
	if len(current) > 0 {
		chunks = append(chunks, current)
	}
	return chunks, nil
}

func summaryMessageUnits(messages []message.Message) [][]message.Message {
	if len(messages) == 0 {
		return nil
	}
	turns := userTurnWindows(messages)
	if len(turns) == 0 {
		return [][]message.Message{cloneMessages(messages)}
	}
	units := make([][]message.Message, 0, len(turns)+1)
	if turns[0].start > 0 {
		units = append(units, cloneMessages(messages[:turns[0].start]))
	}
	for _, turn := range turns {
		units = append(units, cloneMessages(messages[turn.start:turn.end]))
	}
	return units
}

func splitSummaryTextUnits(template string, units []string, model string, budget int) ([][]string, error) {
	if len(units) == 0 {
		return nil, nil
	}
	if budget <= 0 {
		budget = summaryFallbackBudget
	}
	emptyPrompt := summaryPromptEstimate(template, "", model)
	if budget <= emptyPrompt {
		return nil, fmt.Errorf("summary input budget (%d tokens) cannot fit summary prompt overhead (%d tokens)", budget, emptyPrompt)
	}
	chunks := make([][]string, 0, 2)
	var current []string
	for _, unit := range units {
		unit = strings.TrimSpace(unit)
		if unit == "" {
			continue
		}
		if summaryPromptEstimate(template, unit, model) > budget {
			return nil, fmt.Errorf("single summary chunk exceeds summary input budget (%d tokens)", budget)
		}
		candidate := append(append([]string(nil), current...), unit)
		if len(current) > 0 && summaryPromptEstimate(template, strings.Join(candidate, "\n\n"), model) > budget {
			chunks = append(chunks, current)
			current = []string{unit}
			continue
		}
		current = candidate
	}
	if len(current) > 0 {
		chunks = append(chunks, current)
	}
	return chunks, nil
}

func summaryPromptEstimate(template, conversation, model string) int {
	prompt := strings.ReplaceAll(template, "{{conversation}}", conversation)
	return contextmgr.Estimate([]message.Message{message.Text(message.RoleUser, prompt)}, model)
}

func isContextOverflowError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	text := strings.ToLower(err.Error())
	text = strings.NewReplacer("_", " ", "-", " ").Replace(text)
	text = strings.Join(strings.Fields(text), " ")
	if text == "" {
		return false
	}
	if strings.Contains(text, "context canceled") || strings.Contains(text, "context deadline exceeded") {
		return false
	}
	if strings.Contains(text, "max output tokens") || strings.Contains(text, "maximum output tokens") {
		return false
	}
	if strings.Contains(text, "context") {
		for _, marker := range []string{"exceed", "too long", "too large", "maximum", "limit", "length"} {
			if strings.Contains(text, marker) {
				return true
			}
		}
	}
	for _, marker := range []string{
		"prompt is too long",
		"input is too long",
		"too many input tokens",
		"input tokens exceed",
		"input tokens exceeded",
		"input tokens too large",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	if strings.Contains(text, "too many tokens") && !strings.Contains(text, "output") && !strings.Contains(text, "completion") {
		return true
	}
	return false
}

func renderMessages(messages []message.Message) string {
	var b strings.Builder
	for i, msg := range messages {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "Message %d (%s):\n", i+1, msg.Role)
		for _, block := range msg.Content {
			b.WriteString(renderBlock(block))
			b.WriteByte('\n')
		}
	}
	return strings.TrimSpace(b.String())
}

func renderBlock(block message.ContentBlock) string {
	switch block.Type {
	case message.BlockText:
		return block.Text
	case message.BlockImage:
		return "[image] " + block.ImageURL
	case message.BlockReasoning:
		return "[reasoning omitted]"
	case message.BlockToolUse:
		return "[tool_use " + block.ToolName + "] " + compactRaw(block.Input)
	case message.BlockToolResult:
		status := "ok"
		if block.IsError {
			status = "error"
		}
		return "[tool_result " + block.ToolUseID + " " + status + "] " + block.Output
	default:
		return "[" + string(block.Type) + "]"
	}
}

func compactRaw(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	out, err := json.Marshal(v)
	if err != nil {
		return string(raw)
	}
	return string(out)
}
