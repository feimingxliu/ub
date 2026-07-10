// Package diffview renders unified diffs for terminal UI surfaces.
package diffview

import (
	"strings"

	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"

	"github.com/feimingxliu/ub/internal/tool"
	"github.com/feimingxliu/ub/internal/tui/theme"
)

// Model renders and navigates a set of file diffs.
type Model struct {
	files    []tool.FileDiff
	selected int
}

// New creates a diffview model.
func New(files []tool.FileDiff) Model {
	copied := append([]tool.FileDiff(nil), files...)
	return Model{files: copied}
}

// Next selects the next file, wrapping at the end.
func (m Model) Next() Model {
	if len(m.files) == 0 {
		return m
	}
	m.selected = (m.selected + 1) % len(m.files)
	return m
}

// Prev selects the previous file, wrapping at the beginning.
func (m Model) Prev() Model {
	if len(m.files) == 0 {
		return m
	}
	m.selected = (m.selected - 1 + len(m.files)) % len(m.files)
	return m
}

// HandleKey applies simple navigation keys. It reports whether the key was handled.
func (m *Model) HandleKey(key string) bool {
	switch key {
	case "right", "down":
		*m = m.Next()
		return true
	case "left", "up":
		*m = m.Prev()
		return true
	default:
		return false
	}
}

// SelectedPath returns the currently selected file path.
func (m Model) SelectedPath() string {
	if len(m.files) == 0 {
		return ""
	}
	return m.files[m.selected].Path
}

// View renders the selected file diff.
func (m Model) View() string {
	theme := tuitheme.Default()
	if len(m.files) == 0 {
		return theme.Render(theme.Diff.Help, "No diff preview")
	}
	file := m.files[m.selected]
	var b strings.Builder
	b.WriteString(theme.Render(theme.Diff.Tabs, m.tabs()))
	b.WriteByte('\n')
	b.WriteString(theme.Render(theme.Diff.Path, file.Path))
	if strings.TrimSpace(file.Kind) != "" {
		b.WriteString(theme.Render(theme.Diff.Kind, " ("+file.Kind+")"))
	}
	b.WriteByte('\n')
	if strings.TrimSpace(file.UnifiedDiff) == "" {
		b.WriteString(theme.Render(theme.Diff.Help, "(empty diff)"))
		return b.String()
	}
	b.WriteString(highlight(file.Path, file.UnifiedDiff))
	return b.String()
}

func (m Model) tabs() string {
	parts := make([]string, len(m.files))
	for i, file := range m.files {
		name := file.Path
		if name == "" {
			name = "(unknown)"
		}
		if i == m.selected {
			name = "[" + name + "]"
		}
		parts[i] = name
	}
	return strings.Join(parts, "  ")
}

func highlight(path, source string) string {
	lexer := lexers.Match(path)
	if lexer == nil {
		lexer = lexers.Fallback
	}
	iterator, err := lexer.Tokenise(nil, source)
	if err != nil {
		return source
	}
	formatter := formatters.Get("terminal256")
	if formatter == nil {
		formatter = formatters.Fallback
	}
	style := styles.Get("github")
	if style == nil {
		style = styles.Fallback
	}
	var b strings.Builder
	if err := formatter.Format(&b, style, iterator); err != nil {
		return source
	}
	return b.String()
}
