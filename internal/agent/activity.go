package agent

import (
	"fmt"
	"strings"
	"time"

	"github.com/feimingxliu/ub/internal/hook"
	"github.com/feimingxliu/ub/internal/rollout"
)

const (
	maxActivitySummaryRunes    = 180
	maxActivityDetailRunes     = 4000
	maxToolActivityDetailRunes = 12000
	shellMetadataOpenTag       = "<shell_metadata>\n"
	shellMetadataCloseTag      = "</shell_metadata>"
	toolResultTruncatedMarker  = "... [tool result truncated:"
)

// emitThinkingActivity emits a thinking (reasoning) activity event to the
// EventSink. It returns the emitted event and true if non-empty. See the
// inline comment below for why whitespace-only input is not suppressed.
func (a *Agent) emitThinkingActivity(summary, detail string) (Event, bool) {
	// Use raw equality rather than TrimSpace so paragraph-break deltas
	// ("\n\n") still emit — they carry the only signal the TUI has to insert
	// a paragraph break into the streamed thinking block.
	if summary == "" && detail == "" {
		return Event{}, false
	}
	event := Event{
		Type:         EventActivity,
		ActivityKind: ActivityThinking,
		Summary:      truncateActivitySummary(summary),
		Content:      truncateActivityDetail(detail),
	}
	a.emit(event)
	return event, true
}

// emitToolActivity emits a tool activity event with the given status
// (queued/running/done/failed/blocked), summary, detail content, and error
// flag. It is called at each phase of tool execution lifecycle.
func (a *Agent) emitToolActivity(call toolCall, status, summary, content string, isError bool) {
	a.emit(Event{
		Type:         EventActivity,
		ActivityKind: ActivityTool,
		ToolUseID:    call.ID,
		ToolName:     call.Name,
		Status:       status,
		Summary:      truncateActivitySummary(summary),
		Content:      truncateToolActivityDetail(content),
		IsError:      isError,
	})
}

// emitHookOutcomes turns one Decision into one Activity per hook outcome so
// the TUI and rollout can show "which hook ran, did it succeed, what did it
// say." Skipped (filtered-out) hooks produce no outcome here, so they're
// silently absent — matching the user's intent.
func (a *Agent) emitHookOutcomes(dec hook.Decision) {
	if len(dec.Outcomes) == 0 {
		return
	}
	for _, out := range dec.Outcomes {
		status := "done"
		summary := fmt.Sprintf("hook %s exit=%d in %s", strings.Join(out.Command, " "), out.ExitCode, out.Duration.Round(time.Millisecond))
		isError := false
		if out.Err != nil {
			status = "failed"
			isError = true
		} else if out.ExitCode != 0 {
			status = "failed"
			isError = true
		}
		if dec.Block {
			status = "blocked"
			isError = true
		}
		var content strings.Builder
		if out.Stdout != "" {
			content.WriteString("stdout:\n")
			content.WriteString(out.Stdout)
		}
		if out.Stderr != "" {
			if content.Len() > 0 {
				content.WriteString("\n")
			}
			content.WriteString("stderr:\n")
			content.WriteString(out.Stderr)
		}
		if out.Err != nil {
			if content.Len() > 0 {
				content.WriteString("\n")
			}
			content.WriteString("err: ")
			content.WriteString(out.Err.Error())
		}
		a.emit(Event{
			Type:         EventActivity,
			ActivityKind: ActivityHook,
			Status:       status,
			Summary:      truncateActivitySummary(summary),
			Content:      truncateActivityDetail(content.String()),
			IsError:      isError,
		})
	}
}

func (a *Agent) emitPermissionActivity(toolName, source, decision, reason string, allowed bool) Event {
	if strings.TrimSpace(decision) == "" {
		decision = "unknown"
	}
	event := Event{
		Type:         EventActivity,
		ActivityKind: ActivityPermission,
		ToolName:     toolName,
		Source:       source,
		Decision:     decision,
		Reason:       truncateActivitySummary(redactText("reason", reason)),
		Allowed:      allowed,
		IsError:      decision == "error",
	}
	a.emit(event)
	return event
}

func rolloutActivityPayload(event Event) rollout.ActivityPayload {
	return rollout.ActivityPayload{
		ActivityKind:    string(event.ActivityKind),
		ToolUseID:       event.ToolUseID,
		ToolName:        event.ToolName,
		ParentToolUseID: event.ParentToolUseID,
		SubagentID:      event.SubagentID,
		Status:          event.Status,
		Summary:         event.Summary,
		Content:         event.Content,
		Decision:        event.Decision,
		Source:          event.Source,
		Reason:          event.Reason,
		Allowed:         event.Allowed,
		IsError:         event.IsError,
	}
}
