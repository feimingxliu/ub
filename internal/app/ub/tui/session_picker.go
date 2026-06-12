package tui

import (
	"fmt"
	"strings"

	"github.com/feimingxliu/ub/internal/app/ub/tui/tuitheme"
)

type sessionPicker struct {
	all      []SessionInfo
	sessions []SessionInfo
	query    string
	index    int
}

func newSessionPicker(sessions []SessionInfo) *sessionPicker {
	picker := &sessionPicker{all: append([]SessionInfo(nil), sessions...)}
	picker.refilter()
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
	p.index = nextPickerIndex(p.index, len(p.sessions))
}

func (p *sessionPicker) previous() {
	if p == nil || len(p.sessions) == 0 {
		return
	}
	p.index = previousPickerIndex(p.index, len(p.sessions))
}

func (p *sessionPicker) appendRune(r rune) {
	if p == nil {
		return
	}
	appendPickerQueryRuneAndRefilter(&p.query, r, p.refilter)
}

func (p *sessionPicker) backspace() {
	if p == nil {
		return
	}
	backspacePickerQueryAndRefilter(&p.query, p.refilter)
}

func (p *sessionPicker) clearQuery() {
	if p == nil {
		return
	}
	clearPickerQueryAndRefilter(&p.query, p.refilter)
}

func (p *sessionPicker) refilter() {
	if p == nil {
		return
	}
	p.sessions = filterPickerItems(p.all, p.query, sessionMatchesQuery)
	p.index = 0
	for i, sess := range p.sessions {
		if sess.Current {
			p.index = i
			break
		}
	}
	if len(p.sessions) > 0 && p.index >= len(p.sessions) {
		p.index = len(p.sessions) - 1
	}
}

func (p *sessionPicker) view(width int, styles tuitheme.Styles) string {
	if p == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(renderPickerTitle(styles, width, "◇ select session (type filter, enter switch, esc cancel)"))
	b.WriteByte('\n')
	b.WriteString(renderPickerItem(styles, width, "  filter: "+pickerFilterLabel(p.query)))
	if len(p.sessions) == 0 {
		b.WriteByte('\n')
		b.WriteString(renderPickerEmpty(styles, width, "  no matching sessions"))
		return b.String()
	}
	for i, sess := range p.sessions {
		b.WriteByte('\n')
		selected := i == p.index
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
		provider := sess.Provider
		if provider == "" {
			provider = "-"
		}
		updated := "-"
		if !sess.UpdatedAt.IsZero() {
			updated = sess.UpdatedAt.Local().Format("2006-01-02 15:04")
		}
		text := fmt.Sprintf("%s %s  %s  %s/%s  %s", current, sess.ID, updated, provider, model, title)
		b.WriteString(renderPickerChoiceLine(styles, width, selected, text))
	}
	return b.String()
}

func sessionMatchesQuery(sess SessionInfo, query string) bool {
	haystacks := []string{
		sess.ID,
		sess.Title,
		sess.Provider,
		sess.Model,
	}
	if !sess.UpdatedAt.IsZero() {
		haystacks = append(haystacks, sess.UpdatedAt.Local().Format("2006-01-02 15:04"))
	}
	for _, value := range haystacks {
		if fuzzyMatch(value, query) {
			return true
		}
	}
	return false
}

func fuzzyMatch(value, query string) bool {
	value = strings.ToLower(value)
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return true
	}
	if strings.Contains(value, query) {
		return true
	}
	needle := []rune(query)
	if len(needle) == 0 {
		return true
	}
	pos := 0
	for _, r := range value {
		if r == needle[pos] {
			pos++
			if pos == len(needle) {
				return true
			}
		}
	}
	return false
}
