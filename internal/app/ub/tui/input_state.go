package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/mattn/go-runewidth"
)

func promptHistoryFromMessages(messages []InitialMessage) []string {
	var out []string
	for _, msg := range messages {
		if normalizeRole(msg.Role) != userRole {
			continue
		}
		text := strings.TrimSpace(msg.Text)
		if text == "" {
			continue
		}
		out = append(out, text)
	}
	return out
}

func (m *Model) recordPromptHistory(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		m.resetPromptHistoryNavigation()
		return
	}
	if len(m.history) == 0 || m.history[len(m.history)-1] != text {
		m.history = append(m.history, text)
	}
	m.resetPromptHistoryNavigation()
}

func (m *Model) queueInput() bool {
	text := strings.TrimSpace(m.input.Value())
	if text == "" {
		if m.queueIdx >= 0 {
			m.saveQueuedPromptEdit()
			return true
		}
		return false
	}
	if strings.HasPrefix(text, "/") {
		return false
	}
	if isShellInput(text) {
		return false
	}
	if m.queueIdx >= 0 && m.queueIdx < len(m.queuedPrompts) {
		m.queuedPrompts[m.queueIdx] = text
		m.resetQueuedPromptNavigation()
	} else {
		m.queuedPrompts = append(m.queuedPrompts, text)
	}
	m.input.SetValue("")
	m.files = nil
	m.input.CursorEnd()
	m.resetPromptHistoryNavigation()
	return true
}

func (m *Model) saveQueuedPromptEdit() bool {
	if m.queueIdx < 0 || m.queueIdx >= len(m.queuedPrompts) {
		return false
	}
	text := strings.TrimSpace(m.input.Value())
	if text == "" {
		m.queuedPrompts = append(m.queuedPrompts[:m.queueIdx], m.queuedPrompts[m.queueIdx+1:]...)
		m.input.SetValue(m.queueDraft)
		m.input.CursorEnd()
		m.resetQueuedPromptNavigation()
		return true
	}
	m.queuedPrompts[m.queueIdx] = text
	return false
}

func (m *Model) navigateQueuedPrompts(delta int) bool {
	if !m.running || len(m.queuedPrompts) == 0 || delta == 0 {
		return false
	}
	if m.queueIdx >= 0 {
		if m.saveQueuedPromptEdit() {
			return true
		}
	} else if delta < 0 {
		m.queueDraft = m.input.Value()
	} else {
		return false
	}

	switch {
	case delta < 0:
		if m.queueIdx < 0 || m.queueIdx >= len(m.queuedPrompts) {
			m.queueIdx = len(m.queuedPrompts) - 1
		} else if m.queueIdx > 0 {
			m.queueIdx--
		}
	case delta > 0:
		if m.queueIdx < len(m.queuedPrompts)-1 {
			m.queueIdx++
		} else {
			m.input.SetValue(m.queueDraft)
			m.input.CursorEnd()
			m.resetQueuedPromptNavigation()
			return true
		}
	}
	m.input.SetValue(m.queuedPrompts[m.queueIdx])
	m.input.CursorEnd()
	return true
}

func (m *Model) resetQueuedPromptNavigation() {
	m.queueIdx = -1
	m.queueDraft = ""
}

func (m Model) startNextQueuedPrompt() (tea.Model, tea.Cmd) {
	if len(m.queuedPrompts) == 0 {
		return m, nil
	}
	restoreInput := m.input.Value()
	if m.queueIdx >= 0 {
		restoreInput = m.queueDraft
		m.saveQueuedPromptEdit()
	}
	if len(m.queuedPrompts) == 0 {
		m.input.SetValue(restoreInput)
		m.input.CursorEnd()
		m.resetQueuedPromptNavigation()
		return m, nil
	}
	next := m.queuedPrompts[0]
	m.queuedPrompts = append([]string(nil), m.queuedPrompts[1:]...)
	m.input.SetValue(restoreInput)
	m.input.CursorEnd()
	m.resetQueuedPromptNavigation()
	return m.startPrompt(next, false)
}

func (m Model) queuedPromptView(width int) string {
	if len(m.queuedPrompts) == 0 {
		return ""
	}
	prefix := fmt.Sprintf("queued: %d", len(m.queuedPrompts))
	index := 0
	label := "next"
	if m.queueIdx >= 0 && m.queueIdx < len(m.queuedPrompts) {
		index = m.queueIdx
		label = fmt.Sprintf("editing %d/%d", m.queueIdx+1, len(m.queuedPrompts))
	}
	rail := "┃ "
	bodyWidth := max(1, contentWidth(width)-runewidth.StringWidth(rail)-2)
	body := truncateText(fmt.Sprintf("%s%s%s: %s", prefix, statusSeparator, label, m.queuedPrompts[index]), bodyWidth)
	return m.styles.Render(m.styles.SubtleLine, rail) + m.styles.Render(m.styles.Status.Segment, body)
}

func (m *Model) scrollMessages(delta int) {
	if delta == 0 {
		return
	}
	m.scroll += delta
	maxScroll := m.maxMessageScroll()
	if m.scroll < 0 {
		m.scroll = 0
	}
	if m.scroll > maxScroll {
		m.scroll = maxScroll
	}
}

func (m *Model) scrollToBottom() {
	m.scroll = 0
}

func (m *Model) scrollToTop() {
	m.scroll = m.maxMessageScroll()
}

func (m *Model) scrollFocusedMessageIntoView() {
	width := contentWidth(m.width)
	footer := m.footerView(width)
	height := m.messageViewHeight(footer)
	if height <= 0 {
		return
	}
	line, total, ok := m.messages.focusedLine(width, m.styles)
	if !ok {
		return
	}
	if total <= height {
		m.scroll = 0
		return
	}
	maxScroll := total - height
	start := visibleStart(total, height, m.clampedScroll())
	switch {
	case line < start:
		m.scroll = maxScroll - line
	case line >= start+height:
		m.scroll = maxScroll - (line - height + 1)
	default:
		return
	}
	if m.scroll < 0 {
		m.scroll = 0
	}
	if m.scroll > maxScroll {
		m.scroll = maxScroll
	}
}

func (m Model) clampedScroll() int {
	if m.scroll <= 0 {
		return 0
	}
	maxScroll := m.maxMessageScroll()
	if m.scroll > maxScroll {
		return maxScroll
	}
	return m.scroll
}

func (m Model) maxMessageScroll() int {
	width := contentWidth(m.width)
	footer := m.footerView(width)
	height := m.messageViewHeight(footer)
	if height <= 0 {
		return 0
	}
	lines := len(m.messages.render(width, m.styles).lines)
	if lines <= height {
		return 0
	}
	return lines - height
}

func (m Model) pageScrollLines() int {
	height := m.messageViewHeight(m.footerView(contentWidth(m.width)))
	if height <= 1 {
		return 1
	}
	return height - 1
}

func (m Model) messageViewHeight(footer string) int {
	return max(1, frameHeight(m.height)-lineCount(footer)-2)
}

func lineCount(text string) int {
	if text == "" {
		return 0
	}
	return strings.Count(text, "\n") + 1
}

func (m *Model) navigatePromptHistory(delta int) bool {
	if len(m.history) == 0 || delta == 0 {
		return false
	}
	switch {
	case delta < 0:
		if m.histIdx < 0 {
			m.draft = m.input.Value()
			m.histIdx = len(m.history) - 1
		} else if m.histIdx > 0 {
			m.histIdx--
		}
	case delta > 0:
		if m.histIdx < 0 {
			return false
		}
		if m.histIdx < len(m.history)-1 {
			m.histIdx++
		} else {
			m.input.SetValue(m.draft)
			m.input.CursorEnd()
			m.resetPromptHistoryNavigation()
			return true
		}
	}
	m.input.SetValue(m.history[m.histIdx])
	m.input.CursorEnd()
	return true
}

func (m *Model) resetPromptHistoryNavigation() {
	m.histIdx = -1
	m.draft = ""
}
