package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/mattn/go-runewidth"

	"github.com/feimingxliu/ub/internal/tui/theme"
)

func appendToolDetailLines(out []string, toolName, detail, prefix string, width int, styles tuitheme.Styles) []string {
	if strings.TrimSpace(detail) == "" {
		return out
	}
	if toolDetailUsesTodoStyle(toolName) {
		return appendTodoDetailLines(out, detail, prefix, width, styles)
	}
	textWidth := max(10, contentWidth(width)-runewidth.StringWidth(prefix))
	if !toolDetailUsesDiffStyle(toolName) {
		style := styles.Tool.Detail
		for _, line := range strings.Split(detail, "\n") {
			for _, wrapped := range wrapLine(line, textWidth) {
				out = append(out, styles.Render(style, prefix+wrapped))
			}
		}
		return out
	}
	for _, line := range strings.Split(detail, "\n") {
		displayLine := formatToolDetailLine(line)
		style := toolDetailLineStyle(line, styles)
		for _, wrapped := range wrapLine(displayLine, textWidth) {
			out = append(out, styles.Render(style, prefix+wrapped))
		}
	}
	return out
}

func toolDetailUsesTodoStyle(toolName string) bool {
	switch strings.TrimSpace(toolName) {
	case "todo_write", "todo_update":
		return true
	default:
		return false
	}
}

func appendTodoDetailLines(out []string, detail, prefix string, width int, styles tuitheme.Styles) []string {
	textWidth := max(10, contentWidth(width)-runewidth.StringWidth(prefix))
	items := parseTodoDetailItems(detail)
	if len(items) > 0 {
		for _, item := range items {
			style := styles.Tool.Detail
			if item.status == "failed" {
				style = styles.Tool.Failed
			}
			for _, wrapped := range wrapLine(item.text, textWidth) {
				out = append(out, styles.Render(style, prefix+wrapped))
			}
		}
		return out
	}
	style := styles.Tool.Detail
	for _, line := range strings.Split(detail, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "session_id=") || strings.HasPrefix(trimmed, "todo_count=") || trimmed == "## Todo" {
			continue
		}
		for _, wrapped := range wrapLine(trimmed, textWidth) {
			out = append(out, styles.Render(style, prefix+wrapped))
		}
	}
	return out
}

type todoDetailItem struct {
	text   string
	status string
}

func parseTodoDetailItems(detail string) []todoDetailItem {
	var items []todoDetailItem
	for _, line := range strings.Split(detail, "\n") {
		text, status, ok := parseTodoDetailLine(line)
		if !ok {
			continue
		}
		items = append(items, todoDetailItem{text: text, status: status})
	}
	return items
}

func parseTodoDetailLine(line string) (string, string, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "- [") || len(line) < len("- [ ] ") {
		return "", "", false
	}
	closeIdx := strings.Index(line, "]")
	if closeIdx < 0 || closeIdx <= 3 {
		return "", "", false
	}
	mark := line[3:closeIdx]
	rest := strings.TrimSpace(line[closeIdx+1:])
	if dot := strings.Index(rest, ". "); dot > 0 && allDigits(rest[:dot]) {
		rest = strings.TrimSpace(rest[dot+2:])
	}
	label := "[ ]"
	status := "pending"
	switch mark {
	case ">":
		label = "[>]"
		status = "in_progress"
	case "x", "X":
		label = "[x]"
		status = "completed"
	case "~":
		label = "[~]"
		status = "skipped"
	case "!":
		label = "[!]"
		status = "failed"
	}
	if rest == "" {
		return "", "", false
	}
	return label + " " + rest, status, true
}

func allDigits(text string) bool {
	if text == "" {
		return false
	}
	for _, r := range text {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func toolDetailUsesDiffStyle(toolName string) bool {
	switch strings.TrimSpace(toolName) {
	case "write", "edit", "multiedit":
		return true
	default:
		return false
	}
}

func toolDetailLineStyle(line string, styles tuitheme.Styles) lipgloss.Style {
	switch toolDetailLineKind(line) {
	case toolDetailSummaryLine:
		return styles.Diff.Path
	case toolDetailHeaderLine:
		return styles.Diff.Header
	case toolDetailAddedLine:
		return styles.Diff.Added
	case toolDetailRemovedLine:
		return styles.Diff.Removed
	case toolDetailBlankLine:
		return styles.Tool.Detail
	default:
		return styles.Diff.Context
	}
}

type toolDetailLineKindValue int

const (
	toolDetailContextLine toolDetailLineKindValue = iota
	toolDetailSummaryLine
	toolDetailHeaderLine
	toolDetailAddedLine
	toolDetailRemovedLine
	toolDetailBlankLine
)

func toolDetailLineKind(line string) toolDetailLineKindValue {
	trimmed := strings.TrimSpace(line)
	switch {
	case isFileChangeSummaryLine(trimmed):
		return toolDetailSummaryLine
	case strings.HasPrefix(trimmed, "@@"), strings.HasPrefix(trimmed, "+++"), strings.HasPrefix(trimmed, "---"):
		return toolDetailHeaderLine
	case strings.HasPrefix(trimmed, "+"):
		return toolDetailAddedLine
	case strings.HasPrefix(trimmed, "-"):
		return toolDetailRemovedLine
	case strings.TrimSpace(trimmed) == "":
		return toolDetailBlankLine
	default:
		return toolDetailContextLine
	}
}

func isFileChangeSummaryLine(line string) bool {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return false
	}
	switch fields[0] {
	case "create", "modify", "delete", "changed":
		return true
	default:
		return false
	}
}

func formatToolDetailLine(line string) string {
	if summary, ok := humanFileChangeSummary(line); ok {
		return summary
	}
	return line
}

func humanFileChangeSummary(line string) (string, bool) {
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) < 2 {
		return "", false
	}
	path := strings.Join(fields[1:], " ")
	switch fields[0] {
	case "create":
		return "created file: " + path, true
	case "modify":
		return "modified file: " + path, true
	case "delete":
		return "deleted file: " + path, true
	case "changed":
		return "changed file: " + path, true
	default:
		return "", false
	}
}
