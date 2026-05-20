package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/mattn/go-runewidth"
)

type tuiFrame struct {
	content string
	cursor  *tea.Cursor
}

type footerFrame struct {
	lines     []string
	inputLine int
}

func (m Model) renderFrame() tuiFrame {
	width := contentWidth(m.width)
	footer := m.footerFrame(width)
	footerText := strings.Join(footer.lines, "\n")
	messageText := m.messages.view(width, m.messageViewHeight(footerText), m.clampedScroll(), m.styles)
	messageLines := splitFrameLines(messageText)

	blankLines := 2
	if m.height > 0 {
		blankLines = max(0, m.height-len(messageLines)-len(footer.lines))
	}

	lines := append([]string(nil), messageLines...)
	for range blankLines {
		lines = append(lines, "")
	}
	footerTop := len(lines)
	lines = append(lines, footer.lines...)

	inputY := footerTop + footer.inputLine
	return tuiFrame{
		content: strings.Join(lines, "\n"),
		cursor:  m.frameCursor(inputY),
	}
}

func (m Model) frameCursor(inputY int) *tea.Cursor {
	if m.pending != nil || m.picker != nil || m.sessions != nil {
		return nil
	}
	cur := m.input.Cursor()
	if cur == nil {
		return nil
	}
	cur.Y += inputY
	cur.X = inputCursorX(m.input.Prompt, m.input.Value(), m.input.Position(), m.input.Width())
	return cur
}

func inputCursorX(prompt, value string, position, width int) int {
	runes := []rune(value)
	position = clampInt(position, 0, len(runes))
	x := runewidth.StringWidth(prompt) + runewidth.StringWidth(string(runes[:position]))
	if width > 0 {
		x = min(x, runewidth.StringWidth(prompt)+width)
	}
	return x
}

func (m Model) footerFrame(width int) footerFrame {
	var lines []string
	inputLine := len(lines)
	lines = append(lines, splitFrameLines(m.input.View())...)
	if hint := m.shellHintView(width); hint != "" {
		lines = append(lines, splitFrameLines(hint)...)
	}
	if picker := m.pickerView(width); picker != "" {
		lines = append(lines, splitFrameLines(picker)...)
	} else if picker := m.sessionPickerView(width); picker != "" {
		lines = append(lines, splitFrameLines(picker)...)
	} else if picker := m.filePickerView(width); picker != "" {
		lines = append(lines, splitFrameLines(picker)...)
	} else if suggestions := m.slashSuggestions(width); suggestions != "" {
		lines = append(lines, splitFrameLines(suggestions)...)
	}
	if queued := m.queuedPromptView(width); queued != "" {
		lines = append(lines, splitFrameLines(queued)...)
	}
	if m.pending != nil {
		lines = append(lines, "")
		lines = append(lines, splitFrameLines(m.modal.View())...)
	}
	lines = append(lines, splitFrameLines(m.status.view(width, m.styles))...)
	return footerFrame{lines: lines, inputLine: inputLine}
}

func splitFrameLines(text string) []string {
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}
