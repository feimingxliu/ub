package tui

import "fmt"

type statusBar struct {
	model         string
	executionMode string
	cwd           string
	turn          int
	running       bool
	width         int
}

func (s statusBar) view(width int) string {
	state := "idle"
	if s.running {
		state = "running"
	}
	return truncateText(fmt.Sprintf("model: %s | mode: %s | turn: %d | state: %s | cwd: %s", s.model, s.executionMode, s.turn, state, s.cwd), width)
}
