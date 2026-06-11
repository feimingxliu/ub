package tui

import (
	"strings"

	"github.com/feimingxliu/ub/internal/app/ub/tui/tuitheme"
)

type modelPicker struct {
	models []string
	index  int
	title  string
}

func newModelPicker(models []string, current string) *modelPicker {
	return newValuePicker(models, current, "select model (enter select, esc cancel)")
}

func newProviderPicker(providers []string, current string) *modelPicker {
	return newValuePicker(providers, current, "select provider (enter select, esc cancel)")
}

func newEffortPicker(efforts []string, current string) *modelPicker {
	return newValuePicker(efforts, current, "select effort (enter select, esc cancel)")
}

func newValuePicker(models []string, current, title string) *modelPicker {
	picker := &modelPicker{models: append([]string(nil), models...), title: title}
	for i, model := range picker.models {
		if model == current {
			picker.index = i
			break
		}
	}
	return picker
}

func (p *modelPicker) selected() string {
	if p == nil || len(p.models) == 0 {
		return ""
	}
	return p.models[p.index]
}

func (p *modelPicker) next() {
	if p == nil || len(p.models) == 0 {
		return
	}
	p.index = (p.index + 1) % len(p.models)
}

func (p *modelPicker) previous() {
	if p == nil || len(p.models) == 0 {
		return
	}
	p.index = (p.index + len(p.models) - 1) % len(p.models)
}

func (p *modelPicker) view(width int, styles tuitheme.Styles) string {
	if p == nil || len(p.models) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(styles.Render(styles.Picker.Title, truncateText("◇ "+p.title, width)))
	for i, model := range p.models {
		b.WriteByte('\n')
		marker := "  "
		if i == p.index {
			marker = "> "
		}
		line := truncateText(marker+model, width)
		if i == p.index {
			b.WriteString(styles.Render(styles.Picker.Selected, line))
			continue
		}
		b.WriteString(styles.Render(styles.Picker.Item, line))
	}
	return b.String()
}
