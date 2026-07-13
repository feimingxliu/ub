package command

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/feimingxliu/ub/internal/agent"
	"github.com/feimingxliu/ub/internal/message"
	"github.com/feimingxliu/ub/internal/rollout"
	"github.com/feimingxliu/ub/internal/tool"
	"github.com/feimingxliu/ub/internal/tui"
)

func (r *tuiAgentRunner) messagesForCurrentSession() ([]tui.InitialMessage, error) {
	if r == nil || r.state == nil || r.state.rollout == nil || strings.TrimSpace(r.state.sessionID) == "" {
		return nil, fmt.Errorf("current session rollout is unavailable")
	}
	ctx := context.Background()
	if r.cmd != nil && r.cmd.Context() != nil {
		ctx = r.cmd.Context()
	}
	return messagesForTUIFromRollout(ctx, r.state.rollout, r.state.sessionID)
}

func messagesForTUIFromRollout(ctx context.Context, reader rollout.Reader, sessionID string) ([]tui.InitialMessage, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if reader == nil {
		return nil, fmt.Errorf("rollout reader is nil")
	}
	var out []tui.InitialMessage
	toolUses := map[string]message.ContentBlock{}
	if err := reader.ForEach(ctx, sessionID, func(event rollout.Event) error {
		if activity, ok, err := rollout.ActivityFromEvent(event); err != nil {
			return err
		} else if ok {
			out = append(out, activityMessageForTUI(activity, event.Turn))
			return nil
		}

		if event.Type == rollout.TypeToolResult {
			var payload rollout.ToolResultPayload
			if err := json.Unmarshal(event.Payload, &payload); err != nil {
				return fmt.Errorf("decode rollout tool_result event %s: %w", event.ID, err)
			}
			out = appendToolResultForTUI(out, toolUses, event.Turn, payload.ToolUseID, payload.ToolName, tool.Result{
				Content:        payload.Output,
				IsError:        payload.IsError,
				Files:          payload.Files,
				Truncated:      payload.Truncated,
				OriginalBytes:  payload.OriginalBytes,
				FullOutputPath: payload.FullOutputPath,
			})
			return nil
		}

		if event.Type == rollout.TypeSummary {
			return nil
		}
		msg, ok, err := rollout.MessageFromEvent(event)
		if err != nil {
			return err
		}
		if ok {
			source := rollout.MessageSourceFromEvent(event)
			out = appendMessagesForTUI(out, toolUses, msg, event.Turn, source == "auto")
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return out, nil
}

func messagesForTUI(history []message.Message) []tui.InitialMessage {
	out := make([]tui.InitialMessage, 0, len(history))
	toolUses := map[string]message.ContentBlock{}
	// In-memory history has no per-message turn; approximate by counting user
	// messages. Resume normally uses the rollout-backed path which carries
	// the real Turn; this is only the fallback when the rollout is missing.
	turn := 0
	for _, msg := range history {
		if msg.Role == message.RoleUser {
			turn++
		}
		out = appendMessagesForTUI(out, toolUses, msg, turn)
	}
	return out
}

func appendMessagesForTUI(out []tui.InitialMessage, toolUses map[string]message.ContentBlock, msg message.Message, turn int, autoTriggered ...bool) []tui.InitialMessage {
	text := strings.TrimSpace(msg.Text())
	if text != "" {
		out = append(out, tui.InitialMessage{
			Role:          string(msg.Role),
			Turn:          turn,
			Text:          text,
			AutoTriggered: len(autoTriggered) > 0 && autoTriggered[0],
		})
	}
	for _, block := range msg.Content {
		switch block.Type {
		case message.BlockToolUse:
			if strings.TrimSpace(block.ToolUseID) == "" {
				continue
			}
			toolUses[block.ToolUseID] = block
			out = append(out, tui.InitialMessage{
				Turn:         turn,
				ActivityKind: "tool",
				ToolUseID:    block.ToolUseID,
				ToolName:     block.ToolName,
				Status:       "queued",
				Summary:      agent.SummarizeToolInput(block.ToolName, block.Input),
			})
		case message.BlockToolResult:
			if strings.TrimSpace(block.ToolUseID) == "" {
				continue
			}
			out = appendToolResultForTUI(out, toolUses, turn, block.ToolUseID, "", tool.Result{
				Content: block.Output,
				IsError: block.IsError,
			})
		}
	}
	return out
}

func appendToolResultForTUI(out []tui.InitialMessage, toolUses map[string]message.ContentBlock, turn int, toolUseID, toolName string, result tool.Result) []tui.InitialMessage {
	toolUse := toolUses[toolUseID]
	toolName = fallbackString(toolName, toolUse.ToolName)
	if strings.TrimSpace(toolName) == "" {
		toolName = "tool"
	}
	status := "done"
	if result.IsError {
		status = "failed"
	}
	summary, detail := agent.ToolActivityResultWithInput(toolName, toolUse.Input, result)
	return append(out, tui.InitialMessage{
		Turn:         turn,
		ActivityKind: "tool",
		ToolUseID:    toolUseID,
		ToolName:     toolName,
		Status:       status,
		Summary:      summary,
		Content:      detail,
		IsError:      result.IsError,
	})
}

func activityMessageForTUI(activity rollout.ActivityPayload, turn int) tui.InitialMessage {
	return tui.InitialMessage{
		Turn:            turn,
		ActivityKind:    activity.ActivityKind,
		ToolUseID:       activity.ToolUseID,
		ToolName:        activity.ToolName,
		ParentToolUseID: activity.ParentToolUseID,
		SubagentID:      activity.SubagentID,
		Status:          activity.Status,
		Summary:         activity.Summary,
		Content:         activity.Content,
		Decision:        activity.Decision,
		Source:          activity.Source,
		Reason:          activity.Reason,
		Allowed:         activity.Allowed,
		IsError:         activity.IsError,
	}
}

func fallbackString(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func convertAgentEvent(event agent.Event) tui.Event {
	switch event.Type {
	case agent.EventDeltaText:
		return tui.Event{Type: tui.EventDeltaText, Text: event.Text}
	case agent.EventActivity:
		return tui.Event{
			Type:            tui.EventActivity,
			ToolUseID:       event.ToolUseID,
			ToolName:        event.ToolName,
			ParentToolUseID: event.ParentToolUseID,
			SubagentID:      event.SubagentID,
			Content:         event.Content,
			ActivityKind:    string(event.ActivityKind),
			Status:          event.Status,
			Summary:         event.Summary,
			Notice:          string(event.Notice),
			Decision:        event.Decision,
			Source:          event.Source,
			Reason:          event.Reason,
			Allowed:         event.Allowed,
			IsError:         event.IsError,
		}
	case agent.EventContext:
		return tui.Event{
			Type:              tui.EventContext,
			ContextUsedTokens: event.ContextUsedTokens,
			ContextMaxTokens:  event.ContextMaxTokens,
			ContextRatio:      event.ContextRatio,
			ContextReset:      event.ContextReset,
			ContextKind:       event.ContextKind,
			ContextMaxSource:  event.ContextMaxSource,
			ContextConfidence: event.ContextConfidence,
		}
	case agent.EventToolPartialOutput:
		return tui.Event{
			Type:            tui.EventToolPartialOutput,
			ToolUseID:       event.ToolUseID,
			ToolName:        event.ToolName,
			ParentToolUseID: event.ParentToolUseID,
			SubagentID:      event.SubagentID,
			Status:          event.Status,
			Summary:         event.Summary,
			Content:         event.Content,
			IsError:         event.IsError,
		}
	case agent.EventToolCallStart:
		return tui.Event{Type: tui.EventToolCallStart, ToolName: event.ToolName}
	case agent.EventToolCallEnd:
		return tui.Event{Type: tui.EventToolCallEnd, ToolName: event.ToolName, Content: event.Content, IsError: event.IsError}
	case agent.EventPermission:
		return tui.Event{
			Type:     tui.EventPermission,
			ToolName: event.ToolName,
			Decision: event.Decision,
			Source:   event.Source,
			Reason:   event.Reason,
			Allowed:  event.Allowed,
		}
	case agent.EventDone:
		return tui.Event{Type: tui.EventDone, Text: event.Text}
	case agent.EventError:
		return tui.Event{Type: tui.EventError, Content: event.Content, IsError: true, Err: event.Err}
	default:
		return tui.Event{Type: tui.EventError, Content: fmt.Sprintf("unknown agent event %q", event.Type), IsError: true}
	}
}

func sendTUIEvent(ctx context.Context, events chan<- tui.Event, event tui.Event) {
	defer func() {
		// Background post-turn work can outlive the TUI event consumer. Sending
		// to a closed channel must never take the whole terminal down.
		_ = recover()
	}()
	select {
	case events <- event:
	case <-ctx.Done():
	}
}
