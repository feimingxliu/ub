package tui

import "fmt"

type statusBar struct {
	model         string
	executionMode string
	cwd           string
	turn          int
	state         string
	width         int
}

const (
	statusIdle       = "idle"
	statusThinking   = "thinking"
	statusStreaming  = "streaming"
	statusTool       = "tool"
	statusFinalizing = "finalizing"
)

func (s statusBar) view(width int) string {
	state := defaultString(s.state, statusIdle)
	return truncateText(fmt.Sprintf("model: %s | mode: %s | turn: %d | state: %s | cwd: %s", s.model, s.executionMode, s.turn, state, s.cwd), width)
}
