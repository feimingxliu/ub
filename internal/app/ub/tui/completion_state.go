package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/feimingxliu/ub/internal/app/ub/tui/slash"
)

func (m *Model) completeSlash() bool {
	matches := m.slashCommandMatches()
	if len(matches) == 0 {
		return false
	}
	completion := "/" + matches[m.selectedSlashIndex(matches)].Name + " "
	m.input.SetValue(completion)
	m.input.CursorEnd()
	m.slashIdx = 0
	m.resetPromptHistoryNavigation()
	return true
}

func (m *Model) completeFileMention() bool {
	if m.files == nil {
		return false
	}
	selected := m.files.selected()
	if strings.TrimSpace(selected) == "" {
		return false
	}
	line, lineIdx, col := m.currentInputLine()
	token, ok := activeFileMention(line, col)
	if !ok {
		m.files = nil
		return false
	}
	newLine, newCol := insertFileMention(line, token, selected)
	next := replaceInputLine(m.input.Value(), lineIdx, newLine)
	m.input.SetValue(next)
	m.setInputCursor(lineIdx, newCol)
	m.files = nil
	m.resetPromptHistoryNavigation()
	return true
}

func (m *Model) completeSlashValue() bool {
	values, command, _, ok := m.slashValueMatches()
	if !ok || len(values) == 0 {
		return false
	}
	selected := values[selectedIndex(m.slashIdx, len(values))]
	m.input.SetValue("/" + command + " " + selected)
	m.input.CursorEnd()
	m.slashIdx = 0
	m.resetPromptHistoryNavigation()
	return true
}

func (m *Model) moveFileSelection(delta int) bool {
	if m.files == nil {
		return false
	}
	if delta < 0 {
		m.files.previous()
	} else {
		m.files.next()
	}
	return true
}

func (m *Model) refreshFilePicker() {
	value := m.input.Value()
	if strings.HasPrefix(strings.TrimSpace(value), "/") {
		m.files = nil
		return
	}
	line, _, col := m.currentInputLine()
	token, ok := activeFileMention(line, col)
	if !ok {
		m.files = nil
		return
	}
	runner, ok := m.runner.(WorkspaceFileRunner)
	if !ok {
		m.files = newFilePicker(nil, token.prefix, fmt.Errorf("file selection is unavailable in this runner"))
		return
	}
	files, err := runner.ListWorkspaceFiles(m.ctx, token.prefix, maxFileMentionCandidates)
	m.files = newFilePicker(files, token.prefix, err)
}

func (m Model) acceptSlashValueSuggestion() (tea.Model, tea.Cmd, bool) {
	values, command, _, ok := m.slashValueMatches()
	if !ok || len(values) == 0 {
		return m, nil, false
	}
	selected := values[selectedIndex(m.slashIdx, len(values))]
	next := "/" + command + " " + selected
	m.input.SetValue("")
	m.slashIdx = 0
	m.resetPromptHistoryNavigation()
	updated, cmd := m.executeSlash(next)
	return updated, cmd, true
}

func (m *Model) completeSlashOnEnter() bool {
	raw := m.input.Value()
	value := strings.TrimSpace(raw)
	if !strings.HasPrefix(value, "/") || slashInputHasArgs(raw) {
		return false
	}
	if _, err := slash.Parse(value); err == nil {
		return false
	}
	return m.completeSlash()
}

func (m *Model) moveSlashSelection(delta int) bool {
	matches := m.slashCommandMatches()
	if len(matches) == 0 {
		return false
	}
	m.slashIdx = (m.selectedSlashIndex(matches) + delta + len(matches)) % len(matches)
	return true
}

func (m *Model) moveSlashValueSelection(delta int) bool {
	values, _, _, ok := m.slashValueMatches()
	if !ok {
		return false
	}
	if len(values) == 0 {
		return true
	}
	m.slashIdx = (selectedIndex(m.slashIdx, len(values)) + delta + len(values)) % len(values)
	return true
}

func (m Model) slashCommandMatches() []slash.Spec {
	raw := m.input.Value()
	value := strings.TrimSpace(raw)
	if !strings.HasPrefix(value, "/") || slashInputHasArgs(raw) {
		return nil
	}
	return slash.Match(value)
}

func (m Model) slashValueSuggestions(width int) string {
	values, _, label, ok := m.slashValueMatches()
	if !ok {
		return ""
	}
	return valueSuggestionsFrom(values, width, label, m.slashIdx, m.styles)
}

func (m Model) slashValueMatches() ([]string, string, string, bool) {
	source, ok := m.slashValueSource()
	if !ok {
		return nil, "", "", false
	}
	return filterValueSuggestions(source.values, source.prefix), source.command, source.label, true
}

type slashValueSource struct {
	command string
	label   string
	prefix  string
	values  []string
}

func (m Model) slashValueSource() (slashValueSource, bool) {
	if prefix, ok := slashCommandArgPrefix(m.input.Value(), "provider"); ok {
		return slashValueSource{command: "provider", label: "provider", prefix: prefix, values: m.providers}, true
	}
	if prefix, ok := slashCommandArgPrefix(m.input.Value(), "model"); ok {
		return slashValueSource{command: "model", label: "model", prefix: prefix, values: m.models}, true
	}
	if prefix, ok := slashCommandArgPrefix(m.input.Value(), "effort"); ok {
		return slashValueSource{command: "effort", label: "effort", prefix: prefix, values: m.efforts}, true
	}
	if prefix, ok := slashCommandArgPrefix(m.input.Value(), "approval-model"); ok {
		return slashValueSource{command: "approval-model", label: "approval model", prefix: prefix, values: m.approvalModels}, true
	}
	if prefix, ok := slashCommandArgPrefix(m.input.Value(), "small-model"); ok {
		return slashValueSource{command: "small-model", label: "small model", prefix: prefix, values: m.smallModels}, true
	}
	return slashValueSource{}, false
}

func slashCommandArgPrefix(raw, command string) (string, bool) {
	value := strings.TrimLeft(raw, " \t\r\n")
	head := "/" + command
	if !strings.HasPrefix(strings.ToLower(value), head) {
		return "", false
	}
	rest := value[len(head):]
	if rest == "" || !strings.ContainsAny(rest[:1], " \t\r\n") {
		return "", false
	}
	return strings.TrimSpace(rest), true
}

func (m Model) selectedSlashIndex(matches []slash.Spec) int {
	return selectedIndex(m.slashIdx, len(matches))
}

// currentInputLine returns the logical line the cursor is on, its 0-indexed
// line number, and the cursor's column (rune offset) within that line. The
// textarea tracks cursor position as (Line, Column) over logical lines, so we
// split the value on newlines to fetch the corresponding text.
func (m *Model) currentInputLine() (line string, lineIdx, col int) {
	lineIdx = m.input.Line()
	col = m.input.Column()
	lines := strings.Split(m.input.Value(), "\n")
	if lineIdx < 0 || lineIdx >= len(lines) {
		return "", lineIdx, col
	}
	return lines[lineIdx], lineIdx, col
}

// replaceInputLine swaps the logical line at lineIdx with newLine, leaving all
// other lines intact.
func replaceInputLine(value string, lineIdx int, newLine string) string {
	lines := strings.Split(value, "\n")
	if lineIdx < 0 || lineIdx >= len(lines) {
		return value
	}
	lines[lineIdx] = newLine
	return strings.Join(lines, "\n")
}

// setInputCursor moves the textarea cursor to (line, col) over logical lines.
// The textarea exposes no direct row setter, so we rely on MoveToBegin /
// MoveToEnd (which set the row directly) for the first and last lines, and
// CursorDown for intermediate lines. CursorDown crosses soft-wrapped visual
// rows and, due to a bubbles soft-wrap edge case, can stall without advancing
// the logical line, so the loop is guarded by a stale-progress check that
// breaks before an infinite loop. In that rare stall the cursor lands on the
// nearest reachable line; the inserted text itself is already correct, only
// the cursor column may be off. SetCursorColumn then pins the column.
func (m *Model) setInputCursor(line, col int) {
	last := m.input.LineCount() - 1
	switch {
	case line <= 0:
		m.input.MoveToBegin()
	case line >= last:
		m.input.MoveToEnd()
	default:
		m.input.MoveToBegin()
		prev := m.input.Line()
		for m.input.Line() < line {
			m.input.CursorDown()
			if m.input.Line() == prev {
				// CursorDown stalled on a soft-wrapped line; stop to avoid
				// spinning forever.
				break
			}
			prev = m.input.Line()
		}
	}
	m.input.SetCursorColumn(col)
}

func selectedIndex(index, length int) int {
	if length == 0 || index < 0 {
		return 0
	}
	if index >= length {
		return length - 1
	}
	return index
}

func slashInputHasArgs(raw string) bool {
	trimmedLeft := strings.TrimLeft(raw, " \t\r\n")
	if !strings.HasPrefix(trimmedLeft, "/") {
		return false
	}
	withoutSlash := strings.TrimPrefix(trimmedLeft, "/")
	return strings.ContainsAny(withoutSlash, " \t\r\n")
}
