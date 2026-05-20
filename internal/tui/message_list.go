package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"

	"github.com/feimingxliu/ub/internal/tui/tuitheme"
)

const userRole = "user"
const assistantRole = "assistant"
const activityRole = "activity"
const toolRole = "tool"
const systemRole = "system"
const errorRole = "error"

const maxThinkingSummaryRunes = 180

const (
	thinkingGroupName = "thinking"
	toolGroupName     = "tool"
)

type messageKind string

const (
	textMessage          messageKind = "text"
	thinkingMessage      messageKind = "thinking"
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
	items []message
	focus int
}

type renderedMessages struct {
	lines []string
	spans []messageSpan
}

type messageSpan struct {
	itemIndex int
	start     int
	end       int
	startCol  int
	endCol    int
}

func newMessageList() messageList {
	return messageList{focus: -1}
}

func (l *messageList) append(role, text string) {
	l.items = append(l.items, message{
		role:      role,
		text:      text,
		kind:      kindForRole(role),
		title:     text,
		collapsed: defaultCollapsed(kindForRole(role)),
	})
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
	l.clampFocus()
}

func (l *messageList) appendOrUpdateBlock(block message) {
	if strings.TrimSpace(block.key) == "" {
		l.items = append(l.items, block)
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
			l.clampFocus()
			return
		}
	}
	l.items = append(l.items, block)
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
		l.clampFocus()
		return
	}
}

func (l *messageList) clear() {
	l.items = nil
	l.focus = -1
}

func (l *messageList) load(messages []InitialMessage) {
	l.items = nil
	l.focus = -1
	for _, msg := range messages {
		role := normalizeRole(msg.Role)
		if strings.TrimSpace(msg.Text) == "" {
			continue
		}
		l.append(role, msg.Text)
	}
}

func (l *messageList) startAssistant() {
	l.items = append(l.items, message{role: assistantRole, kind: textMessage})
	l.clampFocus()
}

func (l *messageList) appendAssistantDelta(text string) {
	if len(l.items) == 0 || l.items[len(l.items)-1].role != assistantRole {
		l.startAssistant()
	}
	l.items[len(l.items)-1].text += text
	l.items[len(l.items)-1].title = l.items[len(l.items)-1].text
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
		if span.itemIndex < 0 || span.itemIndex >= len(l.items) || !l.items[span.itemIndex].collapsible() {
			return false
		}
		l.focus = span.itemIndex
		l.items[span.itemIndex].collapsed = !l.items[span.itemIndex].collapsed
		return true
	}
	return false
}

func (l messageList) view(width, height, scroll int, styles tuitheme.Styles) string {
	lines := l.render(width, styles).lines
	if len(lines) == 0 {
		return styles.Render(styles.Muted, "No messages yet")
	}
	if height <= 0 || height >= len(lines) {
		return strings.Join(lines, "\n")
	}
	start := visibleStart(len(lines), height, scroll)
	return strings.Join(lines[start:start+height], "\n")
}

func (l messageList) lines(width int) []string {
	return l.render(width, tuitheme.Plain()).lines
}

func (l messageList) render(width int, styles tuitheme.Styles) renderedMessages {
	if len(l.items) == 0 {
		return renderedMessages{}
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
		out = append(out, l.renderItem(item, i == l.focus, width, styles)...)
		spans = append(spans, messageSpan{itemIndex: i, start: start, end: len(out), endCol: contentWidth(width)})
		i++
	}
	return renderedMessages{lines: out, spans: spans}
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
	const gap = "  "
	gapWidth := runewidth.StringWidth(gap)

	var lines []string
	var spans []messageSpan
	line := ""
	lineWidth := 0
	for i := startIndex; i < len(l.items); i++ {
		item := l.items[i]
		if !item.compactActivity() {
			return appendCompactLine(lines, line), spans, i
		}
		plain := activityChipText(item, max(10, min(maxWidth, 34)))
		chipWidth := runewidth.StringWidth(plain)
		if chipWidth > maxWidth {
			plain = truncateText(plain, maxWidth)
			chipWidth = runewidth.StringWidth(plain)
		}
		if line != "" && lineWidth+gapWidth+chipWidth > maxWidth {
			lines = append(lines, line)
			line = ""
			lineWidth = 0
		}
		startCol := lineWidth
		if line != "" {
			line += gap
			lineWidth += gapWidth
			startCol = lineWidth
		}
		line += renderActivityChip(item, i == l.focus, styles, plain)
		lineWidth += chipWidth
		spans = append(spans, messageSpan{
			itemIndex: i,
			start:     startLine + len(lines),
			end:       startLine + len(lines) + 1,
			startCol:  startCol,
			endCol:    startCol + chipWidth,
		})
	}
	return appendCompactLine(lines, line), spans, len(l.items)
}

func appendCompactLine(lines []string, line string) []string {
	if line != "" {
		return append(lines, line)
	}
	return lines
}

func (l messageList) renderItem(item message, focused bool, width int, styles tuitheme.Styles) []string {
	switch item.kind {
	case activityGroupMessage:
		return renderActivityGroupBlock(item, focused, width, styles)
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

func renderTextBlock(item message, width int, styles tuitheme.Styles) []string {
	prefix, indent, prefixStyle := messagePrefix(item.role, styles)
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
	if focused {
		style = activity.Focus
	}
	out := []string{styles.Render(style, line)}
	if item.collapsed {
		return out
	}
	detail := expandedDetail(item)
	if detail == "" {
		detail = item.text
	}
	detailLines := wrapText(detail, max(10, contentWidth(width)-2))
	for _, line := range detailLines {
		out = append(out, styles.Render(activity.Detail, "└ "+line))
	}
	return out
}

func renderActivityGroupBlock(item message, focused bool, width int, styles tuitheme.Styles) []string {
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
	if item.collapsed {
		return out
	}
	if len(item.entries) == 0 {
		return append(out, styles.Render(styles.Tool.Detail, "└ no activity"))
	}
	for _, entry := range item.entries {
		entryLine := activityEntryLine(entry, contentWidth(width)-2)
		out = append(out, styles.Render(activityDetailStyle(entry, styles), "└ "+entryLine))
		if detail := expandedDetail(entry); detail != "" {
			for _, detailLine := range wrapText(detail, max(10, contentWidth(width)-4)) {
				out = append(out, styles.Render(styles.Tool.Detail, "  "+detailLine))
			}
		}
	}
	return out
}

func activityEntryLine(entry message, width int) string {
	icon := activityStatusIcon(entry.kind, entry.status)
	title := defaultString(entry.title, entry.text)
	return truncateText(icon+" "+title, width)
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
	if !strings.HasPrefix(title, "permission ") {
		return title
	}
	parts := strings.Fields(strings.TrimPrefix(title, "permission "))
	if len(parts) < 3 {
		return title
	}
	return "permission " + parts[1] + " " + strings.TrimSuffix(parts[2], ":")
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
	return incoming
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
	if strings.TrimSpace(item.detail) != "" {
		return item.detail
	}
	title := defaultString(item.title, item.text)
	if isPlaceholderActivityTitle(title) {
		return ""
	}
	return stripThinkingPrefix(title)
}

func appendThinkingDetail(existing, incoming string) string {
	if strings.TrimSpace(incoming) == "" {
		return existing
	}
	if strings.TrimSpace(existing) == "" {
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
	title = strings.TrimPrefix(title, "Starting job... ")
	title = strings.TrimPrefix(title, "Reading job output... ")
	title = strings.TrimPrefix(title, "Stopping job... ")
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
	hasQueued := false
	for _, entry := range entries {
		switch entry.status {
		case "failed", "error", "deny", "denied":
			return "failed"
		case "running", "started":
			return "running"
		case "queued":
			hasQueued = true
		}
	}
	if hasQueued {
		return "queued"
	}
	return "done"
}

func (l *messageList) clampFocus() {
	if l.focus >= 0 && l.focus < len(l.items) && l.items[l.focus].collapsible() {
		return
	}
	l.focus = -1
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
		detail := defaultString(event.Content, defaultString(event.Text, summary))
		return message{
			role:      activityRole,
			text:      activityEventText(event),
			key:       activityEventKey(event),
			kind:      thinkingMessage,
			title:     "thinking: " + summary,
			status:    "running",
			detail:    detail,
			collapsed: true,
		}
	case "tool":
		text := toolActivityText(event)
		return message{
			role:      activityRole,
			text:      text,
			key:       activityEventKey(event),
			kind:      toolMessage,
			title:     text,
			name:      defaultString(event.ToolName, "tool"),
			status:    defaultString(event.Status, "done"),
			detail:    event.Content,
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
	if detail != "" {
		return detail
	}
	if item.status != "" {
		return "status: " + item.status
	}
	return ""
}
