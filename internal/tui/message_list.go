package tui

import (
	"fmt"
	"strings"

	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"

	"github.com/feimingxliu/ub/internal/tui/tuitheme"
)

const (
	userRole      = "user"
	assistantRole = "assistant"
	activityRole  = "activity"
	toolRole      = "tool"
	systemRole    = "system"
	errorRole     = "error"
)

const (
	maxThinkingSummaryRunes    = 180
	maxToolPartialPreviewRunes = 12000
)

const activityStatusPartialFailed = "partial_failed"

const (
	thinkingGroupName = "thinking"
	toolGroupName     = "tool"
)

type messageKind string

const (
	textMessage          messageKind = "text"
	thinkingMessage      messageKind = "thinking"
	todoMessage          messageKind = "todo"
	toolMessage          messageKind = "tool"
	permissionMessage    messageKind = "permission"
	activityGroupMessage messageKind = "activity_group"
	noticeMessage        messageKind = "notice"
	systemMessage        messageKind = "system"
	errorMessage         messageKind = "error"
)

type message struct {
	role      string
	text      string
	key       string
	kind      messageKind
	title     string
	name      string
	status    string
	detail    string
	collapsed bool
	entries   []message
	copyIndex int // 1-based index for /copy; 0 means not copyable
}

func (m message) collapsible() bool {
	switch m.kind {
	case thinkingMessage, toolMessage, permissionMessage, activityGroupMessage:
		return true
	default:
		return false
	}
}

type messageList struct {
	items          []message
	focus          int
	entryFocus     int
	renderCache    map[string]renderedMessages
	renderVersion  uint64
	batchDepth     int
	copyIndexDirty bool
}

type renderedMessages struct {
	lines []string
	spans []messageSpan
}

type messageSpan struct {
	itemIndex  int
	entryIndex int
	entry      bool
	start      int
	end        int
	startCol   int
	endCol     int
}

type messageTarget struct {
	itemIndex  int
	entryIndex int
}

func newMessageList() messageList {
	return messageList{focus: -1, entryFocus: -1, renderCache: map[string]renderedMessages{}}
}

func (l *messageList) beginBatch() {
	l.batchDepth++
}

func (l *messageList) endBatch() {
	if l.batchDepth > 0 {
		l.batchDepth--
	}
	if l.batchDepth == 0 && l.copyIndexDirty {
		l.reindexCopy()
	}
	l.invalidateRender()
}

func (l *messageList) invalidateRender() {
	l.renderVersion++
	if l.renderCache == nil {
		l.renderCache = map[string]renderedMessages{}
		return
	}
	for key := range l.renderCache {
		delete(l.renderCache, key)
	}
}

func (l *messageList) append(role, text string) {
	l.items = append(l.items, message{
		role:      role,
		text:      text,
		kind:      kindForRole(role),
		title:     text,
		collapsed: defaultCollapsed(kindForRole(role)),
	})
	l.reindexCopy()
	l.clampFocus()
}

func (l *messageList) appendOrUpdate(role, key, text string) {
	if strings.TrimSpace(key) == "" {
		l.append(role, text)
		return
	}
	block := message{
		role:      role,
		text:      text,
		key:       key,
		kind:      kindForRoleAndKey(role, key),
		title:     text,
		collapsed: defaultCollapsed(kindForRoleAndKey(role, key)),
	}
	l.appendOrUpdateBlock(block)
}

func (l *messageList) appendThinking(key, text string) {
	l.appendOrUpdateBlock(message{
		role:      activityRole,
		text:      text,
		key:       key,
		kind:      thinkingMessage,
		title:     text,
		status:    "running",
		collapsed: true,
	})
}

func (l *messageList) startActivityGroup(key, text string) {
	if strings.TrimSpace(key) == "" {
		return
	}
	block := message{
		role:      activityRole,
		text:      text,
		key:       key,
		kind:      activityGroupMessage,
		name:      thinkingGroupName,
		title:     text,
		status:    "running",
		collapsed: true,
		entries: []message{{
			role:      activityRole,
			text:      text,
			key:       "thinking",
			kind:      thinkingMessage,
			title:     text,
			status:    "running",
			collapsed: true,
		}},
	}
	l.appendOrUpdateBlock(block)
}

func (l *messageList) appendOrUpdateActivity(event Event) {
	block := activityMessage(event)
	l.appendOrUpdateBlock(block)
	l.appendOrUpdateTodo(event)
}

func (l *messageList) appendOrUpdateLiveActivity(event Event, turn int) {
	block := activityMessage(event)
	if turn > 0 && strings.TrimSpace(block.key) != "" {
		block.key = fmt.Sprintf("live:turn-%d:%s", turn, block.key)
	}
	l.appendOrUpdateBlock(block)
	l.appendOrUpdateTodo(event)
}

func (l *messageList) appendOrUpdateLoadedActivity(event Event, turn int) {
	event = normalizeLoadedActivityEvent(event)
	block := activityMessage(event)
	if turn > 0 && strings.TrimSpace(block.key) != "" {
		block.key = fmt.Sprintf("history:turn-%d:%s", turn, block.key)
	}
	l.appendOrUpdateBlock(block)
	l.appendOrUpdateTodo(event)
}

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

func normalizeLoadedActivityEvent(event Event) Event {
	if strings.TrimSpace(event.ActivityKind) != "tool" {
		event.Content = normalizeLoadedActivityDetail(event.Content)
		return event
	}
	event.Summary = normalizeLoadedToolSummary(event.ToolName, event.Status, event.Summary)
	event.Content = normalizeLoadedActivityDetail(event.Content)
	return event
}

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

func (l *messageList) appendOrUpdateActivityInGroup(groupKey, groupName string, event Event) {
	if strings.TrimSpace(groupKey) == "" {
		l.appendOrUpdateActivity(event)
		return
	}
	idx := l.activityGroupIndex(groupKey)
	if idx < 0 {
		l.items = append(l.items, message{
			role:      activityRole,
			key:       groupKey,
			kind:      activityGroupMessage,
			name:      groupName,
			title:     activityGroupPlaceholderTitle(groupName),
			status:    "running",
			collapsed: true,
		})
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
	l.reindexCopy()
	l.clampFocus()
}

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
	l.invalidateRender()
}

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

func (l messageList) activityGroupIndex(key string) int {
	for i := len(l.items) - 1; i >= 0; i-- {
		if l.items[i].role == activityRole && l.items[i].key == key && l.items[i].kind == activityGroupMessage {
			return i
		}
	}
	return -1
}

func (l *messageList) appendToolStatus(name, state string) {
	if strings.TrimSpace(name) == "" {
		name = "tool"
	}
	text := "tool " + name + " " + state
	l.items = append(l.items, message{
		role:      toolRole,
		text:      text,
		kind:      toolMessage,
		title:     text,
		status:    toolStatusFromLegacyState(state),
		collapsed: true,
	})
	l.reindexCopy()
	l.clampFocus()
}

func (l *messageList) appendPermissionEvent(event Event) {
	text := permissionEventText(event)
	l.items = append(l.items, message{
		role:      activityRole,
		text:      text,
		kind:      permissionMessage,
		title:     text,
		status:    event.Decision,
		detail:    strings.TrimSpace(event.Reason),
		collapsed: true,
	})
	l.reindexCopy()
	l.clampFocus()
}

func (l *messageList) appendOrUpdateBlock(block message) {
	if strings.TrimSpace(block.key) == "" {
		l.items = append(l.items, block)
		l.reindexCopy()
		l.clampFocus()
		return
	}
	for i := len(l.items) - 1; i >= 0; i-- {
		item := &l.items[i]
		if item.role == block.role && item.key == block.key {
			collapsed := item.collapsed
			block = mergeActivityMessage(*item, block)
			*item = block
			item.collapsed = collapsed
			l.reindexCopy()
			l.clampFocus()
			return
		}
	}
	l.items = append(l.items, block)
	l.reindexCopy()
	l.clampFocus()
}

func (l *messageList) removeKey(role, key string) {
	if strings.TrimSpace(key) == "" {
		return
	}
	for i := len(l.items) - 1; i >= 0; i-- {
		if l.items[i].role != role || l.items[i].key != key {
			continue
		}
		l.items = append(l.items[:i], l.items[i+1:]...)
		l.reindexCopy()
		l.clampFocus()
		return
	}
}

func (l *messageList) clear() {
	l.items = nil
	l.focus = -1
	l.entryFocus = -1
	l.copyIndexDirty = false
	l.invalidateRender()
}

func (l *messageList) load(messages []InitialMessage) {
	l.beginBatch()
	defer l.endBatch()
	l.items = nil
	l.focus = -1
	l.entryFocus = -1
	l.copyIndexDirty = false
	for _, msg := range messages {
		if strings.TrimSpace(msg.ActivityKind) != "" {
			event := Event{
				Type:         EventActivity,
				Text:         msg.Text,
				ToolUseID:    msg.ToolUseID,
				ToolName:     msg.ToolName,
				Content:      msg.Content,
				ActivityKind: msg.ActivityKind,
				Status:       msg.Status,
				Summary:      msg.Summary,
				Decision:     msg.Decision,
				Source:       msg.Source,
				Reason:       msg.Reason,
				Allowed:      msg.Allowed,
				IsError:      msg.IsError,
			}
			l.appendOrUpdateLoadedActivity(event, msg.Turn)
			continue
		}
		role := normalizeRole(msg.Role)
		if strings.TrimSpace(msg.Text) == "" {
			continue
		}
		l.append(role, msg.Text)
	}
}

func (l *messageList) startAssistant() {
	l.items = append(l.items, message{role: assistantRole, kind: textMessage})
	l.reindexCopy()
	l.clampFocus()
}

// breakAssistant ensures the next assistant delta starts a new message,
// so tool/thinking items can be interleaved between text segments.
func (l *messageList) breakAssistant() {
	// If the last item is an assistant message, appending the next delta
	// will naturally start a new one. If it's not, appendAssistantDelta
	// already calls startAssistant. So this is a no-op that just ensures
	// we don't merge into the previous assistant message.
}

func (l *messageList) appendAssistantDelta(text string) {
	if len(l.items) == 0 || l.items[len(l.items)-1].role != assistantRole {
		l.startAssistant()
	}
	l.items[len(l.items)-1].text += text
	l.items[len(l.items)-1].title = l.items[len(l.items)-1].text
	l.invalidateRender()
}

func (l *messageList) toggleAt(width, height, scroll, x, y int, styles tuitheme.Styles) bool {
	if y < 0 || y >= height {
		return false
	}
	rendered := l.render(width, styles)
	start := visibleStart(len(rendered.lines), height, scroll)
	line := start + y
	for _, span := range rendered.spans {
		if line < span.start || line >= span.end {
			continue
		}
		if x < span.startCol || x >= span.endCol {
			continue
		}
		if span.itemIndex < 0 || span.itemIndex >= len(l.items) {
			return false
		}
		if span.entry {
			group := &l.items[span.itemIndex]
			if group.kind != activityGroupMessage || span.entryIndex < 0 || span.entryIndex >= len(group.entries) {
				return false
			}
			entry := &group.entries[span.entryIndex]
			if !entry.collapsible() || !activityEntryHasDetail(*entry) {
				return false
			}
			l.focus = span.itemIndex
			l.entryFocus = span.entryIndex
			entry.collapsed = !entry.collapsed
			l.invalidateRender()
			return true
		}
		if !l.items[span.itemIndex].collapsible() {
			return false
		}
		l.focus = span.itemIndex
		l.entryFocus = -1
		l.items[span.itemIndex].collapsed = !l.items[span.itemIndex].collapsed
		l.invalidateRender()
		return true
	}
	return false
}

func (l *messageList) toggleLatestCollapsible() bool {
	for i := len(l.items) - 1; i >= 0; i-- {
		item := &l.items[i]
		if item.kind == activityGroupMessage {
			if !item.collapsed {
				for j := len(item.entries) - 1; j >= 0; j-- {
					entry := &item.entries[j]
					if entry.collapsible() && activityEntryHasDetail(*entry) {
						l.focus = i
						l.entryFocus = j
						entry.collapsed = !entry.collapsed
						l.invalidateRender()
						return true
					}
				}
			}
			if item.collapsible() {
				l.focus = i
				l.entryFocus = -1
				item.collapsed = !item.collapsed
				l.invalidateRender()
				return true
			}
			continue
		}
		if item.collapsible() {
			l.focus = i
			l.entryFocus = -1
			item.collapsed = !item.collapsed
			l.invalidateRender()
			return true
		}
	}
	return false
}

func (l *messageList) focusNextCollapsible() bool {
	return l.focusCollapsible(1)
}

func (l *messageList) focusPreviousCollapsible() bool {
	return l.focusCollapsible(-1)
}

func (l *messageList) focusCollapsible(delta int) bool {
	targets := l.collapsibleTargets()
	if len(targets) == 0 {
		l.focus = -1
		l.entryFocus = -1
		return false
	}
	current := -1
	for i, target := range targets {
		if target.itemIndex == l.focus && target.entryIndex == l.entryFocus {
			current = i
			break
		}
	}
	next := 0
	if current < 0 {
		if delta < 0 {
			next = len(targets) - 1
		}
	} else {
		next = (current + delta) % len(targets)
		if next < 0 {
			next += len(targets)
		}
	}
	l.focus = targets[next].itemIndex
	l.entryFocus = targets[next].entryIndex
	return true
}

func (l *messageList) toggleFocusedCollapsible() bool {
	target, ok := l.focusTarget()
	if !ok {
		return false
	}
	if target.entryIndex >= 0 {
		entry := &l.items[target.itemIndex].entries[target.entryIndex]
		entry.collapsed = !entry.collapsed
		l.invalidateRender()
		return true
	}
	l.items[target.itemIndex].collapsed = !l.items[target.itemIndex].collapsed
	if l.items[target.itemIndex].collapsed {
		l.entryFocus = -1
	}
	l.invalidateRender()
	return true
}

func (l messageList) hasFocusedCollapsible() bool {
	_, ok := l.focusTarget()
	return ok
}

func (l *messageList) clearFocus() bool {
	if l.focus < 0 && l.entryFocus < 0 {
		return false
	}
	l.focus = -1
	l.entryFocus = -1
	l.invalidateRender()
	return true
}

func (l messageList) focusTarget() (messageTarget, bool) {
	if l.focus < 0 || l.focus >= len(l.items) {
		return messageTarget{}, false
	}
	item := l.items[l.focus]
	if l.entryFocus >= 0 {
		if item.kind != activityGroupMessage || item.collapsed || l.entryFocus >= len(item.entries) {
			return messageTarget{}, false
		}
		entry := item.entries[l.entryFocus]
		if !entry.collapsible() || !activityEntryHasDetail(entry) {
			return messageTarget{}, false
		}
		return messageTarget{itemIndex: l.focus, entryIndex: l.entryFocus}, true
	}
	if !item.collapsible() {
		return messageTarget{}, false
	}
	return messageTarget{itemIndex: l.focus, entryIndex: -1}, true
}

func (l messageList) collapsibleTargets() []messageTarget {
	var targets []messageTarget
	for itemIndex, item := range l.items {
		if !item.collapsible() {
			continue
		}
		targets = append(targets, messageTarget{itemIndex: itemIndex, entryIndex: -1})
		if item.kind != activityGroupMessage || item.collapsed {
			continue
		}
		for entryIndex, entry := range item.entries {
			if entry.collapsible() && activityEntryHasDetail(entry) {
				targets = append(targets, messageTarget{itemIndex: itemIndex, entryIndex: entryIndex})
			}
		}
	}
	return targets
}

func (l messageList) focusedLine(width int, styles tuitheme.Styles) (int, int, bool) {
	target, ok := l.focusTarget()
	if !ok {
		return 0, 0, false
	}
	rendered := l.render(width, styles)
	for _, span := range rendered.spans {
		if span.itemIndex != target.itemIndex {
			continue
		}
		if target.entryIndex >= 0 && span.entry && span.entryIndex == target.entryIndex {
			return span.start, len(rendered.lines), true
		}
		if target.entryIndex < 0 && !span.entry {
			return span.start, len(rendered.lines), true
		}
	}
	return 0, len(rendered.lines), false
}

func (l messageList) view(width, height, scroll int, styles tuitheme.Styles) string {
	rendered := l.render(width, styles)
	if len(rendered.lines) == 0 {
		return styles.Render(styles.Muted, truncateText("No messages yet · type a prompt or /help", contentWidth(width)))
	}
	if height <= 0 || height >= len(rendered.lines) {
		return strings.Join(rendered.lines, "\n")
	}
	start := visibleStart(len(rendered.lines), height, scroll)
	visible := append([]string(nil), rendered.lines[start:start+height]...)
	visible = l.applyViewportClipMarkers(visible, rendered, start, width, styles)
	return strings.Join(visible, "\n")
}

func (l messageList) applyViewportClipMarkers(lines []string, rendered renderedMessages, start, width int, styles tuitheme.Styles) []string {
	if len(lines) == 0 {
		return lines
	}
	maxWidth := contentWidth(width)
	if maxWidth <= 0 {
		maxWidth = 10
	}
	endLine := start + len(lines) - 1
	if marker := l.viewportClipMarker(rendered, start, endLine); marker != "" {
		lines[len(lines)-1] = styles.Render(styles.Modal.Warning, truncateText(marker, maxWidth))
	}
	return lines
}

func (l messageList) viewportClipMarker(rendered renderedMessages, startLine, endLine int) string {
	earlier := l.viewportCutsExpandedToolDetail(rendered, startLine, true)
	later := l.viewportCutsExpandedToolDetail(rendered, endLine, false)
	if !earlier && !later {
		earlier, later = l.focusedExpandedToolDetailCuts(rendered, startLine, endLine)
	}
	switch {
	case earlier && later:
		return "[tool detail clipped: more above and below - PgUp/PgDn or scroll]"
	case earlier:
		return "[tool detail clipped: more above - PgUp or scroll]"
	case later:
		return "[tool detail clipped: more below - PgDn or scroll]"
	default:
		return ""
	}
}

func (l messageList) focusedExpandedToolDetailCuts(rendered renderedMessages, startLine, endLine int) (bool, bool) {
	target, ok := l.focusTarget()
	if !ok {
		return false, false
	}
	for _, span := range rendered.spans {
		if span.itemIndex != target.itemIndex {
			continue
		}
		if target.entryIndex >= 0 {
			if !span.entry || span.entryIndex != target.entryIndex {
				continue
			}
		} else if span.entry {
			continue
		}
		if !l.spanIsExpandedToolDetail(span) {
			return false, false
		}
		if endLine < span.start || startLine >= span.end {
			return false, false
		}
		return startLine > span.start, endLine+1 < span.end
	}
	return false, false
}

func (l messageList) spanIsExpandedToolDetail(span messageSpan) bool {
	if span.itemIndex < 0 || span.itemIndex >= len(l.items) {
		return false
	}
	item := l.items[span.itemIndex]
	if item.collapsed {
		return false
	}
	if !span.entry {
		return item.kind == toolMessage
	}
	if item.kind != activityGroupMessage || span.entryIndex < 0 || span.entryIndex >= len(item.entries) {
		return false
	}
	entry := item.entries[span.entryIndex]
	return entry.kind == toolMessage && !entry.collapsed
}

func (l messageList) viewportCutsExpandedToolDetail(rendered renderedMessages, line int, top bool) bool {
	for _, span := range rendered.spans {
		if line < span.start || line >= span.end {
			continue
		}
		if !l.spanIsExpandedToolDetail(span) {
			return false
		}
		if top {
			return line > span.start
		}
		return line+1 < span.end
	}
	return false
}

func (l messageList) lines(width int) []string {
	return l.render(width, tuitheme.Plain()).lines
}

func (l messageList) render(width int, styles tuitheme.Styles) renderedMessages {
	if len(l.items) == 0 {
		return renderedMessages{}
	}
	cacheKey := l.renderCacheKey(width, styles)
	if l.renderCache != nil {
		if rendered, ok := l.renderCache[cacheKey]; ok {
			return rendered
		}
	}

	var out []string
	var spans []messageSpan
	for i := 0; i < len(l.items); {
		if l.items[i].compactActivity() {
			if len(out) > 0 {
				out = append(out, "")
			}
			lines, runSpans, next := l.renderCompactActivityRun(i, len(out), width, styles)
			out = append(out, lines...)
			spans = append(spans, runSpans...)
			i = next
			continue
		}
		if len(out) > 0 {
			out = append(out, "")
		}
		item := l.items[i]
		start := len(out)
		if item.kind == activityGroupMessage {
			entryFocus := -1
			if i == l.focus {
				entryFocus = l.entryFocus
			}
			lines, itemSpans := renderActivityGroupBlockWithSpans(item, i == l.focus && l.entryFocus < 0, entryFocus, width, styles, i, start)
			out = append(out, lines...)
			spans = append(spans, itemSpans...)
			i++
			continue
		}
		out = append(out, l.renderItem(item, i == l.focus && l.entryFocus < 0, width, styles)...)
		spans = append(spans, messageSpan{itemIndex: i, start: start, end: len(out), endCol: contentWidth(width)})
		i++
	}
	rendered := renderedMessages{lines: out, spans: spans}
	if l.renderCache != nil {
		l.renderCache[cacheKey] = rendered
	}
	return rendered
}

func (l messageList) renderCacheKey(width int, styles tuitheme.Styles) string {
	styleName := styles.Markdown.StyleName
	if styles.Plain {
		styleName = "plain"
	}
	return fmt.Sprintf("%d:%t:%s:%d:%d:%d", width, styles.Plain, styleName, l.focus, l.entryFocus, l.renderVersion)
}

func (m message) compactActivity() bool {
	switch m.kind {
	case thinkingMessage, toolMessage, permissionMessage:
		return m.collapsible() && m.collapsed
	default:
		return false
	}
}

func (l messageList) renderCompactActivityRun(startIndex, startLine, width int, styles tuitheme.Styles) ([]string, []messageSpan, int) {
	maxWidth := contentWidth(width)
	if maxWidth <= 0 {
		maxWidth = 10
	}

	var lines []string
	var spans []messageSpan
	for i := startIndex; i < len(l.items); i++ {
		item := l.items[i]
		if !item.compactActivity() {
			return lines, spans, i
		}
		plain := activityChipText(item, max(10, maxWidth))
		chipWidth := runewidth.StringWidth(plain)
		if chipWidth > maxWidth {
			plain = truncateText(plain, maxWidth)
			chipWidth = runewidth.StringWidth(plain)
		}
		lineIndex := len(lines)
		lines = append(lines, renderActivityChip(item, i == l.focus && l.entryFocus < 0, styles, plain))
		spans = append(spans, messageSpan{
			itemIndex: i,
			start:     startLine + lineIndex,
			end:       startLine + lineIndex + 1,
			startCol:  0,
			endCol:    chipWidth,
		})
	}
	return lines, spans, len(l.items)
}

func (l messageList) renderItem(item message, focused bool, width int, styles tuitheme.Styles) []string {
	switch item.kind {
	case activityGroupMessage:
		return renderActivityGroupBlock(item, focused, width, styles)
	case todoMessage:
		return renderTodoBlock(item, focused, width, styles)
	case thinkingMessage:
		return renderActivityBlock(item, focused, width, styles, styles.Thinking, "thinking")
	case toolMessage:
		return renderActivityBlock(item, focused, width, styles, styles.Tool, "tool")
	case permissionMessage:
		return renderActivityBlock(item, focused, width, styles, styles.Tool, "permission")
	case errorMessage:
		return renderWrappedPrefixed(item.text, "! ", "  ", width, styles, styles.Role.ErrorPrefix, styles.Error)
	case noticeMessage:
		return renderWrappedPrefixed(item.text, "• ", "  ", width, styles, styles.Role.SystemPrefix, styles.System)
	case systemMessage:
		return renderWrappedPrefixed(item.text, "# ", "  ", width, styles, styles.Role.SystemPrefix, styles.System)
	default:
		return renderTextBlock(item, width, styles)
	}
}

func renderTodoBlock(item message, focused bool, width int, styles tuitheme.Styles) []string {
	icon := activityStatusIcon(item.kind, item.status)
	title := defaultString(item.title, "Todo")
	line := truncateText(icon+" "+title, contentWidth(width))
	style := styles.Tool.Expanded
	if item.status == "failed" || item.status == "error" {
		style = styles.Tool.Failed
	}
	if focused {
		style = styles.Focus
	}
	out := []string{styles.Render(style, line)}
	return appendTodoDetailLines(out, item.detail, "  ", width, styles)
}

func renderTextBlock(item message, width int, styles tuitheme.Styles) []string {
	prefix, _, prefixStyle := messagePrefix(item.role, styles)
	if item.copyIndex > 0 {
		prefix = fmt.Sprintf("[%d]%s", item.copyIndex, prefix)
	}
	indent := strings.Repeat(" ", runewidth.StringWidth(prefix))
	textWidth := max(10, contentWidth(width)-runewidth.StringWidth(prefix))
	lines := markdownLines(item.text, textWidth, styles)
	if len(lines) == 0 {
		lines = []string{""}
	}
	out := make([]string, 0, len(lines))
	out = append(out, styles.Render(prefixStyle, prefix)+lines[0])
	for _, line := range lines[1:] {
		out = append(out, indent+line)
	}
	return out
}

func renderActivityBlock(item message, focused bool, width int, styles tuitheme.Styles, activity tuitheme.ActivityStyles, label string) []string {
	marker := "▸ "
	if !item.collapsed {
		marker = "▾ "
	}
	statusIcon := activityStatusIcon(item.kind, item.status)
	title := defaultString(item.title, item.text)
	if label == "thinking" && !strings.HasPrefix(strings.ToLower(title), "thinking") {
		title = "thinking: " + title
	}
	line := truncateText(marker+statusIcon+" "+title, contentWidth(width))
	style := activity.Collapsed
	if !item.collapsed {
		style = activity.Expanded
	}
	if item.status == "failed" || item.status == "error" {
		style = activity.Failed
	}
	if focused {
		style = activity.Focus
	}
	out := []string{styles.Render(style, line)}
	if item.collapsed {
		return out
	}
	detail := expandedDetail(item)
	if detail == "" {
		return out
	}
	if item.kind == toolMessage {
		return appendToolDetailLines(out, item.name, detail, "└ ", width, styles)
	}
	detailLines := wrapText(detail, max(10, contentWidth(width)-2))
	for _, line := range detailLines {
		out = append(out, styles.Render(activity.Detail, "└ "+line))
	}
	return out
}

func renderActivityGroupBlock(item message, focused bool, width int, styles tuitheme.Styles) []string {
	lines, _ := renderActivityGroupBlockWithSpans(item, focused, -1, width, styles, -1, 0)
	return lines
}

func renderActivityGroupBlockWithSpans(item message, focused bool, focusedEntry, width int, styles tuitheme.Styles, itemIndex, startLine int) ([]string, []messageSpan) {
	marker := "▸ "
	if !item.collapsed {
		marker = "▾ "
	}
	icon := activityStatusIcon(item.kind, item.status)
	title := defaultString(item.title, activityGroupTitle(item.entries))
	line := truncateText(marker+icon+" "+title, contentWidth(width))
	style := styles.Tool.Collapsed
	if item.status == "failed" || item.status == "error" {
		style = styles.Tool.Failed
	}
	if focused {
		style = styles.Focus
	}
	out := []string{styles.Render(style, line)}
	spans := []messageSpan{{
		itemIndex: itemIndex,
		start:     startLine,
		end:       startLine + 1,
		endCol:    contentWidth(width),
	}}
	if item.collapsed {
		return out, spans
	}
	if len(item.entries) == 0 {
		return append(out, styles.Render(styles.Tool.Detail, "└ no activity")), spans
	}
	twoStageDetails := item.name == toolGroupName
	for entryIndex, entry := range item.entries {
		entryLine := activityEntryLine(entry, contentWidth(width)-2, twoStageDetails)
		lineIndex := startLine + len(out)
		style := activityDetailStyle(entry, styles)
		if entryIndex == focusedEntry {
			style = styles.Focus
		}
		out = append(out, styles.Render(style, "└ "+entryLine))
		hasEntryTarget := twoStageDetails && activityEntryHasDetail(entry)
		if detail := expandedDetail(entry); detail != "" && (!twoStageDetails || !entry.collapsed) {
			out = appendActivityEntryDetailLines(out, entry, detail, "  ", width, styles)
		}
		if hasEntryTarget {
			spans = append(spans, messageSpan{
				itemIndex:  itemIndex,
				entryIndex: entryIndex,
				entry:      true,
				start:      lineIndex,
				end:        startLine + len(out),
				endCol:     contentWidth(width),
			})
		}
	}
	return out, spans
}

func activityEntryLine(entry message, width int, twoStageDetails bool) string {
	marker := "  "
	if twoStageDetails && activityEntryHasDetail(entry) {
		marker = "▸ "
		if !entry.collapsed {
			marker = "▾ "
		}
	}
	icon := activityStatusIcon(entry.kind, entry.status)
	title := defaultString(entry.title, entry.text)
	return truncateText(marker+icon+" "+title, width)
}

func activityEntryHasDetail(entry message) bool {
	return expandedDetail(entry) != ""
}

func appendActivityEntryDetailLines(out []string, entry message, detail, prefix string, width int, styles tuitheme.Styles) []string {
	if entry.kind == toolMessage {
		return appendToolDetailLines(out, entry.name, detail, prefix, width, styles)
	}
	style := styles.Tool.Detail
	if entry.kind == thinkingMessage {
		style = styles.Thinking.Detail
	}
	textWidth := max(10, contentWidth(width)-runewidth.StringWidth(prefix))
	for _, line := range wrapText(detail, textWidth) {
		out = append(out, styles.Render(style, prefix+line))
	}
	return out
}

func appendToolDetailLines(out []string, toolName, detail, prefix string, width int, styles tuitheme.Styles) []string {
	if strings.TrimSpace(detail) == "" {
		return out
	}
	if toolDetailUsesTodoStyle(toolName) {
		return appendTodoDetailLines(out, detail, prefix, width, styles)
	}
	textWidth := max(10, contentWidth(width)-runewidth.StringWidth(prefix))
	if !toolDetailUsesDiffStyle(toolName) {
		style := styles.Tool.Detail
		for _, line := range strings.Split(detail, "\n") {
			for _, wrapped := range wrapLine(line, textWidth) {
				out = append(out, styles.Render(style, prefix+wrapped))
			}
		}
		return out
	}
	for _, line := range strings.Split(detail, "\n") {
		displayLine := formatToolDetailLine(line)
		style := toolDetailLineStyle(line, styles)
		for _, wrapped := range wrapLine(displayLine, textWidth) {
			out = append(out, styles.Render(style, prefix+wrapped))
		}
	}
	return out
}

func toolDetailUsesTodoStyle(toolName string) bool {
	switch strings.TrimSpace(toolName) {
	case "todo_write", "todo_update":
		return true
	default:
		return false
	}
}

func appendTodoDetailLines(out []string, detail, prefix string, width int, styles tuitheme.Styles) []string {
	textWidth := max(10, contentWidth(width)-runewidth.StringWidth(prefix))
	items := parseTodoDetailItems(detail)
	if len(items) > 0 {
		for _, item := range items {
			style := styles.Tool.Detail
			if item.status == "failed" {
				style = styles.Tool.Failed
			}
			for _, wrapped := range wrapLine(item.text, textWidth) {
				out = append(out, styles.Render(style, prefix+wrapped))
			}
		}
		return out
	}
	style := styles.Tool.Detail
	for _, line := range strings.Split(detail, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "session_id=") || strings.HasPrefix(trimmed, "todo_count=") || trimmed == "## Todo" {
			continue
		}
		for _, wrapped := range wrapLine(trimmed, textWidth) {
			out = append(out, styles.Render(style, prefix+wrapped))
		}
	}
	return out
}

type todoDetailItem struct {
	text   string
	status string
}

func parseTodoDetailItems(detail string) []todoDetailItem {
	var items []todoDetailItem
	for _, line := range strings.Split(detail, "\n") {
		text, status, ok := parseTodoDetailLine(line)
		if !ok {
			continue
		}
		items = append(items, todoDetailItem{text: text, status: status})
	}
	return items
}

func parseTodoDetailLine(line string) (string, string, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "- [") || len(line) < len("- [ ] ") {
		return "", "", false
	}
	closeIdx := strings.Index(line, "]")
	if closeIdx < 0 || closeIdx <= 3 {
		return "", "", false
	}
	mark := line[3:closeIdx]
	rest := strings.TrimSpace(line[closeIdx+1:])
	if dot := strings.Index(rest, ". "); dot > 0 && allDigits(rest[:dot]) {
		rest = strings.TrimSpace(rest[dot+2:])
	}
	label := "[ ]"
	status := "pending"
	switch mark {
	case ">":
		label = "[>]"
		status = "in_progress"
	case "x", "X":
		label = "[x]"
		status = "completed"
	case "~":
		label = "[~]"
		status = "skipped"
	case "!":
		label = "[!]"
		status = "failed"
	}
	if rest == "" {
		return "", "", false
	}
	return label + " " + rest, status, true
}

func allDigits(text string) bool {
	if text == "" {
		return false
	}
	for _, r := range text {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func toolDetailUsesDiffStyle(toolName string) bool {
	switch strings.TrimSpace(toolName) {
	case "write", "edit", "multiedit":
		return true
	default:
		return false
	}
}

func toolDetailLineStyle(line string, styles tuitheme.Styles) lipgloss.Style {
	switch toolDetailLineKind(line) {
	case toolDetailSummaryLine:
		return styles.Diff.Path
	case toolDetailHeaderLine:
		return styles.Diff.Header
	case toolDetailAddedLine:
		return styles.Diff.Added
	case toolDetailRemovedLine:
		return styles.Diff.Removed
	case toolDetailBlankLine:
		return styles.Tool.Detail
	default:
		return styles.Diff.Context
	}
}

type toolDetailLineKindValue int

const (
	toolDetailContextLine toolDetailLineKindValue = iota
	toolDetailSummaryLine
	toolDetailHeaderLine
	toolDetailAddedLine
	toolDetailRemovedLine
	toolDetailBlankLine
)

func toolDetailLineKind(line string) toolDetailLineKindValue {
	trimmed := strings.TrimSpace(line)
	switch {
	case isFileChangeSummaryLine(trimmed):
		return toolDetailSummaryLine
	case strings.HasPrefix(trimmed, "@@"), strings.HasPrefix(trimmed, "+++"), strings.HasPrefix(trimmed, "---"):
		return toolDetailHeaderLine
	case strings.HasPrefix(trimmed, "+"):
		return toolDetailAddedLine
	case strings.HasPrefix(trimmed, "-"):
		return toolDetailRemovedLine
	case strings.TrimSpace(trimmed) == "":
		return toolDetailBlankLine
	default:
		return toolDetailContextLine
	}
}

func isFileChangeSummaryLine(line string) bool {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return false
	}
	switch fields[0] {
	case "create", "modify", "delete", "changed":
		return true
	default:
		return false
	}
}

func formatToolDetailLine(line string) string {
	if summary, ok := humanFileChangeSummary(line); ok {
		return summary
	}
	return line
}

func humanFileChangeSummary(line string) (string, bool) {
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) < 2 {
		return "", false
	}
	path := strings.Join(fields[1:], " ")
	switch fields[0] {
	case "create":
		return "created file: " + path, true
	case "modify":
		return "modified file: " + path, true
	case "delete":
		return "deleted file: " + path, true
	case "changed":
		return "changed file: " + path, true
	default:
		return "", false
	}
}

func activityDetailStyle(entry message, styles tuitheme.Styles) lipgloss.Style {
	switch entry.kind {
	case thinkingMessage:
		return styles.Thinking.Collapsed
	case permissionMessage:
		if entry.status == "deny" || entry.status == "denied" || entry.status == "error" {
			return styles.Tool.Denied
		}
		return styles.Tool.Done
	default:
		switch entry.status {
		case "failed", "error":
			return styles.Tool.Failed
		case "queued", "running", "started":
			return styles.Tool.Running
		default:
			return styles.Tool.Done
		}
	}
}

func renderActivityChip(item message, focused bool, styles tuitheme.Styles, text string) string {
	style := styles.Tool.Collapsed
	if item.kind == thinkingMessage {
		style = styles.Thinking.Collapsed
	}
	if item.status == "failed" || item.status == "error" {
		style = styles.Tool.Failed
		if item.kind == thinkingMessage {
			style = styles.Thinking.Failed
		}
	}
	if focused {
		style = styles.Focus
	}
	return styles.Render(style, text)
}

func activityChipText(item message, width int) string {
	marker := "▸ "
	icon := activityStatusIcon(item.kind, item.status)
	title := defaultString(item.title, item.text)
	switch item.kind {
	case thinkingMessage:
		title = strings.TrimPrefix(title, "thinking: ")
	case permissionMessage:
		title = compactPermissionTitle(title)
	}
	return truncateText(marker+icon+" "+title, width)
}

func compactPermissionTitle(title string) string {
	title = strings.TrimSpace(title)
	parts := strings.Fields(title)
	if len(parts) < 4 || !strings.EqualFold(parts[0], "permission") {
		return title
	}
	return "Permission " + parts[2] + " " + strings.TrimSuffix(parts[3], ":")
}

func renderWrappedPrefixed(text, prefix, indent string, width int, styles tuitheme.Styles, prefixStyle, textStyle lipgloss.Style) []string {
	textWidth := max(10, contentWidth(width)-runewidth.StringWidth(prefix))
	lines := wrapText(text, textWidth)
	out := make([]string, 0, len(lines))
	out = append(out, renderMaybe(styles, prefixStyle, prefix)+renderMaybe(styles, textStyle, lines[0]))
	for _, line := range lines[1:] {
		out = append(out, indent+renderMaybe(styles, textStyle, line))
	}
	return out
}

func renderMarkdownPrefixed(text, prefix, indent string, width int, styles tuitheme.Styles, prefixStyle lipgloss.Style) []string {
	textWidth := max(10, contentWidth(width)-runewidth.StringWidth(prefix))
	lines := markdownLines(text, textWidth, styles)
	if len(lines) == 0 {
		lines = []string{""}
	}
	out := make([]string, 0, len(lines))
	out = append(out, renderMaybe(styles, prefixStyle, prefix)+lines[0])
	for _, line := range lines[1:] {
		out = append(out, indent+line)
	}
	return out
}

func renderMaybe(styles tuitheme.Styles, style lipgloss.Style, value string) string {
	if styles.Plain {
		return value
	}
	return style.Render(value)
}

func markdownLines(text string, width int, styles tuitheme.Styles) []string {
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return []string{""}
	}
	styleName := styles.Markdown.StyleName
	if styleName == "" {
		styleName = "dark"
	}
	if styles.Plain {
		styleName = "notty"
	}
	renderer, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle(styleName),
		glamour.WithWordWrap(max(10, width)),
	)
	if err != nil {
		return wrapText(text, width)
	}
	rendered, err := renderer.Render(text)
	if err != nil {
		return wrapText(text, width)
	}
	rendered = xansi.Wrap(rendered, max(10, width), " ")
	return trimBlankLines(strings.Split(strings.TrimRight(rendered, "\n"), "\n"))
}

func trimBlankLines(lines []string) []string {
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " \t")
	}
	start := 0
	for start < len(lines) && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	end := len(lines)
	for end > start && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	if start >= end {
		return []string{""}
	}
	lines = lines[start:end]
	if hasSharedLeftMargin(lines, "  ") {
		for i := range lines {
			if strings.TrimSpace(lines[i]) == "" {
				continue
			}
			lines[i] = strings.TrimPrefix(lines[i], "  ")
		}
	}
	return lines
}

func hasSharedLeftMargin(lines []string, margin string) bool {
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !strings.HasPrefix(line, margin) {
			return false
		}
	}
	return len(lines) > 0
}

func (l messageList) texts() []string {
	out := make([]string, len(l.items))
	for i, item := range l.items {
		out[i] = item.text
	}
	return out
}

func (l messageList) copyText(n int) (string, bool) {
	if n <= 0 {
		return "", false
	}
	for _, item := range l.items {
		if item.copyIndex != n {
			continue
		}
		text := messageCopyText(item)
		if text != "" {
			return text, true
		}
		return "", false
	}
	return "", false
}

func (l messageList) lastAssistantText() (string, bool) {
	for i := len(l.items) - 1; i >= 0; i-- {
		if l.items[i].kind == textMessage && l.items[i].role == assistantRole {
			text := messageCopyText(l.items[i])
			if text != "" {
				return text, true
			}
			return "", false
		}
	}
	return "", false
}

func messageCopyText(item message) string {
	if text := strings.TrimSpace(item.text); text != "" {
		return item.text
	}
	if text := strings.TrimSpace(item.detail); text != "" {
		return item.detail
	}
	if text := strings.TrimSpace(item.title); text != "" {
		return item.title
	}
	return ""
}

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

func mergeActivityMessage(existing, incoming message) message {
	if existing.kind == thinkingMessage && incoming.kind == thinkingMessage {
		return mergeThinkingMessage(existing, incoming)
	}
	if existing.kind == toolMessage && incoming.kind == toolMessage {
		return mergeToolMessage(existing, incoming)
	}
	return incoming
}

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
		incoming.detail = truncateToolPartialPreview(existing.detail + incoming.detail)
	}
	if genericRunningToolTitle(incoming) && strings.TrimSpace(existing.title) != "" {
		incoming.title = existing.title
		incoming.text = existing.text
	}
	return incoming
}

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

func genericRunningToolTitle(item message) bool {
	if item.status != "running" {
		return false
	}
	action := strings.TrimSpace(toolAction(item.name))
	return action != "" && strings.TrimSpace(item.title) == action && strings.TrimSpace(item.text) == action
}

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
	incoming.title = "thinking: " + summary
	incoming.text = incoming.title
	return incoming
}

func thinkingDetail(item message) string {
	// Use raw non-empty check so whitespace-only deltas ("\n\n" paragraph
	// breaks) survive the merge — TrimSpace would treat them as missing and
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
	// concatenate normally — TrimSpace here would silently drop the only
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
	if strings.HasPrefix(strings.ToLower(text), "thinking:") {
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
	switch strings.TrimSpace(event.ActivityKind) {
	case "permission":
		source := defaultString(event.Source, "permission")
		toolName := defaultString(event.ToolName, "tool")
		return "permission:" + source + ":" + toolName
	case "notice":
		return "notice:" + defaultString(event.Summary, event.Text)
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

func (l *messageList) reindexCopy() {
	if l.batchDepth > 0 {
		l.copyIndexDirty = true
		return
	}
	idx := 0
	for i := range l.items {
		if l.items[i].kind == textMessage {
			idx++
			l.items[i].copyIndex = idx
		} else {
			l.items[i].copyIndex = 0
		}
	}
	l.copyIndexDirty = false
	l.invalidateRender()
}

func (l *messageList) clampFocus() {
	if _, ok := l.focusTarget(); ok {
		return
	}
	l.focus = -1
	l.entryFocus = -1
}

func visibleStart(totalLines, height, scroll int) int {
	if height <= 0 || height >= totalLines {
		return 0
	}
	maxScroll := totalLines - height
	if scroll < 0 {
		scroll = 0
	}
	if scroll > maxScroll {
		scroll = maxScroll
	}
	start := totalLines - height - scroll
	if start < 0 {
		start = 0
	}
	return start
}

func messagePrefix(role string, styles tuitheme.Styles) (prefix, indent string, prefixStyle lipgloss.Style) {
	switch role {
	case userRole:
		return "› ", "  ", styles.Role.UserPrefix
	case assistantRole:
		return "  ", "  ", styles.Role.AssistantPrefix
	case systemRole:
		return "# ", "  ", styles.Role.SystemPrefix
	case errorRole:
		return "! ", "  ", styles.Role.ErrorPrefix
	default:
		return "# ", "  ", styles.Role.SystemPrefix
	}
}

func normalizeRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case userRole:
		return userRole
	case assistantRole:
		return assistantRole
	case activityRole:
		return activityRole
	case toolRole:
		return toolRole
	case errorRole:
		return errorRole
	default:
		return systemRole
	}
}

func kindForRole(role string) messageKind {
	switch role {
	case activityRole:
		return noticeMessage
	case toolRole:
		return toolMessage
	case systemRole:
		return systemMessage
	case errorRole:
		return errorMessage
	default:
		return textMessage
	}
}

func kindForRoleAndKey(role, key string) messageKind {
	if role == activityRole && strings.HasPrefix(key, "thinking:") {
		return thinkingMessage
	}
	return kindForRole(role)
}

func defaultCollapsed(kind messageKind) bool {
	switch kind {
	case thinkingMessage, toolMessage, permissionMessage, activityGroupMessage:
		return true
	default:
		return false
	}
}

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
			title:     "thinking: " + thinkingSummary(summary),
			status:    "running",
			detail:    detail,
			collapsed: true,
		}
	case "tool":
		text := toolActivityText(event)
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
		text := permissionEventText(event)
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
	case "notice":
		text := activityEventText(event)
		return message{role: activityRole, text: text, kind: noticeMessage, title: text}
	default:
		text := activityEventText(event)
		return message{role: activityRole, text: text, kind: noticeMessage, title: text}
	}
}

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
