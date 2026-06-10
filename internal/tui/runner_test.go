package tui

import (
	"context"
	"strings"
	"testing"
)

type panicPromptRunner struct{}

func (panicPromptRunner) Run(context.Context, string, chan<- Event) error {
	panic("boom")
}

func TestRunPromptRecoversRunnerPanic(t *testing.T) {
	events := make(chan Event, 1)
	cmd := runPrompt(context.Background(), panicPromptRunner{}, "go", events)
	_ = cmd()

	event, ok := <-events
	if !ok {
		t.Fatal("events channel closed before panic event")
	}
	if event.Type != EventError || !strings.Contains(event.Content, "agent run panic: boom") {
		t.Fatalf("panic event = %+v", event)
	}
	if _, ok := <-events; ok {
		t.Fatal("events channel should be closed after panic event")
	}
}
