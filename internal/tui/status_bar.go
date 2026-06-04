package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/mattn/go-runewidth"

	"github.com/feimingxliu/ub/internal/tui/tuitheme"
)

type statusBar struct {
	provider          string
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
	statusShell      = "shell"
	statusFinalizing = "finalizing"
	statusSeparator  = " │ "
	statusCompactSep = "│"
)

func (s statusBar) view(width int, styles tuitheme.Styles) string {
	rendered, _ := s.render(width, styles)
	return rendered
}

// helpHit reports whether display column x falls on the help "?" segment
// in the rendered status bar. Returns false when the segment is dropped
// at the given width. Hit-testing uses the same render pass the status
// bar uses on screen, so it stays in sync with how the segment is laid
// out (including per-segment padding).
func (s statusBar) helpHit(width int, styles tuitheme.Styles, x int) bool {
	_, span := s.render(width, styles)
	if span.width <= 0 {
		return false
	}
	return x >= span.start && x < span.start+span.width
}

type helpSpan struct {
	start int
	width int
}

func (s statusBar) render(width int, styles tuitheme.Styles) (string, helpSpan) {
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
	segments = append(segments, statusSegment{label: "?", semantic: "help"})
	segments = fitStatusSegments(segments, width, styles)

	rendered := make([]string, len(segments))
	help := helpSpan{}
	separator := styles.Render(styles.SubtleLine, statusSeparatorText(styles))
	col := 0
	for i, segment := range segments {
		if i > 0 {
			col += lipgloss.Width(separator)
		}
		piece := styles.Render(statusSegmentStyle(segment, styles), statusSegmentText(segment))
		w := lipgloss.Width(piece)
		if segment.semantic == "help" {
			help = helpSpan{start: col, width: w}
		}
		rendered[i] = piece
		col += w
	}
	return styles.Render(styles.Status.Bar, strings.Join(rendered, separator)), help
}

type statusSegment struct {
	label    string
	value    string
	semantic string
}

func fitStatusSegments(segments []statusSegment, width int, styles tuitheme.Styles) []statusSegment {
	width = contentWidth(width)
	out := append([]statusSegment(nil), segments...)
	if statusSegmentsWidth(out, styles) <= width {
		return out
	}

	out = removeStatusSegment(out, "effort")
	if statusSegmentsWidth(out, styles) <= width {
		return out
	}

	out = removeStatusSegment(out, "cwd")
	if statusSegmentsWidth(out, styles) <= width {
		return out
	}

	out = shrinkStatusSegments(out, width, styles)
	if statusSegmentsWidth(out, styles) <= width {
		return out
	}

	for _, label := range []string{"cwd", "turn", "ctx est", "ctx last", "effort", "model", "mode"} {
		out = removeStatusSegment(out, label)
		if statusSegmentsWidth(out, styles) <= width {
			return out
		}
	}
	return out
}

func shrinkStatusSegments(segments []statusSegment, width int, styles tuitheme.Styles) []statusSegment {
	out := append([]statusSegment(nil), segments...)
	rules := []statusShrinkRule{
		{label: "cwd", minWidth: 4},
		{label: "model", minWidth: 8},
		{label: "ctx est", minWidth: 5},
		{label: "ctx last", minWidth: 5},
	}
	for {
		beforeWidth := statusSegmentsWidth(out, styles)
		if beforeWidth <= width {
			return out
		}
		changed := false
		for _, rule := range rules {
			idx := statusSegmentIndex(out, rule.label)
			if idx < 0 {
				continue
			}
			currentWidth := runewidth.StringWidth(out[idx].value)
			if currentWidth <= rule.minWidth {
				continue
			}
			currentTotal := statusSegmentsWidth(out, styles)
			if currentTotal <= width {
				return out
			}
			overflow := currentTotal - width
			reduceBy := min(overflow, currentWidth-rule.minWidth)
			nextValue := shrinkStatusValue(out[idx].value, currentWidth-reduceBy)
			if nextValue == out[idx].value {
				continue
			}
			out[idx].value = nextValue
			changed = true
			if statusSegmentsWidth(out, styles) <= width {
				return out
			}
		}
		if !changed || statusSegmentsWidth(out, styles) >= beforeWidth {
			return out
		}
	}
}

type statusShrinkRule struct {
	label    string
	minWidth int
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

func statusSegmentsWidth(segments []statusSegment, styles tuitheme.Styles) int {
	separator := styles.Render(styles.SubtleLine, statusSeparatorText(styles))
	total := 0
	for i, segment := range segments {
		if i > 0 {
			total += lipgloss.Width(separator)
		}
		total += lipgloss.Width(styles.Render(statusSegmentStyle(segment, styles), statusSegmentText(segment)))
	}
	return total
}

func statusSegmentText(segment statusSegment) string {
	if strings.TrimSpace(segment.value) == "" {
		return segment.label
	}
	return segment.label + ": " + segment.value
}

func statusSeparatorText(styles tuitheme.Styles) string {
	if styles.Plain {
		return statusSeparator
	}
	return statusCompactSep
}

func statusSegmentIndex(segments []statusSegment, label string) int {
	for i, segment := range segments {
		if segment.label == label {
			return i
		}
	}
	return -1
}

func removeStatusSegment(segments []statusSegment, label string) []statusSegment {
	idx := statusSegmentIndex(segments, label)
	if idx < 0 {
		return segments
	}
	out := append([]statusSegment(nil), segments[:idx]...)
	return append(out, segments[idx+1:]...)
}

func shrinkStatusValue(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if runewidth.StringWidth(value) <= width {
		return value
	}
	if width == 1 {
		return "…"
	}
	target := width - 1
	var b strings.Builder
	lineWidth := 0
	for _, r := range value {
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

func statusSegmentStyle(segment statusSegment, styles tuitheme.Styles) lipgloss.Style {
	base := styles.Status.Segment
	switch segment.semantic {
	case "mode":
		return base.Copy().Foreground(modeStyle(segment.value, styles).GetForeground())
	case "state":
		return base.Copy().Foreground(stateStyle(segment.value, styles).GetForeground())
	case "help":
		return base.Copy().Foreground(styles.Status.Value.GetForeground()).Bold(true)
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
	case stringExecutionModeFullAccess:
		return styles.Status.ModeFull
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
	stringExecutionModePlan       = "plan"
	stringExecutionModeAuto       = "auto"
	stringExecutionModeFullAccess = "full-access"
)
