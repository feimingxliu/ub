package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

func (m Model) footerView(width int) string {
	return strings.Join(m.footerFrame(width).lines, "\n")
}

func (m Model) showCheatsheet() (tea.Model, tea.Cmd) {
	m.messages.append(systemRole, slashHelp())
	m.scrollToBottom()
	return m, nil
}

func (m Model) statusHelpHit(x, y int) bool {
	if y != frameHeight(m.height)-1 {
		return false
	}
	return m.status.helpHit(contentWidth(m.width), m.styles, x)
}

func (m *Model) toggleMessageAt(x, y int) bool {
	width := contentWidth(m.width)
	footer := m.footerView(width)
	height := m.messageViewHeight(footer)
	return m.messages.toggleAt(width, height, m.clampedScroll(), x, y, m.styles)
}

// MessageTexts returns the rendered message text values for tests.
func (m Model) MessageTexts() []string {
	return m.messages.texts()
}

// InputValue returns the current input value for tests.
func (m Model) InputValue() string {
	return m.input.Value()
}

// Running reports whether an Agent turn is in progress.
func (m Model) Running() bool {
	return m.running
}

// QueuedPrompts returns queued user prompts for tests.
func (m Model) QueuedPrompts() []string {
	return append([]string(nil), m.queuedPrompts...)
}

// Turn returns the current TUI turn number.
func (m Model) Turn() int {
	return m.status.turn
}
