package tui

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/feimingxliu/ub/internal/tui/tuitheme"
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
	p.index = (p.index + 1) % len(p.sessions)
}

func (p *sessionPicker) previous() {
	if p == nil || len(p.sessions) == 0 {
		return
	}
	p.index = (p.index + len(p.sessions) - 1) % len(p.sessions)
}

func (p *sessionPicker) appendRune(r rune) {
	if p == nil || !unicode.IsPrint(r) {
		return
	}
	p.query += string(r)
	p.refilter()
}

func (p *sessionPicker) backspace() {
	if p == nil || p.query == "" {
		return
	}
	runes := []rune(p.query)
	p.query = string(runes[:len(runes)-1])
	p.refilter()
}

func (p *sessionPicker) clearQuery() {
	if p == nil || p.query == "" {
		return
	}
	p.query = ""
	p.refilter()
}

func (p *sessionPicker) refilter() {
	if p == nil {
		return
	}
	p.sessions = nil
	query := strings.TrimSpace(p.query)
	for _, sess := range p.all {
		if query == "" || sessionMatchesQuery(sess, query) {
			p.sessions = append(p.sessions, sess)
		}
	}
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
	b.WriteString(styles.Render(styles.Picker.Title, truncateText("◇ select session (type filter, enter switch, esc cancel)", width)))
	b.WriteByte('\n')
	filter := p.query
	if filter == "" {
		filter = "all"
	}
	b.WriteString(styles.Render(styles.Picker.Item, truncateText("  filter: "+filter, width)))
	if len(p.sessions) == 0 {
		b.WriteByte('\n')
		b.WriteString(styles.Render(styles.Picker.Empty, truncateText("  no matching sessions", width)))
		return b.String()
	}
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
		provider := sess.Provider
		if provider == "" {
			provider = "-"
		}
		updated := "-"
		if !sess.UpdatedAt.IsZero() {
			updated = sess.UpdatedAt.Local().Format("2006-01-02 15:04")
		}
		line := truncateText(fmt.Sprintf("%s%s %s  %s  %s/%s  %s", marker, current, sess.ID, updated, provider, model, title), width)
		if i == p.index {
			b.WriteString(styles.Render(styles.Picker.Selected, line))
			continue
		}
		b.WriteString(styles.Render(styles.Picker.Item, line))
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
