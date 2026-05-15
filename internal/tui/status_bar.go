package tui

import "fmt"

type statusBar struct {
	model         string
	executionMode string
	cwd           string
	width         int
}

func (s statusBar) view() string {
	return fmt.Sprintf("model: %s | mode: %s | cwd: %s", s.model, s.executionMode, s.cwd)
}
