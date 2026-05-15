package tui

import (
	"strings"
)

type modelPicker struct {
	models []string
	index  int
}

func newModelPicker(models []string, current string) *modelPicker {
	picker := &modelPicker{models: append([]string(nil), models...)}
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

func (p *modelPicker) view(width int) string {
	if p == nil || len(p.models) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("select model (enter select, esc cancel)")
	for i, model := range p.models {
		b.WriteByte('\n')
		marker := "  "
		if i == p.index {
			marker = "> "
		}
		b.WriteString(truncateText(marker+model, width))
	}
	return b.String()
}
