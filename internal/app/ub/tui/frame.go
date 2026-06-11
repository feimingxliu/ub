package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	xansi "github.com/charmbracelet/x/ansi"
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
	if m.btw.visible && m.pending == nil && m.pendingAsk == nil && m.pendingLimit == nil {
		return m.renderSideQuestionFrame()
	}
	width := contentWidth(m.width)
	height := frameHeight(m.height)
	footer := m.footerFrame(width)
	footerText := strings.Join(footer.lines, "\n")
	messageText := m.messages.view(width, m.messageViewHeight(footerText), m.clampedScroll(), m.styles)
	messageLines := splitFrameLines(messageText)

	blankLines := max(0, height-len(messageLines)-len(footer.lines))

	lines := append([]string(nil), messageLines...)
	for range blankLines {
		lines = append(lines, "")
	}
	footerTop := len(lines)
	lines = append(lines, footer.lines...)

	inputY := footerTop + footer.inputLine
	lines = padFrameLines(lines, width, height)
	return tuiFrame{
		content: strings.Join(lines, "\n"),
		cursor:  m.frameCursor(inputY),
	}
}

func padFrameLines(lines []string, width, height int) []string {
	if width <= 0 {
		return lines
	}
	padded := make([]string, len(lines))
	for i, line := range lines {
		padded[i] = padFrameLine(line, width)
	}
	for height > 0 && len(padded) < height {
		padded = append(padded, strings.Repeat(" ", width))
	}
	return padded
}

func padFrameLine(line string, width int) string {
	visualWidth := xansi.StringWidth(line)
	if visualWidth >= width {
		return line
	}
	return line + strings.Repeat(" ", width-visualWidth)
}

func (m Model) frameCursor(inputY int) *tea.Cursor {
	if m.pending != nil || m.pendingAsk != nil || m.picker != nil || m.sessions != nil || m.plans != nil || m.rewind != nil || m.btw.visible {
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
	} else if picker := m.planPickerView(width); picker != "" {
		lines = append(lines, splitFrameLines(picker)...)
	} else if picker := m.rewindPickerView(width); picker != "" {
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
	if m.pendingAsk != nil {
		lines = append(lines, "")
		lines = append(lines, splitFrameLines(m.askPrompt.View(width))...)
	}
	if prompt := m.limitPromptView(width); prompt != "" {
		lines = append(lines, "")
		lines = append(lines, splitFrameLines(prompt)...)
	}
	if indicator := m.runIndicatorView(width); indicator != "" {
		lines = append(lines, splitFrameLines(indicator)...)
	}
	if toast := m.toastView(width); toast != "" {
		lines = append(lines, splitFrameLines(toast)...)
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
