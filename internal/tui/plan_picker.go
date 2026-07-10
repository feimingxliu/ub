package tui

import (
	"fmt"
	"strings"

	"github.com/feimingxliu/ub/internal/tool/plan"
	"github.com/feimingxliu/ub/internal/tui/theme"
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
	p.index = nextPickerIndex(p.index, len(p.plans))
}

func (p *planPicker) previous() {
	if p == nil || len(p.plans) == 0 {
		return
	}
	p.index = previousPickerIndex(p.index, len(p.plans))
}

func (p *planPicker) appendRune(r rune) {
	if p == nil {
		return
	}
	appendPickerQueryRuneAndRefilter(&p.query, r, p.refilter)
}

func (p *planPicker) backspace() {
	if p == nil {
		return
	}
	backspacePickerQueryAndRefilter(&p.query, p.refilter)
}

func (p *planPicker) clearQuery() {
	if p == nil {
		return
	}
	clearPickerQueryAndRefilter(&p.query, p.refilter)
}

func (p *planPicker) refilter() {
	if p == nil {
		return
	}
	p.plans = filterPickerItems(p.all, p.query, planMatchesQuery)
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
	b.WriteString(renderPickerTitle(styles, width, "◇ select plan (type filter, enter edit, esc cancel)"))
	b.WriteByte('\n')
	b.WriteString(renderPickerItem(styles, width, "  filter: "+pickerFilterLabel(p.query)))
	if len(p.plans) == 0 {
		b.WriteByte('\n')
		b.WriteString(renderPickerEmpty(styles, width, "  no matching plans"))
		return b.String()
	}
	for i, item := range p.plans {
		b.WriteByte('\n')
		selected := i == p.index
		status := item.Status
		if status == "" {
			status = "-"
		}
		updated := "-"
		if !item.UpdatedAt.IsZero() {
			updated = item.UpdatedAt.Local().Format("2006-01-02 15:04")
		}
		text := fmt.Sprintf("%s  %s  %s  %d steps  %s", item.ID, updated, status, item.StepCount, defaultString(item.Title, "(untitled)"))
		b.WriteString(renderPickerChoiceLine(styles, width, selected, text))
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
