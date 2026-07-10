package tui

import (
	"strings"

	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"

	"github.com/feimingxliu/ub/internal/tui/theme"
)

func renderWrappedPrefixed(text, prefix, indent string, width int, styles tuitheme.Styles, prefixStyle, textStyle lipgloss.Style) []string {
	textWidth := max(10, contentWidth(width)-runewidth.StringWidth(prefix))
	lines := wrapText(text, textWidth)
	out := make([]string, 0, len(lines))
	out = append(out, renderMaybe(styles, prefixStyle, prefix)+renderMaybe(styles, textStyle, lines[0]))
	for _, line := range lines[1:] {
		out = append(out, indent+renderMaybe(styles, textStyle, line))
	}
	return out
}

func renderMarkdownPrefixed(text, prefix, indent string, width int, styles tuitheme.Styles, prefixStyle lipgloss.Style) []string {
	textWidth := max(10, contentWidth(width)-runewidth.StringWidth(prefix))
	lines := markdownLines(text, textWidth, styles)
	if len(lines) == 0 {
		lines = []string{""}
	}
	out := make([]string, 0, len(lines))
	out = append(out, renderMaybe(styles, prefixStyle, prefix)+lines[0])
	for _, line := range lines[1:] {
		out = append(out, indent+line)
	}
	return out
}

func renderMaybe(styles tuitheme.Styles, style lipgloss.Style, value string) string {
	if styles.Plain {
		return value
	}
	return style.Render(value)
}

func markdownLines(text string, width int, styles tuitheme.Styles) []string {
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return []string{""}
	}
	if styles.Plain && width >= 40 && !looksLikeMarkdown(text) {
		return wrapText(text, width)
	}
	styleName := styles.Markdown.StyleName
	if styleName == "" {
		styleName = "dark"
	}
	renderer, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle(styleName),
		glamour.WithWordWrap(max(10, width)),
	)
	if err != nil {
		return wrapText(text, width)
	}
	rendered, err := renderer.Render(text)
	if err != nil {
		return wrapText(text, width)
	}
	rendered = xansi.Wrap(rendered, max(10, width), " ")
	return trimBlankLines(strings.Split(strings.TrimRight(rendered, "\n"), "\n"))
}

func looksLikeMarkdown(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ">") || strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "---") {
			return true
		}
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") || strings.HasPrefix(trimmed, "+ ") {
			return true
		}
		if strings.Contains(trimmed, "**") || strings.Contains(trimmed, "__") || strings.Contains(trimmed, "`") || strings.Contains(trimmed, "](") {
			return true
		}
	}
	return false
}

func trimBlankLines(lines []string) []string {
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " \t")
	}
	start := 0
	for start < len(lines) && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	end := len(lines)
	for end > start && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	if start >= end {
		return []string{""}
	}
	lines = lines[start:end]
	if hasSharedLeftMargin(lines, "  ") {
		for i := range lines {
			if strings.TrimSpace(lines[i]) == "" {
				continue
			}
			lines[i] = strings.TrimPrefix(lines[i], "  ")
		}
	}
	return lines
}

func hasSharedLeftMargin(lines []string, margin string) bool {
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !strings.HasPrefix(line, margin) {
			return false
		}
	}
	return len(lines) > 0
}
