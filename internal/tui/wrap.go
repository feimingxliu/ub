package tui

import (
	"strings"

	"github.com/mattn/go-runewidth"
)

const defaultViewWidth = 80

func contentWidth(width int) int {
	if width <= 0 {
		return defaultViewWidth
	}
	return max(20, width)
}

func wrapText(text string, width int) []string {
	width = contentWidth(width)
	if text == "" {
		return []string{""}
	}
	var out []string
	for _, line := range strings.Split(text, "\n") {
		out = append(out, wrapLine(line, width)...)
	}
	return out
}

func wrapLine(line string, width int) []string {
	if runewidth.StringWidth(line) <= width {
		return []string{line}
	}
	words := strings.Fields(line)
	if len(words) == 0 {
		return hardWrap(line, width)
	}
	var out []string
	current := ""
	for _, word := range words {
		if current == "" {
			if runewidth.StringWidth(word) > width {
				wrapped := hardWrap(word, width)
				out = append(out, wrapped[:len(wrapped)-1]...)
				current = wrapped[len(wrapped)-1]
				continue
			}
			current = word
			continue
		}
		next := current + " " + word
		if runewidth.StringWidth(next) <= width {
			current = next
			continue
		}
		out = append(out, current)
		if runewidth.StringWidth(word) > width {
			wrapped := hardWrap(word, width)
			out = append(out, wrapped[:len(wrapped)-1]...)
			current = wrapped[len(wrapped)-1]
		} else {
			current = word
		}
	}
	if current != "" {
		out = append(out, current)
	}
	return out
}

func hardWrap(text string, width int) []string {
	if text == "" {
		return []string{""}
	}
	var out []string
	var b strings.Builder
	lineWidth := 0
	for _, r := range text {
		rw := runewidth.RuneWidth(r)
		if lineWidth > 0 && lineWidth+rw > width {
			out = append(out, b.String())
			b.Reset()
			lineWidth = 0
		}
		b.WriteRune(r)
		lineWidth += rw
	}
	if b.Len() > 0 {
		out = append(out, b.String())
	}
	return out
}

func truncateText(text string, width int) string {
	width = contentWidth(width)
	if runewidth.StringWidth(text) <= width {
		return text
	}
	if width <= 1 {
		return "…"
	}
	target := width - 1
	var b strings.Builder
	lineWidth := 0
	for _, r := range text {
		rw := runewidth.RuneWidth(r)
		if lineWidth+rw > target {
			break
		}
		b.WriteRune(r)
		lineWidth += rw
	}
	b.WriteRune('…')
	return b.String()
}
