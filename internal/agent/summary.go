package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	_ "embed"
	"github.com/feimingxliu/ub/internal/config"
	contextmgr "github.com/feimingxliu/ub/internal/context"
	"github.com/feimingxliu/ub/internal/message"
	"github.com/feimingxliu/ub/internal/provider"
	"github.com/feimingxliu/ub/internal/rollout"
)

const (
	defaultTriggerRatio    = 0.8
	defaultKeepRecentTurns = 3
)

//go:embed summary_prompt.txt
var summaryPromptTemplate string

type preparedMessages struct {
	messages        []message.Message
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

func (a *Agent) prepareMessages(ctx context.Context, sessionID string, turn int, messages []message.Message) (preparedMessages, error) {
	requestMessages := cloneMessages(messages)
	estimated := contextmgr.Estimate(requestMessages, a.model)
	if !a.shouldSummarize(estimated) {
		a.emitContextUsage(estimated, false)
		return preparedMessages{messages: requestMessages, estimatedTokens: estimated}, nil
	}
	compacted, ok, err := a.compactMessages(ctx, sessionID, turn, requestMessages, estimated)
	if err != nil {
		return preparedMessages{}, err
	}
	if !ok {
		a.emitContextUsage(estimated, false)
		return preparedMessages{messages: requestMessages, estimatedTokens: estimated}, nil
	}
	a.emit(Event{
		Type:         EventActivity,
		ActivityKind: ActivityNotice,
		Status:       "done",
		Summary:      fmt.Sprintf("summarized %d earlier messages", compacted.compactedMessages),
	})
	a.emitContextUsage(compacted.estimatedTokens, true)
	return preparedMessages{
		messages:        compacted.messages,
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
	estimated := contextmgr.Estimate(messages, a.model)
	compacted, ok, err := a.compactMessages(ctx, req.SessionID, req.Turn, messages, estimated)
	if err != nil {
		return CompactResult{}, a.recordError(ctx, req.SessionID, req.Turn, err)
	}
	if !ok {
		a.emitContextUsage(estimated, false)
		reason := "nothing to compact yet"
		a.emit(Event{
			Type:         EventActivity,
			ActivityKind: ActivityNotice,
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

func (a *Agent) compactMessages(ctx context.Context, sessionID string, turn int, messages []message.Message, estimated int) (compactedMessages, bool, error) {
	prefix, suffix, ok := splitSummaryWindow(messages, effectiveKeepRecentTurns(a.contextCfg))
	if !ok {
		return compactedMessages{}, false, nil
	}
	summary, err := a.generateSummary(ctx, prefix)
	if err != nil {
		return compactedMessages{}, false, err
	}
	compacted := append([]message.Message{rollout.SummaryMessage(summary)}, suffix...)
	if err := a.append(ctx, sessionID, func() (rollout.Event, error) {
		return rollout.Summary(sessionID, turn, summary, len(prefix), len(suffix), estimated)
	}); err != nil {
		return compactedMessages{}, false, err
	}
	return compactedMessages{
		messages:          compacted,
		summary:           summary,
		compactedMessages: len(prefix),
		keptMessages:      len(suffix),
		estimatedTokens:   contextmgr.Estimate(compacted, a.model),
	}, true, nil
}

func (a *Agent) shouldSummarize(estimated int) bool {
	if estimated <= 0 {
		return false
	}
	maxContext := a.provider.Caps().MaxContextTokens
	if maxContext <= 0 {
		return false
	}
	return float64(estimated)/float64(maxContext) > effectiveTriggerRatio(a.contextCfg)
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
	prompt := strings.ReplaceAll(summaryPromptTemplate, "{{conversation}}", renderMessages(messages))
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
	maxContext := a.provider.Caps().MaxContextTokens
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
	})
}

func splitSummaryWindow(messages []message.Message, keepRecentTurns int) ([]message.Message, []message.Message, bool) {
	keepRecentTurns = effectiveKeepRecentTurns(config.ContextConfig{KeepRecentTurns: keepRecentTurns})
	totalUserTurns := 0
	for _, msg := range messages {
		if msg.Role == message.RoleUser {
			totalUserTurns++
		}
	}
	if totalUserTurns <= keepRecentTurns {
		return nil, nil, false
	}
	seenUserTurns := 0
	cutoff := -1
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != message.RoleUser {
			continue
		}
		seenUserTurns++
		if seenUserTurns == keepRecentTurns {
			cutoff = i
			break
		}
	}
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
