package tui

import "strings"

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
