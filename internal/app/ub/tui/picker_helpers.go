package tui

import (
	"strings"
	"unicode"

	"github.com/feimingxliu/ub/internal/app/ub/tui/tuitheme"
)

func nextPickerIndex(index, length int) int {
	if length == 0 {
		return 0
	}
	return (selectedIndex(index, length) + 1) % length
}

func previousPickerIndex(index, length int) int {
	if length == 0 {
		return 0
	}
	return (selectedIndex(index, length) + length - 1) % length
}

func pickerMarker(selected bool) string {
	if selected {
		return "> "
	}
	return "  "
}

func appendPickerQueryRune(query *string, r rune) bool {
	if query == nil || !unicode.IsPrint(r) {
		return false
	}
	*query += string(r)
	return true
}

func backspacePickerQuery(query *string) bool {
	if query == nil || *query == "" {
		return false
	}
	runes := []rune(*query)
	*query = string(runes[:len(runes)-1])
	return true
}

func clearPickerQuery(query *string) bool {
	if query == nil || *query == "" {
		return false
	}
	*query = ""
	return true
}

func appendPickerQueryRuneAndRefilter(query *string, r rune, refilter func()) {
	if appendPickerQueryRune(query, r) {
		refilter()
	}
}

func backspacePickerQueryAndRefilter(query *string, refilter func()) {
	if backspacePickerQuery(query) {
		refilter()
	}
}

func clearPickerQueryAndRefilter(query *string, refilter func()) {
	if clearPickerQuery(query) {
		refilter()
	}
}

func filterPickerItems[T any](items []T, query string, matches func(T, string) bool) []T {
	query = strings.TrimSpace(query)
	out := make([]T, 0, len(items))
	for _, item := range items {
		if query == "" || matches(item, query) {
			out = append(out, item)
		}
	}
	return out
}

func renderPickerChoiceLine(styles tuitheme.Styles, width int, selected bool, text string) string {
	line := truncateText(pickerMarker(selected)+text, width)
	if selected {
		return styles.Render(styles.Picker.Selected, line)
	}
	return styles.Render(styles.Picker.Item, line)
}

func renderPickerTitle(styles tuitheme.Styles, width int, text string) string {
	return styles.Render(styles.Picker.Title, truncateText(text, width))
}

func renderPickerItem(styles tuitheme.Styles, width int, text string) string {
	return styles.Render(styles.Picker.Item, truncateText(text, width))
}

func renderPickerEmpty(styles tuitheme.Styles, width int, text string) string {
	return styles.Render(styles.Picker.Empty, truncateText(text, width))
}

func pickerFilterLabel(query string) string {
	if query == "" {
		return "all"
	}
	return query
}

func (m Model) pickerView(width int) string {
	if m.picker == nil {
		return ""
	}
	return m.picker.view(width, m.styles)
}

func (m Model) sessionPickerView(width int) string {
	if m.sessions == nil {
		return ""
	}
	return m.sessions.view(width, m.styles)
}

func (m Model) planPickerView(width int) string {
	if m.plans == nil {
		return ""
	}
	return m.plans.view(width, m.styles)
}

func (m Model) rewindPickerView(width int) string {
	if m.rewind == nil {
		return ""
	}
	return m.rewind.view(width, m.styles)
}

func (m Model) filePickerView(width int) string {
	if m.files == nil {
		return ""
	}
	return m.files.view(width, m.styles)
}

func (m Model) shellHintView(width int) string {
	value := strings.TrimSpace(m.input.Value())
	if !isShellInput(value) {
		return ""
	}
	command := strings.TrimSpace(strings.TrimPrefix(value, "!"))
	label := "shell mode · enter runs locally"
	if command == "" {
		label = "shell mode · type a command, enter runs locally"
	}
	if cwd := strings.TrimSpace(m.status.cwd); cwd != "" {
		label += " · cwd " + cwd
	}
	return m.styles.Render(m.styles.Picker.Title, truncateText(label, width))
}

func (m Model) modelSuggestions(prefix string, width int) string {
	return valueSuggestionsFrom(filterValueSuggestions(m.models, prefix), width, "model", m.slashIdx, m.styles)
}

func valueSuggestionsFrom(values []string, width int, label string, selected int, styles tuitheme.Styles) string {
	var b strings.Builder
	for i, value := range values {
		if i > 0 {
			b.WriteByte('\n')
		}
		marker := "  "
		if i == selectedIndex(selected, len(values)) {
			marker = "> "
		}
		line := truncateText(marker+value, width)
		if i == selectedIndex(selected, len(values)) {
			b.WriteString(styles.Render(styles.Picker.Selected, line))
			continue
		}
		b.WriteString(styles.Render(styles.Picker.Item, line))
	}
	if len(values) == 0 {
		return styles.Render(styles.Picker.Empty, truncateText("  no matching "+label, width))
	}
	return b.String()
}

func filterValueSuggestions(values []string, prefix string) []string {
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if prefix != "" && !strings.Contains(strings.ToLower(value), prefix) {
			continue
		}
		out = append(out, value)
		if len(out) >= 8 {
			break
		}
	}
	return out
}

func normalizeModels(models []string, current string) []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(model string) {
		model = strings.TrimSpace(model)
		if model == "" {
			return
		}
		if _, ok := seen[model]; ok {
			return
		}
		seen[model] = struct{}{}
		out = append(out, model)
	}
	add(current)
	for _, model := range models {
		add(model)
	}
	return out
}

func normalizeOptions(options []string, current string) []string {
	current = strings.TrimSpace(current)
	seen := map[string]struct{}{}
	var out []string
	for _, option := range options {
		option = strings.TrimSpace(option)
		if option == "" {
			continue
		}
		if _, ok := seen[option]; ok {
			continue
		}
		seen[option] = struct{}{}
		out = append(out, option)
	}
	if current != "" {
		if _, ok := seen[current]; !ok {
			out = append([]string{current}, out...)
		}
	}
	return out
}

func modelAllowed(models []string, model string) bool {
	for _, candidate := range models {
		if candidate == model {
			return true
		}
	}
	return false
}
