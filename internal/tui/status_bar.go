package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/feimingxliu/ub/internal/tui/tuitheme"
)

type statusBar struct {
	model             string
	effort            string
	executionMode     string
	cwd               string
	turn              int
	state             string
	width             int
	contextUsedTokens int
	contextMaxTokens  int
	contextRatio      float64
	contextKind       string
}

const (
	statusIdle       = "idle"
	statusThinking   = "thinking"
	statusStreaming  = "streaming"
	statusTool       = "tool"
	statusFinalizing = "finalizing"
)

func (s statusBar) view(width int, styles tuitheme.Styles) string {
	state := defaultString(s.state, statusIdle)
	effort := defaultString(s.effort, "none")
	segments := []statusSegment{
		{label: "model", value: defaultString(s.model, "unknown")},
		{label: "effort", value: effort},
		{label: "mode", value: defaultString(s.executionMode, "work"), semantic: "mode"},
	}
	if segment, ok := s.contextSegment(); ok {
		segments = append(segments, segment)
	}
	segments = append(segments, statusSegment{label: "state", value: state, semantic: "state"})
	if s.turn > 0 {
		segments = append(segments, statusSegment{label: "turn", value: fmt.Sprintf("%d", s.turn)})
	}
	segments = append(segments, statusSegment{label: "cwd", value: defaultString(s.cwd, ".")})
	segments = fitStatusSegments(segments, width)

	rendered := make([]string, len(segments))
	for i, segment := range segments {
		raw := segment.label + ": " + segment.value
		rendered[i] = styles.Render(statusSegmentStyle(segment, styles), raw)
	}
	return styles.Render(styles.Status.Bar, strings.Join(rendered, " "))
}

type statusSegment struct {
	label    string
	value    string
	semantic string
}

func fitStatusSegments(segments []statusSegment, width int) []statusSegment {
	width = contentWidth(width)
	rawWidth := statusSegmentsWidth(segments)
	if rawWidth <= width {
		return segments
	}
	out := append([]statusSegment(nil), segments...)
	for i := range out {
		switch out[i].label {
		case "cwd":
			out[i].value = shrinkStatusValue(out[i].value, max(8, width/4))
		case "model":
			out[i].value = shrinkStatusValue(out[i].value, max(10, width/3))
		case "ctx est", "ctx last":
			out[i].value = shrinkStatusValue(out[i].value, max(8, width/6))
		}
	}
	if statusSegmentsWidth(out) <= width {
		return out
	}
	for i := range out {
		switch out[i].label {
		case "effort", "mode", "state":
			out[i].value = shrinkStatusValue(out[i].value, max(4, width/8))
		}
	}
	return out
}

func (s statusBar) contextSegment() (statusSegment, bool) {
	if s.contextUsedTokens <= 0 {
		return statusSegment{}, false
	}
	value := fmt.Sprintf("%d", s.contextUsedTokens)
	if s.contextMaxTokens > 0 {
		percent := int(s.contextRatio*100 + 0.5)
		if percent == 0 && s.contextUsedTokens > 0 {
			percent = int(float64(s.contextUsedTokens)/float64(s.contextMaxTokens)*100 + 0.5)
		}
		value = fmt.Sprintf("%d/%d %d%%", s.contextUsedTokens, s.contextMaxTokens, percent)
	}
	label := "ctx est"
	if s.contextKind == "last" {
		label = "ctx last"
	}
	return statusSegment{label: label, value: value}, true
}

func statusSegmentsWidth(segments []statusSegment) int {
	var parts []string
	for _, segment := range segments {
		parts = append(parts, segment.label+": "+segment.value)
	}
	return runewidth.StringWidth(strings.Join(parts, " "))
}

func shrinkStatusValue(value string, width int) string {
	if runewidth.StringWidth(value) <= width {
		return value
	}
	return truncateText(value, width)
}

func statusSegmentStyle(segment statusSegment, styles tuitheme.Styles) lipgloss.Style {
	base := styles.Status.Segment
	switch segment.semantic {
	case "mode":
		return base.Copy().Foreground(modeStyle(segment.value, styles).GetForeground())
	case "state":
		return base.Copy().Foreground(stateStyle(segment.value, styles).GetForeground())
	default:
		return base
	}
}

func modeStyle(mode string, styles tuitheme.Styles) lipgloss.Style {
	switch mode {
	case stringExecutionModePlan:
		return styles.Status.ModePlan
	case stringExecutionModeAuto:
		return styles.Status.ModeAuto
	default:
		return styles.Status.ModeWork
	}
}

func stateStyle(state string, styles tuitheme.Styles) lipgloss.Style {
	switch state {
	case statusIdle:
		return styles.Status.StateIdle
	case "failed", "error":
		return styles.Status.StateError
	default:
		return styles.Status.StateBusy
	}
}

const (
	stringExecutionModePlan = "plan"
	stringExecutionModeAuto = "auto"
)
