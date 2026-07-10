package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/feimingxliu/ub/internal/pkg/core/message"
	"github.com/feimingxliu/ub/internal/pkg/llm/provider"
	"github.com/feimingxliu/ub/internal/pkg/workspace/rollout"
)

func (a *Agent) consumeStream(ctx context.Context, sessionID string, turn int, stream provider.Stream, estimatedTokens int) (streamResult, error) {
	var text strings.Builder
	var reasoningText strings.Builder
	var reasoningSignature strings.Builder
	var blocks []message.ContentBlock
	var calls []toolCall
	acceptedInputTokens := 0
	for {
		event, err := stream.Next(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			slog.Warn("stream error", "session", sessionID, "turn", turn, "err", err)
			return streamResult{}, err
		}
		switch event.Type {
		case provider.EventTextDelta:
			text.WriteString(event.Text)
			a.emit(Event{Type: EventDeltaText, Text: event.Text})
		case provider.EventReasoningDelta:
			chunk := event.Reasoning
			// Signature-only events carry the Anthropic reasoning signature
			// for replay on the next turn; no visible text to display.
			if event.ReasoningSignature != "" {
				reasoningSignature.WriteString(event.ReasoningSignature)
			}
			if chunk == "" {
				chunk = event.Text
			}
			if chunk == "" {
				continue
			}
			// Live-emit each delta so the TUI can stream thinking,
			// but accumulate text and persist a single rollout row at end-of-stream
			// — otherwise long reasoning bursts can produce hundreds of activity
			// rows per turn and bloat the rollout database.
			// Whitespace-only chunks (e.g. paragraph-break "\n\n" deltas from
			// Anthropic / OpenAI reasoning streams) must NOT be dropped here:
			// they are the only signal that separates paragraphs, and without
			// them the live TUI renders the entire thought chain as one blob.
			reasoningText.WriteString(chunk)
			_, _ = a.emitThinkingActivity(reasoningSummary(chunk, ""), reasoningDetail(chunk, ""))
		case provider.EventToolCall:
			call := toolCall{
				ID:    strings.TrimSpace(event.ToolUseID),
				Name:  event.ToolName,
				Input: cloneRaw(event.Input),
			}
			if call.ID == "" {
				call.ID = rollout.NewID("tool")
			}
			calls = append(calls, call)
			blocks = append(blocks, message.ToolUseBlock(call.ID, call.Name, call.Input))
			a.emitToolActivity(call, "queued", SummarizeToolInput(call.Name, call.Input), ToolInputDetail(call.Name, call.Input), false)
		case provider.EventUsage:
			if event.Usage != nil {
				observeInputUsage(a.model, estimatedTokens, event.Usage.InputTokens)
				if event.Usage.InputTokens > acceptedInputTokens {
					acceptedInputTokens = event.Usage.InputTokens
				}
				a.emitActualContextUsage(event.Usage.InputTokens)
				if err := a.append(ctx, sessionID, func() (rollout.Event, error) {
					return rollout.UsageWithDetails(sessionID, turn, usagePayload(event.Usage))
				}); err != nil {
					return streamResult{}, err
				}
			}
		case provider.EventDone:
			goto done
		case provider.EventError:
			if event.Err != nil {
				return streamResult{}, event.Err
			}
			return streamResult{}, errors.New("provider returned error event")
		default:
			return streamResult{}, fmt.Errorf("provider returned unsupported event type %q", event.Type)
		}
	}
done:
	// A usage event can precede a provider error. Only teach the context-window
	// resolver from requests that reached a successful done/EOF boundary.
	a.observeContextWindowUsage(acceptedInputTokens)
	if err := a.persistAccumulatedThinking(ctx, sessionID, turn, reasoningText.String()); err != nil {
		return streamResult{}, err
	}
	reasoning := reasoningText.String()
	sig := reasoningSignature.String()
	var prefix []message.ContentBlock
	if reasoning != "" && sig != "" {
		prefix = append(prefix, message.ReasoningBlock(reasoning, sig))
	}
	if text.Len() > 0 {
		prefix = append(prefix, message.TextBlock(text.String()))
	}
	if len(prefix) > 0 {
		blocks = append(prefix, blocks...)
	}
	return streamResult{
		text:         text.String(),
		message:      message.New(message.RoleAssistant, blocks...),
		toolCalls:    calls,
		reasoningLen: reasoningText.Len(),
	}, nil
}

// persistAccumulatedThinking writes a single rollout activity row capturing the
// full reasoning chain for the turn. Called once at end-of-stream; see the
// EventReasoningDelta case for the rationale.
func (a *Agent) persistAccumulatedThinking(ctx context.Context, sessionID string, turn int, full string) error {
	if strings.TrimSpace(full) == "" {
		return nil
	}
	activity := Event{
		Type:         EventActivity,
		ActivityKind: ActivityThinking,
		Summary:      reasoningSummary(full, ""),
		Content:      truncateActivityDetail(reasoningDetail(full, "")),
	}
	return a.append(ctx, sessionID, func() (rollout.Event, error) {
		return rollout.Activity(sessionID, turn, rolloutActivityPayload(activity))
	})
}

func usagePayload(usage *provider.Usage) rollout.UsagePayload {
	if usage == nil {
		return rollout.UsagePayload{}
	}
	return rollout.UsagePayload{
		InputTokens:      usage.InputTokens,
		OutputTokens:     usage.OutputTokens,
		ReasoningTokens:  usage.ReasoningTokens,
		CacheReadTokens:  usage.CacheReadTokens,
		CacheWriteTokens: usage.CacheWriteTokens,
	}
}
