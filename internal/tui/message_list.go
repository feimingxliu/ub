package tui

import (
	"strings"

	"github.com/mattn/go-runewidth"
)

const userRole = "user"
const assistantRole = "assistant"
const toolRole = "tool"
const systemRole = "system"
const errorRole = "error"

type message struct {
	role string
	text string
}

type messageList struct {
	items []message
}

func newMessageList() messageList {
	return messageList{}
}

func (l *messageList) append(role, text string) {
	l.items = append(l.items, message{role: role, text: text})
}

func (l *messageList) clear() {
	l.items = nil
}

func (l *messageList) load(messages []InitialMessage) {
	l.items = nil
	for _, msg := range messages {
		role := normalizeRole(msg.Role)
		if strings.TrimSpace(msg.Text) == "" {
			continue
		}
		l.append(role, msg.Text)
	}
}

func (l *messageList) startAssistant() {
	l.items = append(l.items, message{role: assistantRole})
}

func (l *messageList) appendAssistantDelta(text string) {
	if len(l.items) == 0 || l.items[len(l.items)-1].role != assistantRole {
		l.startAssistant()
	}
	l.items[len(l.items)-1].text += text
}

func (l *messageList) appendToolStatus(name, state string) {
	if strings.TrimSpace(name) == "" {
		name = "tool"
	}
	l.append(toolRole, "tool "+name+" "+state)
}

func (l messageList) view(width, height, scroll int) string {
	lines := l.lines(width)
	if len(lines) == 0 {
		return "No messages yet"
	}
	if height <= 0 || height >= len(lines) {
		return strings.Join(lines, "\n")
	}
	maxScroll := len(lines) - height
	if scroll < 0 {
		scroll = 0
	}
	if scroll > maxScroll {
		scroll = maxScroll
	}
	start := len(lines) - height - scroll
	if start < 0 {
		start = 0
	}
	return strings.Join(lines[start:start+height], "\n")
}

func (l messageList) lines(width int) []string {
	if len(l.items) == 0 {
		return nil
	}

	var out []string
	for i, item := range l.items {
		if i > 0 {
			out = append(out, "")
		}
		prefix, indent := messagePrefix(item.role)
		textWidth := max(10, contentWidth(width)-runewidth.StringWidth(prefix))
		lines := wrapText(item.text, textWidth)
		out = append(out, prefix+lines[0])
		for _, line := range lines[1:] {
			out = append(out, indent+line)
		}
	}
	return out
}

func (l messageList) texts() []string {
	out := make([]string, len(l.items))
	for i, item := range l.items {
		out[i] = item.text
	}
	return out
}

func messagePrefix(role string) (prefix, indent string) {
	switch role {
	case userRole:
		return "> ", "  "
	case assistantRole:
		return "  ", "  "
	case toolRole:
		return "$ ", "  "
	case systemRole:
		return "# ", "  "
	case errorRole:
		return "! ", "  "
	default:
		return "# ", "  "
	}
}

func normalizeRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case userRole:
		return userRole
	case assistantRole:
		return assistantRole
	case toolRole:
		return toolRole
	case errorRole:
		return errorRole
	default:
		return systemRole
	}
}
