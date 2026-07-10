package tui

import (
	"fmt"
	"strings"
)

// appendOrUpdateActivity appends a new activity block or updates an existing
// one in the live transcript. It also carries through any todo state from the
// event. Live events arrive in real time during an agent turn.
func (l *messageList) appendOrUpdateActivity(event Event) {
	block := activityMessage(event)
	l.appendOrUpdateBlock(block)
	l.appendOrUpdateTodo(event)
}

// appendOrUpdateLiveActivity appends a live event scoped to a specific turn
// with a "live:turn-N:" key prefix so it does not collide with restored
// history events from the same turn during resume.
func (l *messageList) appendOrUpdateLiveActivity(event Event, turn int) {
	block := activityMessage(event)
	if turn > 0 && strings.TrimSpace(block.key) != "" {
		block.key = fmt.Sprintf("live:turn-%d:%s", turn, block.key)
	}
	l.appendOrUpdateBlock(block)
	l.appendOrUpdateTodo(event)
}

// appendOrUpdateLoadedActivity appends a restored history event with a
// "history:turn-N:" key prefix. The event is first normalized so its summary
// text matches the live-format convention, avoiding duplicate rendering when
// the same event is replayed on resume.
func (l *messageList) appendOrUpdateLoadedActivity(event Event, turn int) {
	event = normalizeLoadedActivityEvent(event)
	block := activityMessage(event)
	if turn > 0 && strings.TrimSpace(block.key) != "" {
		block.key = fmt.Sprintf("history:turn-%d:%s", turn, block.key)
	}
	l.appendOrUpdateBlock(block)
	l.appendOrUpdateTodo(event)
}

// appendOrUpdateTodo wraps a todo event into a message block and upserts it.
// When a todo_write event starts a new list, the previous todo block for the
// same role is removed first so the transcript shows only the current list.
func (l *messageList) appendOrUpdateTodo(event Event) {
	block, ok := todoMessageFromEvent(event)
	if !ok {
		return
	}
	if todoEventStartsNewList(event) {
		l.removeKey(block.role, block.key)
	}
	l.appendOrUpdateBlock(block)
}

func todoEventStartsNewList(event Event) bool {
	return strings.TrimSpace(event.ToolName) == "todo_write"
}

// normalizeLoadedActivityEvent strips redundant summary prefixes (tool title,
// action name) that were stored by old agent versions, because the restored
// summary would otherwise read "Read read foo.go" instead of just "foo.go".
func normalizeLoadedActivityEvent(event Event) Event {
	if strings.TrimSpace(event.ActivityKind) != "tool" {
		event.Content = normalizeLoadedActivityDetail(event.Content)
		return event
	}
	event.Summary = normalizeLoadedToolSummary(event.ToolName, event.Status, event.Summary)
	event.Content = normalizeLoadedActivityDetail(event.Content)
	return event
}

// normalizeLoadedToolSummary strips the leading tool-title label (e.g. "Read")
// from a loaded summary when it would duplicate the automatically-prefixed
// tool-action label in the activity message rendering. For summaries that are
// just the tool name + "failed", it returns empty so the renderer produces
// "Read failed" instead of "Read Read failed".
func normalizeLoadedToolSummary(toolName, status, summary string) string {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return ""
	}
	candidates := []string{
		toolTitle(toolName, ""),
		toolAction(toolName),
		legacyToolTitle(toolName),
		legacyToolAction(toolName),
	}
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if summary == candidate {
			return ""
		}
		if strings.TrimSpace(status) == "failed" && summary == candidate+" failed" {
			return ""
		}
		if strings.HasPrefix(summary, candidate+" ") {
			rest := strings.TrimSpace(strings.TrimPrefix(summary, candidate))
			if strings.TrimSpace(status) == "failed" {
				rest = strings.TrimSpace(strings.TrimSuffix(rest, " failed"))
			}
			return rest
		}
	}
	return summary
}

func legacyToolTitle(toolName string) string {
	switch strings.TrimSpace(toolName) {
	case "task":
		return "Ran task"
	default:
		return ""
	}
}

func legacyToolAction(toolName string) string {
	switch strings.TrimSpace(toolName) {
	case "task":
		return "Running task..."
	default:
		return ""
	}
}

// normalizeLoadedActivityDetail handles backward compatibility for detail
// content stored by old agent versions that appended "... (truncated)" in free
// text. It promotes truncation notices to the first line so the TUI renders
// the notice header consistently regardless of agent version.
func normalizeLoadedActivityDetail(detail string) string {
	detail = strings.TrimRight(detail, " \t\r\n")
	if strings.TrimSpace(detail) == "" {
		return ""
	}
	if strings.Contains(detail, "activity detail truncated") {
		return promoteActivityTruncationNotice(detail)
	}
	if strings.HasSuffix(strings.TrimSpace(detail), "... (truncated)") {
		preview := strings.TrimRight(strings.TrimSuffix(detail, "... (truncated)"), " \t\r\n")
		if preview == "" {
			return "[activity detail truncated: restored from legacy session detail]"
		}
		return "[activity detail truncated: restored from legacy session detail]\n" + preview
	}
	return detail
}

func promoteActivityTruncationNotice(detail string) string {
	lines := strings.Split(detail, "\n")
	for i, line := range lines {
		if !strings.Contains(line, "activity detail truncated") {
			continue
		}
		notice := strings.TrimPrefix(strings.TrimSpace(line), "... ")
		if i == 0 {
			return detail
		}
		rest := append([]string{}, lines[:i]...)
		rest = append(rest, lines[i+1:]...)
		return notice + "\n" + strings.TrimLeft(strings.Join(rest, "\n"), "\n")
	}
	return detail
}

// appendOrUpdateActivityInGroup adds an activity entry to (or updates an entry
// in) a named group. If the group does not exist, it is created as a collapsible
// container. A placeholder "Thinking..." entry is removed from the group when
// the first non-thinking entry arrives. The group title and status are
// recomputed on each change.
func (l *messageList) appendOrUpdateActivityInGroup(groupKey, groupName string, event Event) {
	if strings.TrimSpace(groupKey) == "" {
		l.appendOrUpdateActivity(event)
		return
	}
	idx := l.activityGroupIndex(groupKey)
	if idx < 0 {
		group := message{
			role:      activityRole,
			key:       groupKey,
			kind:      activityGroupMessage,
			name:      groupName,
			title:     activityGroupPlaceholderTitle(groupName),
			status:    "running",
			collapsed: true,
		}
		l.stampMessage(&group)
		l.items = append(l.items, group)
		idx = len(l.items) - 1
	}
	entry := activityMessage(event)
	entry.key = defaultString(activityEntryKey(event), entry.key)
	group := &l.items[idx]
	if strings.TrimSpace(group.name) == "" {
		group.name = groupName
	}
	if entry.kind != thinkingMessage {
		group.entries = removePlaceholderThinkingEntry(group.entries)
	}
	group.entries = upsertActivityEntry(group.entries, entry)
	group.status = activityGroupStatus(group.entries)
	group.title = activityGroupTitleForName(group.name, group.entries)
	group.text = group.title
	l.stampMessage(group)
	l.invalidateRender()
	l.clampFocus()
}

// finishActivityGroup marks a group with the given status (done/failed) and
// finalizes its title. It is a no-op if the group key does not exist.
func (l *messageList) finishActivityGroup(key, status string) {
	idx := l.activityGroupIndex(key)
	if idx < 0 {
		return
	}
	if strings.TrimSpace(status) != "" {
		l.items[idx].status = status
	}
	if l.items[idx].title == "" {
		l.items[idx].title = activityGroupTitle(l.items[idx].entries)
		l.items[idx].text = l.items[idx].title
	}
	l.stampMessage(&l.items[idx])
	l.invalidateRender()
}

// removePlaceholderActivityGroup removes an activity group that contains only
// a single "Thinking..." entry (a placeholder that was created before any real
// activity arrived). This prevents showing an empty group when thinking was
// the only event for that run. Returns true if the group was removed.
func (l *messageList) removePlaceholderActivityGroup(key string) bool {
	idx := l.activityGroupIndex(key)
	if idx < 0 {
		return false
	}
	group := l.items[idx]
	if len(group.entries) > 1 {
		return false
	}
	if len(group.entries) == 1 {
		entry := group.entries[0]
		if entry.kind != thinkingMessage || strings.TrimSpace(entry.title) != "Thinking..." {
			return false
		}
	}
	l.items = append(l.items[:idx], l.items[idx+1:]...)
	l.reindexCopy()
	l.clampFocus()
	return true
}

// activityGroupIndex searches from the end of the message list for an
// activity group with the given key. Reverse search matches the most recent
// group when multiple groups have the same key (should not normally happen).
func (l messageList) activityGroupIndex(key string) int {
	for i := len(l.items) - 1; i >= 0; i-- {
		if l.items[i].role == activityRole && l.items[i].key == key && l.items[i].kind == activityGroupMessage {
			return i
		}
	}
	return -1
}

// appendToolStatus adds a simple tool status line to the transcript. Used
// by legacy message types that arrive as plain status text rather than
// structured events.
func (l *messageList) appendToolStatus(name, state string) {
	if strings.TrimSpace(name) == "" {
		name = "tool"
	}
	text := "tool " + name + " " + state
	block := message{
		role:      toolRole,
		text:      text,
		kind:      toolMessage,
		title:     text,
		status:    toolStatusFromLegacyState(state),
		collapsed: true,
	}
	l.stampMessage(&block)
	l.items = append(l.items, block)
	l.invalidateRender()
	l.clampFocus()
}

// appendPermissionEvent appends a permission decision block to the
// transcript. Unlike tool activities, permission events are not grouped —
// each is a standalone message that shows the source, decision, and reason.
func (l *messageList) appendPermissionEvent(event Event) {
	text := permissionEventText(event)
	block := message{
		role:      activityRole,
		text:      text,
		kind:      permissionMessage,
		title:     text,
		status:    event.Decision,
		detail:    strings.TrimSpace(event.Reason),
		collapsed: true,
	}
	l.stampMessage(&block)
	l.items = append(l.items, block)
	l.invalidateRender()
	l.clampFocus()
}

// upsertActivityEntry inserts or replaces an entry in the activity entry
// list. If the new entry has a matching key, the old entry is replaced;
// otherwise the new entry is appended. The old entry's collapsed state is
// preserved on update.
func upsertActivityEntry(entries []message, entry message) []message {
	if strings.TrimSpace(entry.key) != "" {
		for i := range entries {
			if entries[i].key == entry.key {
				collapsed := entries[i].collapsed
				entry = mergeActivityMessage(entries[i], entry)
				entries[i] = entry
				entries[i].collapsed = collapsed
				return entries
			}
		}
	}
	return append(entries, entry)
}

// mergeActivityMessage merges two messages of the same kind by delegating to
// the kind-specific merge. Different-kind messages are treated as replacements
// (incoming wins).
func mergeActivityMessage(existing, incoming message) message {
	if existing.kind == thinkingMessage && incoming.kind == thinkingMessage {
		return mergeThinkingMessage(existing, incoming)
	}
	if existing.kind == toolMessage && incoming.kind == toolMessage {
		return mergeToolMessage(existing, incoming)
	}
	return incoming
}

// mergeToolMessage merges a running-tool update into its existing entry. The
// detail content is accumulated (with truncation) when the tool is still
// running; once the tool completes (status != running), the existing detail
// is preserved only when the incoming detail is not meaningful or contains
// only shell metadata. The title is preserved from the existing entry when
// the incoming has a generic running label.
func mergeToolMessage(existing, incoming message) message {
	if incoming.status != "running" {
		if shouldKeepExistingToolDetail(existing, incoming) {
			incoming.detail = existing.detail
		}
		return incoming
	}
	if incoming.detail == "" {
		incoming.detail = existing.detail
	} else if existing.detail != "" {
		if strings.TrimSpace(existing.detail) == strings.TrimSpace(incoming.detail) {
			incoming.detail = existing.detail
		} else {
			incoming.detail = truncateToolPartialPreview(appendToolRunningDetail(existing.detail, incoming.detail))
		}
	}
	if genericRunningToolTitle(incoming) && strings.TrimSpace(existing.title) != "" {
		incoming.title = existing.title
		incoming.text = existing.text
	}
	return incoming
}

func appendToolRunningDetail(existing, incoming string) string {
	if existing == "" {
		return incoming
	}
	if incoming == "" {
		return existing
	}
	if strings.HasSuffix(existing, "\n") || strings.HasPrefix(incoming, "\n") {
		return existing + incoming
	}
	return existing + "\n" + incoming
}

// shouldKeepExistingToolDetail decides whether the running-detail content
// accumulated so far is more useful than the final detail delivered at
// completion time. This prevents a verbose running output from being replaced
// by a short final summary that loses the useful intermediate state.
func shouldKeepExistingToolDetail(existing, incoming message) bool {
	existingDetail := strings.TrimSpace(existing.detail)
	if existingDetail == "" {
		return false
	}
	incomingDetail := strings.TrimSpace(incoming.detail)
	if incomingDetail == "" {
		return true
	}
	if toolDetailHasTruncationNotice(incomingDetail) {
		return false
	}
	if !meaningfulToolDetail(incomingDetail, incoming) {
		return true
	}
	if shellMetadataOnlyDetail(incomingDetail) {
		return true
	}
	return false
}

// shellMetadataOnlyDetail returns true when a detail string contains only a
// <shell_metadata> block and empty stdout/stderr sections — the tool ran but
// produced no visible output. In that case the accumulated running detail
// (which may have partial output) should be preserved.
func shellMetadataOnlyDetail(detail string) bool {
	if !strings.HasPrefix(detail, "<shell_metadata>") {
		return false
	}
	withoutMetadata := detail
	if closeIndex := strings.Index(withoutMetadata, "</shell_metadata>"); closeIndex >= 0 {
		withoutMetadata = withoutMetadata[closeIndex+len("</shell_metadata>"):]
	} else {
		return true
	}
	withoutMetadata = strings.TrimSpace(withoutMetadata)
	if withoutMetadata == "" {
		return true
	}
	withoutMetadata = strings.TrimPrefix(withoutMetadata, "--- stdout ---")
	withoutMetadata = strings.TrimSpace(withoutMetadata)
	withoutMetadata = strings.TrimPrefix(withoutMetadata, "--- stderr ---")
	return strings.TrimSpace(withoutMetadata) == ""
}

func toolDetailHasTruncationNotice(detail string) bool {
	return strings.Contains(detail, "activity detail truncated") ||
		strings.Contains(detail, "... [tool result truncated:") ||
		strings.Contains(detail, "full_output_path=")
}

// genericRunningToolTitle returns true when a running-tool message has no
// meaningful title beyond the tool action name. The existing entry's title
// is preserved in that case to avoid flickering as partial output arrives.
func genericRunningToolTitle(item message) bool {
	if item.status != "running" {
		return false
	}
	action := strings.TrimSpace(toolAction(item.name))
	return action != "" && strings.TrimSpace(item.title) == action && strings.TrimSpace(item.text) == action
}

// truncateToolPartialPreview truncates accumulated running tool output to
// maxToolPartialPreviewRunes. The head is dropped and replaced with an
// "[earlier output truncated]" marker, preserving only the tail.
func truncateToolPartialPreview(text string) string {
	runes := []rune(text)
	if len(runes) <= maxToolPartialPreviewRunes {
		return text
	}
	marker := "[earlier output truncated]\n"
	markerRunes := []rune(marker)
	budget := maxToolPartialPreviewRunes - len(markerRunes)
	if budget <= 0 {
		return string(runes[len(runes)-maxToolPartialPreviewRunes:])
	}
	return marker + string(runes[len(runes)-budget:])
}

func mergeThinkingMessage(existing, incoming message) message {
	detail := appendThinkingDetail(thinkingDetail(existing), thinkingDetail(incoming))
	if strings.TrimSpace(detail) == "" {
		return incoming
	}
	summary := thinkingSummary(detail)
	incoming.detail = detail
	incoming.title = thinkingTitlePrefix(incoming, existing) + summary
	incoming.text = incoming.title
	return incoming
}

func thinkingTitlePrefix(items ...message) string {
	fallback := "thinking: "
	for _, item := range items {
		for _, text := range []string{item.title, item.text} {
			prefix, ok := titlePrefixBeforeThinking(text)
			if ok {
				titlePrefix := prefix + "thinking: "
				if strings.TrimSpace(prefix) != "" {
					return titlePrefix
				}
				fallback = titlePrefix
			}
		}
	}
	return fallback
}

func titlePrefixBeforeThinking(text string) (string, bool) {
	text = strings.TrimSpace(text)
	lower := strings.ToLower(text)
	idx := strings.Index(lower, "thinking:")
	if idx < 0 {
		return "", false
	}
	return text[:idx], true
}

func thinkingDetail(item message) string {
	// Use raw non-empty check so whitespace-only deltas ("\n\n" paragraph
	// breaks) survive the merge - TrimSpace would treat them as missing and
	// fall through to the placeholder title path.
	if item.detail != "" {
		return item.detail
	}
	title := defaultString(item.title, item.text)
	if isPlaceholderActivityTitle(title) {
		return ""
	}
	return stripThinkingPrefix(title)
}

func appendThinkingDetail(existing, incoming string) string {
	// Use raw equality so whitespace-only chunks ("\n\n" paragraph breaks)
	// concatenate normally - TrimSpace here would silently drop the only
	// signal we have for paragraph boundaries in streamed reasoning.
	if incoming == "" {
		return existing
	}
	if existing == "" {
		return incoming
	}
	if incoming == existing || strings.HasPrefix(incoming, existing) {
		return incoming
	}
	if strings.HasSuffix(existing, incoming) {
		return existing
	}
	return existing + incoming
}

func thinkingSummary(detail string) string {
	summary := strings.Join(strings.Fields(detail), " ")
	if summary == "" {
		return ""
	}
	runes := []rune(summary)
	if len(runes) <= maxThinkingSummaryRunes {
		return summary
	}
	return string(runes[:maxThinkingSummaryRunes-3]) + "..."
}

func stripThinkingPrefix(text string) string {
	text = strings.TrimSpace(text)
	lower := strings.ToLower(text)
	if strings.HasPrefix(lower, "subagent: thinking:") {
		return strings.TrimSpace(text[len("subagent: thinking:"):])
	}
	if strings.HasPrefix(lower, "thinking:") {
		return strings.TrimSpace(text[len("thinking:"):])
	}
	return text
}

func removePlaceholderThinkingEntry(entries []message) []message {
	if len(entries) != 1 {
		return entries
	}
	entry := entries[0]
	if entry.kind == thinkingMessage && isPlaceholderActivityTitle(entry.title) {
		return nil
	}
	return entries
}

func isPlaceholderActivityTitle(title string) bool {
	switch strings.TrimSpace(title) {
	case "Thinking...", "Compacting...":
		return true
	default:
		return false
	}
}

func activityEntryKey(event Event) string {
	if key := activityEventKey(event); strings.TrimSpace(key) != "" {
		return key
	}
	prefix := ""
	if subagentID := strings.TrimSpace(event.SubagentID); subagentID != "" {
		prefix = "subagent:" + subagentID + ":"
	}
	switch strings.TrimSpace(event.ActivityKind) {
	case "permission":
		source := defaultString(event.Source, "permission")
		toolName := defaultString(event.ToolName, "tool")
		return prefix + "permission:" + source + ":" + toolName
	case "notice":
		return prefix + "notice:" + defaultString(event.Summary, event.Text)
	default:
		return ""
	}
}

func activityGroupPlaceholderTitle(groupName string) string {
	switch groupName {
	case thinkingGroupName:
		return "Thinking..."
	case toolGroupName:
		return "tools"
	default:
		return "Activity"
	}
}

func activityGroupTitleForName(groupName string, entries []message) string {
	title := activityGroupTitle(entries)
	switch groupName {
	case thinkingGroupName:
		if isPlaceholderActivityTitle(title) || strings.HasPrefix(strings.ToLower(title), "thinking") {
			return title
		}
		return "thinking: " + title
	case toolGroupName:
		if strings.HasPrefix(strings.ToLower(title), "tools") {
			return title
		}
		return "tools: " + title
	default:
		return title
	}
}

func activityGroupTitle(entries []message) string {
	if len(entries) == 0 {
		return "Thinking..."
	}
	toolCount, queued, running, done, failed := 0, 0, 0, 0, 0
	permissionCount := 0
	thinking := ""
	notice := ""
	for _, entry := range entries {
		switch entry.kind {
		case thinkingMessage:
			if thinking == "" {
				thinking = strings.TrimPrefix(defaultString(entry.title, entry.text), "thinking: ")
			}
		case noticeMessage:
			if notice == "" {
				notice = defaultString(entry.title, entry.text)
			}
		case permissionMessage:
			permissionCount++
		case toolMessage:
			toolCount++
			switch entry.status {
			case "queued":
				queued++
			case "running", "started":
				running++
			case "failed", "error":
				failed++
			default:
				done++
			}
		}
	}

	var parts []string
	if thinking != "" {
		parts = append(parts, thinking)
	}
	if toolCount > 0 {
		statuses := activityCountParts([]activityCount{
			{label: "failed", value: failed},
			{label: "running", value: running},
			{label: "queued", value: queued},
			{label: "done", value: done},
		})
		toolPart := "tools"
		if len(statuses) > 0 {
			toolPart += ": " + strings.Join(statuses, ", ")
		} else {
			toolPart += fmt.Sprintf(": %d", toolCount)
		}
		if active := activityToolHighlights(entries, true, 2); len(active) > 0 {
			toolPart += " · now: " + strings.Join(active, ", ")
		} else if recent := activityToolHighlights(entries, false, 2); len(recent) > 0 {
			toolPart += " · last: " + strings.Join(recent, ", ")
		}
		parts = append(parts, toolPart)
	}
	if permissionCount > 0 {
		parts = append(parts, fmt.Sprintf("permissions: %d", permissionCount))
	}
	if strings.TrimSpace(notice) != "" {
		parts = append(parts, notice)
	}
	if len(parts) == 0 {
		return "Activity"
	}
	return strings.Join(parts, "  ")
}

func activityToolHighlights(entries []message, activeOnly bool, limit int) []string {
	var highlights []string
	for i := len(entries) - 1; i >= 0 && len(highlights) < limit; i-- {
		entry := entries[i]
		if entry.kind != toolMessage {
			continue
		}
		active := entry.status == "queued" || entry.status == "running" || entry.status == "started"
		if activeOnly && !active {
			continue
		}
		highlights = append(highlights, compactToolHighlight(entry))
	}
	return highlights
}

func compactToolHighlight(entry message) string {
	title := defaultString(entry.title, defaultString(entry.name, "tool"))
	title = strings.TrimSpace(title)
	title = strings.TrimPrefix(title, "Writing command... ")
	title = strings.TrimPrefix(title, "Reading file... ")
	title = strings.TrimPrefix(title, "Listing directory... ")
	title = strings.TrimPrefix(title, "Searching content... ")
	title = strings.TrimPrefix(title, "Finding files... ")
	title = strings.TrimPrefix(title, "Preparing write... ")
	title = strings.TrimPrefix(title, "Preparing edit... ")
	title = strings.TrimPrefix(title, "Preparing multi-edit... ")
	title = strings.TrimPrefix(title, "Starting job... ")
	title = strings.TrimPrefix(title, "Reading job output... ")
	title = strings.TrimPrefix(title, "Stopping job... ")
	title = strings.TrimPrefix(title, "Running Task... ")
	title = strings.TrimPrefix(title, "Writing memory... ")
	title = strings.TrimPrefix(title, "Writing plan... ")
	title = strings.TrimPrefix(title, "Updating plan step... ")
	title = strings.TrimPrefix(title, "Writing todos... ")
	title = strings.TrimPrefix(title, "Updating todos... ")
	title = strings.TrimPrefix(title, "Reading tool result... ")
	title = strings.TrimPrefix(title, "Checking diagnostics... ")
	title = strings.TrimPrefix(title, "Finding references... ")
	title = strings.TrimPrefix(title, "Reading hover... ")
	title = strings.TrimPrefix(title, "Getting completions... ")
	title = strings.TrimPrefix(title, "Listing document symbols... ")
	title = strings.TrimPrefix(title, "Preparing rename... ")
	title = strings.TrimPrefix(title, "Listing code actions... ")
	title = strings.TrimPrefix(title, "Ran Task ")
	title = strings.TrimPrefix(title, "Ran task ")
	if title == "" {
		title = defaultString(entry.name, "tool")
	}
	return truncateText(title, 32)
}

type activityCount struct {
	label string
	value int
}

func activityCountParts(counts []activityCount) []string {
	var parts []string
	for _, count := range counts {
		if count.value > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", count.value, count.label))
		}
	}
	return parts
}

func activityGroupStatus(entries []message) string {
	if len(entries) == 0 {
		return "running"
	}
	failed := 0
	done := 0
	hasQueued := false
	hasRunning := false
	for _, entry := range entries {
		switch entry.status {
		case "failed", "error", "deny", "denied":
			failed++
		case "running", "started":
			hasRunning = true
		case "queued":
			hasQueued = true
		default:
			done++
		}
	}
	if hasRunning {
		return "running"
	}
	if hasQueued {
		return "queued"
	}
	if failed > 0 {
		if done > 0 {
			return activityStatusPartialFailed
		}
		return "failed"
	}
	return "done"
}
