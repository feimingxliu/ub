package fake

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"

	"github.com/feimingxliu/ub/internal/config"
	"github.com/feimingxliu/ub/internal/message"
	"github.com/feimingxliu/ub/internal/provider"
)

func TestScriptEmitsEventsInOrder(t *testing.T) {
	p := New(Script{
		TextDelta("hi"),
		ReasoningDelta("thinking"),
		ToolCall("fs.read", map[string]any{"path": "main.go"}),
		Usage(3, 5),
		Done(),
	})
	stream, err := p.Chat(context.Background(), provider.Request{
		Model:    "fake/model",
		Messages: []message.Message{message.Text(message.RoleUser, "hello")},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	defer stream.Close()

	ev, err := stream.Next(context.Background())
	if err != nil || ev.Type != provider.EventTextDelta || ev.Text != "hi" {
		t.Fatalf("text event = %#v, err=%v", ev, err)
	}
	ev, err = stream.Next(context.Background())
	if err != nil || ev.Type != provider.EventReasoningDelta || ev.Reasoning != "thinking" {
		t.Fatalf("reasoning event = %#v, err=%v", ev, err)
	}
	ev, err = stream.Next(context.Background())
	if err != nil || ev.Type != provider.EventToolCall || ev.ToolName != "fs.read" {
		t.Fatalf("tool event = %#v, err=%v", ev, err)
	}
	var input map[string]string
	if err := json.Unmarshal(ev.Input, &input); err != nil || input["path"] != "main.go" {
		t.Fatalf("tool input = %s, err=%v", ev.Input, err)
	}
	ev, err = stream.Next(context.Background())
	if err != nil || ev.Type != provider.EventUsage || ev.Usage.InputTokens != 3 || ev.Usage.OutputTokens != 5 {
		t.Fatalf("usage event = %#v, err=%v", ev, err)
	}
	ev, err = stream.Next(context.Background())
	if err != nil || ev.Type != provider.EventDone {
		t.Fatalf("done event = %#v, err=%v", ev, err)
	}
	_, err = stream.Next(context.Background())
	if !errors.Is(err, io.EOF) {
		t.Fatalf("after done err = %v, want EOF", err)
	}
}

func TestFromConfigScript(t *testing.T) {
	p, err := NewFromConfig("test", config.ProviderConfig{
		Type: "fake",
		Script: []config.ProviderScriptEvent{
			{Type: "text_delta", Text: "configured"},
			{Type: "reasoning_delta", Reasoning: "configured thinking"},
			{Type: "done"},
		},
	})
	if err != nil {
		t.Fatalf("NewFromConfig: %v", err)
	}
	stream, err := p.Chat(context.Background(), provider.Request{})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	ev, err := stream.Next(context.Background())
	if err != nil || ev.Text != "configured" {
		t.Fatalf("configured event = %#v, err=%v", ev, err)
	}
	ev, err = stream.Next(context.Background())
	if err != nil || ev.Type != provider.EventReasoningDelta || ev.Reasoning != "configured thinking" {
		t.Fatalf("configured reasoning event = %#v, err=%v", ev, err)
	}
}

func TestMultiRoundScripts(t *testing.T) {
	p := NewRounds(
		Script{ToolCall("read", map[string]any{"path": "main.go"}), Done()},
		Script{TextDelta("done"), Done()},
	)
	first, err := p.Chat(context.Background(), provider.Request{})
	if err != nil {
		t.Fatalf("first Chat: %v", err)
	}
	ev, err := first.Next(context.Background())
	if err != nil || ev.Type != provider.EventToolCall {
		t.Fatalf("first event = %#v, err=%v", ev, err)
	}
	second, err := p.Chat(context.Background(), provider.Request{
		Messages: []message.Message{
			message.New(message.RoleUser, message.ToolResultBlock(ev.ToolUseID, "file content", false)),
		},
	})
	if err != nil {
		t.Fatalf("second Chat: %v", err)
	}
	ev, err = second.Next(context.Background())
	if err != nil || ev.Type != provider.EventTextDelta || ev.Text != "done" {
		t.Fatalf("second event = %#v, err=%v", ev, err)
	}
}

func TestErrorEvent(t *testing.T) {
	p := New(Script{Error("boom")})
	stream, err := p.Chat(context.Background(), provider.Request{})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	ev, err := stream.Next(context.Background())
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if ev.Type != provider.EventError || ev.Err == nil || ev.Err.Error() != "boom" {
		t.Fatalf("error event = %#v", ev)
	}
}

func TestNextHonorsContextCancelAndClose(t *testing.T) {
	p := New(Script{TextDelta("hi")})
	stream, err := p.Chat(context.Background(), provider.Request{})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := stream.Next(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Next canceled err = %v", err)
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if _, err := stream.Next(context.Background()); !errors.Is(err, io.EOF) {
		t.Fatalf("Next after close err = %v", err)
	}
}

func TestConfigUnknownEventType(t *testing.T) {
	_, err := NewFromConfig("test", config.ProviderConfig{
		Type:   "fake",
		Script: []config.ProviderScriptEvent{{Type: "mystery"}},
	})
	if err == nil {
		t.Fatal("expected unknown event type error")
	}
}
