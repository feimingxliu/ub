package tui

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/feimingxliu/ub/internal/app/ub/tui/tuitheme"
	"github.com/feimingxliu/ub/internal/pkg/tool/plan"
)

type planPicker struct {
	all   []plan.Info
	plans []plan.Info
	query string
	index int
}

func newPlanPicker(plans []plan.Info) *planPicker {
	picker := &planPicker{all: append([]plan.Info(nil), plans...)}
	picker.refilter()
	return picker
}

func (p *planPicker) selected() plan.Info {
	if p == nil || len(p.plans) == 0 {
		return plan.Info{}
	}
	return p.plans[p.index]
}

func (p *planPicker) next() {
	if p == nil || len(p.plans) == 0 {
		return
	}
	p.index = (p.index + 1) % len(p.plans)
}

func (p *planPicker) previous() {
	if p == nil || len(p.plans) == 0 {
		return
	}
	p.index = (p.index + len(p.plans) - 1) % len(p.plans)
}

func (p *planPicker) appendRune(r rune) {
	if p == nil || !unicode.IsPrint(r) {
		return
	}
	p.query += string(r)
	p.refilter()
}

func (p *planPicker) backspace() {
	if p == nil || p.query == "" {
		return
	}
	runes := []rune(p.query)
	p.query = string(runes[:len(runes)-1])
	p.refilter()
}

func (p *planPicker) clearQuery() {
	if p == nil || p.query == "" {
		return
	}
	p.query = ""
	p.refilter()
}

func (p *planPicker) refilter() {
	if p == nil {
		return
	}
	p.plans = nil
	query := strings.TrimSpace(p.query)
	for _, item := range p.all {
		if query == "" || planMatchesQuery(item, query) {
			p.plans = append(p.plans, item)
		}
	}
	p.index = 0
	if len(p.plans) > 0 && p.index >= len(p.plans) {
		p.index = len(p.plans) - 1
	}
}

func (p *planPicker) view(width int, styles tuitheme.Styles) string {
	if p == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(styles.Render(styles.Picker.Title, truncateText("◇ select plan (type filter, enter edit, esc cancel)", width)))
	b.WriteByte('\n')
	filter := p.query
	if filter == "" {
		filter = "all"
	}
	b.WriteString(styles.Render(styles.Picker.Item, truncateText("  filter: "+filter, width)))
	if len(p.plans) == 0 {
		b.WriteByte('\n')
		b.WriteString(styles.Render(styles.Picker.Empty, truncateText("  no matching plans", width)))
		return b.String()
	}
	for i, item := range p.plans {
		b.WriteByte('\n')
		marker := "  "
		if i == p.index {
			marker = "> "
		}
		status := item.Status
		if status == "" {
			status = "-"
		}
		updated := "-"
		if !item.UpdatedAt.IsZero() {
			updated = item.UpdatedAt.Local().Format("2006-01-02 15:04")
		}
		line := truncateText(fmt.Sprintf("%s%s  %s  %s  %d steps  %s", marker, item.ID, updated, status, item.StepCount, defaultString(item.Title, "(untitled)")), width)
		if i == p.index {
			b.WriteString(styles.Render(styles.Picker.Selected, line))
			continue
		}
		b.WriteString(styles.Render(styles.Picker.Item, line))
	}
	return b.String()
}

func planMatchesQuery(item plan.Info, query string) bool {
	haystacks := []string{
		item.ID,
		item.Title,
		item.Status,
		item.Path,
	}
	if !item.UpdatedAt.IsZero() {
		haystacks = append(haystacks, item.UpdatedAt.Local().Format("2006-01-02 15:04"))
	}
	for _, value := range haystacks {
		if fuzzyMatch(value, query) {
			return true
		}
	}
	return false
}
