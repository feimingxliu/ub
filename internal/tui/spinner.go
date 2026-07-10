package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/mattn/go-runewidth"

	"github.com/feimingxliu/ub/internal/tui/theme"
)

const spinnerTickInterval = 80 * time.Millisecond

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type spinnerTickMsg struct{}

func spinnerTickCmd() tea.Cmd {
	return tea.Tick(spinnerTickInterval, func(time.Time) tea.Msg {
		return spinnerTickMsg{}
	})
}

func (m *Model) beginRunIndicator() {
	m.runStartedAt = time.Now()
	m.spinnerFrame = 0
	m.activitySummary = ""
}

func formatElapsed(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	seconds := int(d.Seconds())
	switch {
	case seconds < 60:
		return fmt.Sprintf("%ds", seconds)
	case seconds < 3600:
		return fmt.Sprintf("%dm%02ds", seconds/60, seconds%60)
	default:
		return fmt.Sprintf("%dh%02dm", seconds/3600, (seconds%3600)/60)
	}
}

func stateLabel(state string) string {
	switch state {
	case statusThinking:
		return "Thinking"
	case statusStreaming:
		return "Streaming"
	case statusTool:
		return "Tool"
	case statusShell:
		return "Shell"
	case statusFinalizing:
		return "Finalizing"
	default:
		if state == "" {
			return ""
		}
		return strings.ToUpper(state[:1]) + state[1:]
	}
}

func (m Model) runIndicatorView(width int) string {
	if !m.running {
		return ""
	}
	switch m.status.state {
	case "", statusIdle, "failed", "error":
		return ""
	}
	frame := spinnerFrames[m.spinnerFrame%len(spinnerFrames)]
	label := stateLabel(m.status.state)
	elapsed := formatElapsed(time.Since(m.runStartedAt))

	var b strings.Builder
	b.WriteString(frame)
	b.WriteByte(' ')
	b.WriteString(label)
	b.WriteString(" · ")
	b.WriteString(elapsed)
	if summary := strings.TrimSpace(m.activitySummary); summary != "" {
		b.WriteString(" · ")
		b.WriteString(summary)
	}

	line := b.String()
	if w := contentWidth(width); w > 0 && runewidth.StringWidth(line) > w {
		line = shrinkStatusValue(line, w)
	}
	return m.styles.Render(runIndicatorStyle(m.status.state, m.styles), line)
}

func runIndicatorStyle(state string, styles tuitheme.Styles) lipgloss.Style {
	return styles.Status.StateBusy.Foreground(stateStyle(state, styles).GetForeground())
}
