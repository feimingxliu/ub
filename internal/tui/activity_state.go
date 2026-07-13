package tui

import (
	"fmt"
	"strings"
)

// toolPartialActivity converts a tool-partial-output event into a tool
// activity event with status "running". This lets the TUI render streaming
// stdout/stderr as an in-progress tool activity before the final result arrives.
func toolPartialActivity(event Event) Event {
	return Event{
		Type:            EventActivity,
		ActivityKind:    "tool",
		ToolUseID:       event.ToolUseID,
		ToolName:        event.ToolName,
		ParentToolUseID: event.ParentToolUseID,
		SubagentID:      event.SubagentID,
		Status:          "running",
		Summary:         event.Summary,
		Content:         event.Content,
		IsError:         event.IsError,
	}
}

// permissionEventText formats a one-line summary of a permission decision:
// "Permission <source> <decision> <tool>: <reason>". Decision defaults to
// allow/deny based on the Allowed flag when the Decision field is empty.
func permissionEventText(event Event) string {
	source := defaultString(event.Source, "permission")
	decision := defaultString(event.Decision, "")
	if decision == "" {
		if event.Allowed {
			decision = "allow"
		} else {
			decision = "deny"
		}
	}
	toolName := defaultString(event.ToolName, "tool")
	text := fmt.Sprintf("Permission %s %s %s", source, decision, toolName)
	if reason := strings.TrimSpace(event.Reason); reason != "" {
		text += ": " + reason
	}
	return text
}

// activityEventText produces a human-readable one-line description of an
// activity event, dispatching by ActivityKind. Subagent events are prefixed
// with "subagent: " so they are visually distinguishable in the transcript.
func activityEventText(event Event) string {
	prefix := subagentActivityPrefix(event)
	switch strings.TrimSpace(event.ActivityKind) {
	case "thinking":
		return prefix + "thinking: " + defaultString(event.Summary, event.Text)
	case "tool":
		return prefix + toolActivityText(event)
	case "permission":
		return prefix + permissionEventText(event)
	case "mode":
		return prefix + modeEventText(event)
	case "notice":
		return prefix + "notice: " + defaultString(event.Summary, event.Text)
	default:
		return prefix + defaultString(event.Summary, defaultString(event.Content, "activity"))
	}
}

// activityEventKey derives the stable deduplication key for an activity event.
// Tool and mode events are keyed by ToolUseID so updates replace the same
// block. Thinking events are keyed by subagent ID (or just "thinking" for
// the main agent). Notice events use their Notice kind as the key.
func activityEventKey(event Event) string {
	subagentID := strings.TrimSpace(event.SubagentID)
	switch strings.TrimSpace(event.ActivityKind) {
	case "tool":
		if strings.TrimSpace(event.ToolUseID) != "" {
			return "tool:" + event.ToolUseID
		}
	case "mode":
		if strings.TrimSpace(event.ToolUseID) != "" {
			return "mode:" + event.ToolUseID
		}
	case "thinking":
		if subagentID != "" {
			return "subagent:" + subagentID + ":thinking"
		}
		return "thinking"
	case "notice":
		if event.Notice == "compacting" {
			return "notice:compacting"
		}
		if event.Notice == "goal_inject" || event.Notice == "goal_status" || event.Notice == "goal_created" {
			return "notice:goal"
		}
	}
	return ""
}

func subagentActivityPrefix(event Event) string {
	if strings.TrimSpace(event.SubagentID) == "" {
		return ""
	}
	return "subagent: "
}

func thinkingActivityKey(runID int) string {
	return fmt.Sprintf("thinking:%d", runID)
}

func thinkingActivityGroupKey(runID int) string {
	return activityGroupKeyForName(runID, thinkingGroupName)
}

func toolActivityGroupKey(runID int) string {
	return activityGroupKeyForName(runID, toolGroupName)
}

func activityGroupKeyForName(runID int, groupName string) string {
	return fmt.Sprintf("activity:%s:%d", groupName, runID)
}

func activityGroupNameForEvent(event Event) string {
	switch strings.TrimSpace(event.ActivityKind) {
	case "thinking":
		return thinkingGroupName
	case "tool", "permission", "mode":
		return toolGroupName
	default:
		return ""
	}
}

func modeEventText(event Event) string {
	summary := defaultString(event.Summary, "Mode switch")
	decision := strings.TrimSpace(event.Decision)
	if decision == "" {
		if event.Allowed {
			decision = "approved"
		} else {
			decision = "denied"
		}
	}
	text := summary + " " + decision
	if line := firstNonEmptyLine(event.Content); line != "" {
		text += ": " + line
	}
	return text
}

func toolActivityText(event Event) string {
	name := strings.TrimSpace(event.ToolName)
	title := toolTitle(name, toolTitleSummary(event))
	switch toolEventStatus(event) {
	case "queued", "running":
		action := toolAction(name)
		if summary := strings.TrimSpace(event.Summary); summary != "" {
			return action + " " + summary
		}
		return action
	case "failed":
		return title + " failed"
	default:
		return title
	}
}

func toolTitleSummary(event Event) string {
	name := strings.TrimSpace(event.ToolName)
	if isPlanArtifactTool(name) {
		if planID := planIDFromToolResult(event.Content); planID != "" {
			return planID
		}
	}
	return event.Summary
}

func isPlanArtifactTool(name string) bool {
	switch strings.TrimSpace(name) {
	case "plan_write", "plan_update":
		return true
	default:
		return false
	}
}

func planIDFromToolResult(content string) string {
	for _, line := range strings.Split(content, "\n") {
		if id, ok := strings.CutPrefix(strings.TrimSpace(line), "plan_id="); ok {
			return strings.TrimSpace(id)
		}
	}
	return ""
}

// activityMessage converts an agent Event into a TUI message block for
// rendering. Each ActivityKind maps to a different messageKind (thinking,
// tool, permission, notice) with appropriate defaults for status, title,
// and detail. The key is derived from activityEventKey so repeated events
// for the same tool call update the same block instead of appending.
func activityMessage(event Event) message {
	switch strings.TrimSpace(event.ActivityKind) {
	case "thinking":
		summary := defaultString(event.Summary, event.Text)
		// Preserve whitespace-only Content (e.g. "\n\n" paragraph breaks);
		// defaultString would strip them via TrimSpace and we'd lose the
		// only signal for paragraph boundaries in streamed reasoning.
		detail := event.Content
		if detail == "" {
			detail = event.Text
		}
		if detail == "" {
			detail = summary
		}
		return message{
			role:      activityRole,
			text:      activityEventText(event),
			key:       activityEventKey(event),
			kind:      thinkingMessage,
			title:     subagentActivityPrefix(event) + "thinking: " + thinkingSummary(summary),
			status:    "running",
			detail:    detail,
			collapsed: true,
		}
	case "tool":
		text := subagentActivityPrefix(event) + toolActivityText(event)
		detail := event.Content
		if toolDetailUsesTodoStyle(event.ToolName) {
			detail = ""
		}
		return message{
			role:      activityRole,
			text:      text,
			key:       activityEventKey(event),
			kind:      toolMessage,
			title:     text,
			name:      defaultString(event.ToolName, "tool"),
			status:    defaultString(toolEventStatus(event), "done"),
			detail:    detail,
			collapsed: true,
		}
	case "permission":
		text := subagentActivityPrefix(event) + permissionEventText(event)
		return message{
			role:      activityRole,
			text:      text,
			key:       activityEventKey(event),
			kind:      permissionMessage,
			title:     text,
			name:      defaultString(event.ToolName, "tool"),
			status:    event.Decision,
			detail:    strings.TrimSpace(event.Reason),
			collapsed: true,
		}
	case "mode":
		text := subagentActivityPrefix(event) + modeEventText(event)
		return message{
			role:      activityRole,
			text:      text,
			key:       activityEventKey(event),
			kind:      noticeMessage,
			title:     text,
			name:      "mode",
			status:    defaultString(event.Decision, event.Status),
			detail:    strings.TrimSpace(event.Content),
			collapsed: true,
		}
	case "notice":
		text := activityEventText(event)
		key := activityEventKey(event)
		msg := message{role: activityRole, text: text, kind: noticeMessage, title: text}
		if key != "" {
			msg.key = key
			msg.status = defaultString(event.Status, "done")
		}
		return msg
	default:
		text := activityEventText(event)
		return message{role: activityRole, text: text, kind: noticeMessage, title: text}
	}
}

// todoMessageFromEvent extracts a todo view message from a todo tool event.
// Returns ok=false when the event is not from a todo tool or has no parseable
// items. The todo block is rendered as a separate non-collapsible view in the
// transcript (kind=todoMessage) with a summary title like "Todo: 2 running, 3 pending".
func todoMessageFromEvent(event Event) (message, bool) {
	if strings.TrimSpace(event.ActivityKind) != "tool" || !toolDetailUsesTodoStyle(event.ToolName) {
		return message{}, false
	}
	detail := strings.TrimRight(event.Content, " \t\r\n")
	items := parseTodoDetailItems(detail)
	if len(items) == 0 {
		return message{}, false
	}
	key := "todo"
	if sessionID := todoSessionID(detail); sessionID != "" {
		key += ":" + sessionID
	}
	status := todoStatus(items)
	title := todoTitle(items)
	return message{
		role:      activityRole,
		text:      title,
		key:       key,
		kind:      todoMessage,
		title:     title,
		status:    status,
		detail:    detail,
		collapsed: false,
	}, true
}

func todoSessionID(detail string) string {
	for _, line := range strings.Split(detail, "\n") {
		line = strings.TrimSpace(line)
		if value, ok := strings.CutPrefix(line, "session_id="); ok {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// todoStatus derives the overall status of a todo list from its items:
// running if any item is in_progress, queued if any are pending, failed if
// any failed (partial if some also completed), done otherwise.
func todoStatus(items []todoDetailItem) string {
	if len(items) == 0 {
		return "done"
	}
	pending, running, completed, skipped, failed := todoCounts(items)
	switch {
	case running > 0:
		return "running"
	case pending > 0:
		return "queued"
	case failed > 0 && completed+skipped > 0:
		return activityStatusPartialFailed
	case failed > 0:
		return "failed"
	default:
		return "done"
	}
}

// todoTitle builds a summary title like "Todo: 2 running, 3 pending, 1 done"
// from the item counts. Only non-zero counts are shown.
func todoTitle(items []todoDetailItem) string {
	pending, running, completed, skipped, failed := todoCounts(items)
	var parts []string
	for _, count := range []activityCount{
		{label: "running", value: running},
		{label: "pending", value: pending},
		{label: "done", value: completed},
		{label: "skipped", value: skipped},
		{label: "failed", value: failed},
	} {
		if count.value > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", count.value, count.label))
		}
	}
	if len(parts) == 0 {
		return "Todo"
	}
	return "Todo: " + strings.Join(parts, ", ")
}

func todoCounts(items []todoDetailItem) (pending, running, completed, skipped, failed int) {
	for _, item := range items {
		switch item.status {
		case "in_progress":
			running++
		case "completed":
			completed++
		case "skipped":
			skipped++
		case "failed":
			failed++
		default:
			pending++
		}
	}
	return pending, running, completed, skipped, failed
}

// toolStatusFromLegacyState converts old-style tool status strings (started/
// finished) used by early agent versions into the current status vocabulary
// (running/done).
func toolStatusFromLegacyState(state string) string {
	switch strings.TrimSpace(state) {
	case "started":
		return "running"
	case "finished":
		return "done"
	default:
		return state
	}
}

// activityStatusIcon maps a (messageKind, status) pair to a single-character
// icon for display in the transcript gutter: ✓ for done, × for failed,
// … for running, ! for partial failure, ? for pending permission, • for queued.
func activityStatusIcon(kind messageKind, status string) string {
	status = strings.ToLower(strings.TrimSpace(status))
	switch kind {
	case activityGroupMessage:
		switch status {
		case "failed", "error":
			return "×"
		case activityStatusPartialFailed:
			return "!"
		case "queued", "running", "started":
			return "…"
		default:
			return "✓"
		}
	case thinkingMessage:
		return "…"
	case permissionMessage:
		switch status {
		case "deny", "denied", "error":
			return "×"
		case "allow", "allowed":
			return "✓"
		default:
			return "?"
		}
	case todoMessage:
		switch status {
		case "queued":
			return "•"
		case "running", "started":
			return "…"
		case "failed", "error":
			return "×"
		case activityStatusPartialFailed:
			return "!"
		default:
			return "✓"
		}
	default:
		switch status {
		case "queued":
			return "•"
		case "running", "started":
			return "…"
		case "failed", "error":
			return "×"
		case "denied":
			return "×"
		default:
			return "✓"
		}
	}
}

func expandedDetail(item message) string {
	detail := strings.TrimSpace(item.detail)
	if detail == "" {
		return ""
	}
	if item.kind == toolMessage && !meaningfulToolDetail(detail, item) {
		return ""
	}
	return detail
}

func truncateExpandedDetailForDisplay(detail string) string {
	if detail == "" {
		return ""
	}
	runes := []rune(detail)
	if len(runes) <= maxExpandedDetailRunes {
		return detail
	}
	omitted := len(runes) - maxExpandedDetailRunes
	marker := fmt.Sprintf("\n[display truncated: %d earlier characters hidden; full output may be available in tool result details]", omitted)
	markerRunes := []rune(marker)
	budget := maxExpandedDetailRunes - len(markerRunes)
	if budget <= 0 {
		return string(runes[len(runes)-maxExpandedDetailRunes:])
	}
	return string(runes[:budget]) + marker
}

func meaningfulToolDetail(detail string, item message) bool {
	normalized := strings.ToLower(strings.Join(strings.Fields(detail), " "))
	switch normalized {
	case "", "completed", "complete", "done", "queued", "running", "started", "failed", "error", "status: done", "status: running", "status: queued", "status: failed", "status: error":
		return false
	}
	if normalized == strings.ToLower(strings.Join(strings.Fields(item.text), " ")) {
		return false
	}
	if normalized == strings.ToLower(strings.Join(strings.Fields(item.title), " ")) {
		return false
	}
	if item.status != "" && normalized == strings.ToLower(strings.Join(strings.Fields(item.status), " ")) {
		return false
	}
	return true
}

func toolEventStatus(event Event) string {
	status := strings.TrimSpace(event.Status)
	if status == "" && event.IsError {
		return "failed"
	}
	return status
}

func toolAction(name string) string {
	switch strings.TrimSpace(name) {
	case "read":
		return "Reading file..."
	case "ls":
		return "Listing directory..."
	case "grep":
		return "Searching content..."
	case "glob":
		return "Finding files..."
	case "write":
		return "Preparing write..."
	case "edit":
		return "Preparing edit..."
	case "multiedit":
		return "Preparing multi-edit..."
	case "apply_patch":
		return "Preparing patch..."
	case "bash":
		return "Writing command..."
	case "task":
		return "Running Task..."
	case "remember":
		return "Writing memory..."
	case "enter_plan_mode":
		return "Requesting plan execmode..."
	case "exit_plan_mode":
		return "Requesting plan approval..."
	case "plan_write":
		return "Writing plan..."
	case "plan_update":
		return "Updating plan..."
	case "plan_update_step":
		return "Updating plan step..."
	case "todo_write":
		return "Writing todos..."
	case "todo_update":
		return "Updating todos..."
	case "tool_result":
		return "Reading tool result..."
	case "diagnostics":
		return "Checking diagnostics..."
	case "references":
		return "Finding references..."
	case "hover":
		return "Reading hover..."
	case "completion":
		return "Getting completions..."
	case "document_symbols":
		return "Listing document symbols..."
	case "rename":
		return "Preparing rename..."
	case "code_action":
		return "Listing code actions..."
	case "job_run":
		return "Starting job..."
	case "job_output":
		return "Reading job output..."
	case "job_kill":
		return "Stopping job..."
	default:
		if display, ok := mcpToolDisplayName(name); ok {
			return "Calling " + display + "..."
		}
		if name := strings.TrimSpace(name); name != "" {
			return "Running " + name + "..."
		}
		return "Working..."
	}
}

func toolTitle(name, summary string) string {
	summary = strings.TrimSpace(summary)
	verb := "Tool"
	switch strings.TrimSpace(name) {
	case "read":
		verb = "Read"
	case "ls":
		verb = "Listed"
	case "grep":
		verb = "Searched"
	case "glob":
		verb = "Found"
	case "write":
		verb = "Wrote"
	case "edit":
		verb = "Edited"
	case "multiedit":
		verb = "Edited multiple files"
	case "apply_patch":
		verb = "Applied patch"
	case "bash":
		verb = "Ran"
	case "task":
		verb = "Ran Task"
	case "remember":
		verb = "Remembered"
	case "enter_plan_mode":
		verb = "Requested plan mode"
	case "exit_plan_mode":
		verb = "Requested plan approval"
	case "plan_write":
		verb = "Wrote plan"
	case "plan_update":
		verb = "Updated plan"
	case "plan_update_step":
		verb = "Updated plan step"
	case "todo_write":
		verb = "Wrote todos"
	case "todo_update":
		verb = "Updated todos"
	case "tool_result":
		verb = "Read tool result"
	case "diagnostics":
		verb = "Checked diagnostics"
	case "references":
		verb = "Found references"
	case "hover":
		verb = "Read hover"
	case "completion":
		verb = "Got completions"
	case "document_symbols":
		verb = "Listed document symbols"
	case "rename":
		verb = "Prepared rename"
	case "code_action":
		verb = "Listed code actions"
	case "job_run":
		verb = "Started job"
	case "job_output":
		verb = "Read job output"
	case "job_kill":
		verb = "Stopped job"
	default:
		if display, ok := mcpToolDisplayName(name); ok {
			verb = "Called " + display
			break
		}
		if strings.TrimSpace(name) != "" {
			verb = "Ran " + name
		}
	}
	if summary == "" {
		return verb
	}
	return verb + " " + summary
}

func mcpToolDisplayName(name string) (string, bool) {
	name = strings.TrimSpace(name)
	if !strings.HasPrefix(name, "mcp__") {
		return "", false
	}
	parts := strings.SplitN(name, "__", 3)
	if len(parts) != 3 || strings.TrimSpace(parts[1]) == "" || strings.TrimSpace(parts[2]) == "" {
		return "MCP tool", true
	}
	return "MCP " + parts[1] + "/" + parts[2], true
}

func statusForActivity(event Event) string {
	switch strings.TrimSpace(event.ActivityKind) {
	case "tool":
		switch event.Status {
		case "queued", "running":
			return statusTool
		default:
			return statusThinking
		}
	case "thinking":
		return statusThinking
	case "permission":
		return statusTool
	case "mode":
		return statusTool
	case "notice":
		if event.Status == "running" {
			return statusCompacting
		}
		return statusThinking
	default:
		return statusThinking
	}
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func firstNonEmptyLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}
