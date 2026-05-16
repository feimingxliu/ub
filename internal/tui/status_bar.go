package tui

import "fmt"

type statusBar struct {
	model         string
	effort        string
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
	effort := defaultString(s.effort, "none")
	if s.turn <= 0 {
		return truncateText(fmt.Sprintf("model: %s | mode: %s | effort: %s | state: %s | cwd: %s", s.model, s.executionMode, effort, state, s.cwd), width)
	}
	return truncateText(fmt.Sprintf("model: %s | mode: %s | effort: %s | state: %s | turn: %d | cwd: %s", s.model, s.executionMode, effort, state, s.turn, s.cwd), width)
}
