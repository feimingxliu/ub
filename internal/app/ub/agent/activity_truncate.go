package agent

import (
	"fmt"
	"strings"
)

func truncateActivitySummary(text string) string {
	// Activity summaries are rendered as a single-line label (chip or status
	// row). Collapse all interior whitespace so reasoning summaries — which the
	// model often produces with embedded "\n\n" paragraph breaks — don't end up
	// pushing the TUI footer off-screen when the chip is rendered.
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) <= maxActivitySummaryRunes {
		return text
	}
	return string(runes[:maxActivitySummaryRunes-3]) + "..."
}

func truncateActivityDetail(text string) string {
	return truncateActivityDetailToRunes(text, maxActivityDetailRunes)
}

func truncateToolActivityDetail(text string) string {
	return truncateActivityDetailToRunes(text, maxToolActivityDetailRunes)
}

func truncateActivityDetailToRunes(text string, maxRunes int) string {
	text = strings.TrimRight(text, " \t\r\n")
	if strings.TrimSpace(text) == "" {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	notice := fmt.Sprintf("[activity detail truncated: showing preview of %d runes; original %d runes]", maxRunes, len(runes))
	footer := ""
	if foundFooter, ok := toolResultTruncationFooter(text); ok {
		footer = strings.TrimRight(foundFooter, " \t\r\n")
		notice = "[activity detail truncated: showing preview; tool result footer preserved]"
	}
	suffix := ""
	if footer != "" {
		suffix = "\n" + footer
	}
	prefix := notice + "\n"
	budget := maxRunes - len([]rune(prefix)) - len([]rune(suffix))
	if budget < 0 {
		budget = 0
	}
	preview := strings.TrimRight(string(runes[:budget]), " \t\r\n")
	if preview == "" {
		return notice + suffix
	}
	return prefix + preview + suffix
}

func toolResultTruncationFooter(text string) (string, bool) {
	index := strings.LastIndex(text, toolResultTruncatedMarker)
	if index < 0 {
		return "", false
	}
	footer := strings.TrimSpace(text[index:])
	return footer, footer != ""
}
