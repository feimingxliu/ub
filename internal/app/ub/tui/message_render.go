package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/mattn/go-runewidth"

	"github.com/feimingxliu/ub/internal/app/ub/tui/tuitheme"
)

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

	contentW := contentWidth(width)
	out := make([]string, 0, len(l.items)*3)
	spans := make([]messageSpan, 0, len(l.items))
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
		out = append(out, l.renderItemCached(item, i == l.focus && l.entryFocus < 0, width, styles)...)
		spans = append(spans, messageSpan{itemIndex: i, start: start, end: len(out), endCol: contentW})
		i++
	}
	rendered := renderedMessages{lines: out, spans: spans}
	if l.renderCache != nil {
		if len(l.renderCache) >= maxRenderCacheEntries {
			for key := range l.renderCache {
				delete(l.renderCache, key)
				break
			}
		}
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
	prefixWidth := runewidth.StringWidth(prefix)
	indent := strings.Repeat(" ", prefixWidth)
	textWidth := max(10, contentWidth(width)-prefixWidth)
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
	detail := truncateExpandedDetailForDisplay(expandedDetail(item))
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
		if detail := truncateExpandedDetailForDisplay(expandedDetail(entry)); detail != "" && (!twoStageDetails || !entry.collapsed) {
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
