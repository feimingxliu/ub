package tui

import "strings"

const userRole = "You"
const assistantRole = "Assistant"
const toolRole = "Tool"
const systemRole = "System"

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

func (l messageList) view() string {
	if len(l.items) == 0 {
		return "No messages yet"
	}

	var b strings.Builder
	for i, item := range l.items {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(item.role)
		b.WriteString(": ")
		b.WriteString(item.text)
	}
	return b.String()
}

func (l messageList) texts() []string {
	out := make([]string, len(l.items))
	for i, item := range l.items {
		out[i] = item.text
	}
	return out
}
