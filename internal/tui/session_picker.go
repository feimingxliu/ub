package tui

import (
	"fmt"
	"strings"
)

type sessionPicker struct {
	sessions []SessionInfo
	index    int
}

func newSessionPicker(sessions []SessionInfo) *sessionPicker {
	picker := &sessionPicker{sessions: append([]SessionInfo(nil), sessions...)}
	for i, sess := range picker.sessions {
		if sess.Current {
			picker.index = i
			break
		}
	}
	return picker
}

func (p *sessionPicker) selected() SessionInfo {
	if p == nil || len(p.sessions) == 0 {
		return SessionInfo{}
	}
	return p.sessions[p.index]
}

func (p *sessionPicker) next() {
	if p == nil || len(p.sessions) == 0 {
		return
	}
	p.index = (p.index + 1) % len(p.sessions)
}

func (p *sessionPicker) previous() {
	if p == nil || len(p.sessions) == 0 {
		return
	}
	p.index = (p.index + len(p.sessions) - 1) % len(p.sessions)
}

func (p *sessionPicker) view(width int) string {
	if p == nil || len(p.sessions) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("select session (enter switch, esc cancel)")
	for i, sess := range p.sessions {
		b.WriteByte('\n')
		marker := "  "
		if i == p.index {
			marker = "> "
		}
		current := " "
		if sess.Current {
			current = "*"
		}
		title := sess.Title
		if title == "" {
			title = "(untitled)"
		}
		model := sess.Model
		if model == "" {
			model = "-"
		}
		updated := "-"
		if !sess.UpdatedAt.IsZero() {
			updated = sess.UpdatedAt.Local().Format("2006-01-02 15:04")
		}
		b.WriteString(truncateText(fmt.Sprintf("%s%s %s  %s  %s  %s", marker, current, sess.ID, updated, model, title), width))
	}
	return b.String()
}
