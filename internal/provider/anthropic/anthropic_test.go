package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/feimingxliu/ub/internal/config"
	"github.com/feimingxliu/ub/internal/message"
	"github.com/feimingxliu/ub/internal/provider"
)

func TestNewFromConfigRequiresAPIKey(t *testing.T) {
	_, err := NewFromConfig("anthropic", config.ProviderConfig{Type: "anthropic"})
	if err == nil {
		t.Fatal("expected missing api key error")
	}
	if !strings.Contains(err.Error(), "api_key") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFactoryCreatesAnthropicProvider(t *testing.T) {
	p, err := provider.New("anthropic", config.ProviderConfig{
		Type:   "anthropic",
		APIKey: "sk-test",
	})
	if err != nil {
		t.Fatalf("provider.New: %v", err)
	}
	if p.Name() != "anthropic" {
		t.Fatalf("Name() = %q", p.Name())
	}
	if p.Caps().SupportsStreaming {
		t.Fatalf("I-08 provider should be non-streaming")
	}
}

func TestChatSendsRequestAndReturnsEvents(t *testing.T) {
	var requestPath string
	var requestHeader string
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		requestHeader = r.Header.Get("x-org-id")
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id":"msg_1",
			"type":"message",
			"role":"assistant",
			"model":"claude-test",
			"content":[{"type":"text","text":"pong"}],
			"stop_reason":"end_turn",
			"stop_sequence":null,
			"usage":{"input_tokens":2,"output_tokens":1}
		}`)
	}))
	defer server.Close()

	p, err := NewFromConfig("anthropic", config.ProviderConfig{
		Type:    "anthropic",
		APIKey:  "sk-test",
		BaseURL: server.URL,
		Headers: map[string]string{"x-org-id": "org-1"},
		Timeout: time.Second,
	})
	if err != nil {
		t.Fatalf("NewFromConfig: %v", err)
	}
	stream, err := p.Chat(context.Background(), provider.Request{
		Model: "claude-test",
		Messages: []message.Message{
			message.Text(message.RoleSystem, "system prompt"),
			message.Text(message.RoleUser, "ping"),
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	defer stream.Close()

	if requestPath != "/v1/messages" {
		t.Fatalf("request path = %q", requestPath)
	}
	if requestHeader != "org-1" {
		t.Fatalf("x-org-id = %q", requestHeader)
	}
	if requestBody["model"] != "claude-test" {
		t.Fatalf("model = %#v", requestBody["model"])
	}
	messages := requestBody["messages"].([]any)
	first := messages[0].(map[string]any)
	if first["role"] != "user" {
		t.Fatalf("first role = %#v", first["role"])
	}
	content := first["content"].([]any)[0].(map[string]any)
	if content["text"] != "ping" {
		t.Fatalf("message text = %#v", content["text"])
	}
	system := requestBody["system"].([]any)[0].(map[string]any)
	if system["text"] != "system prompt" {
		t.Fatalf("system text = %#v", system["text"])
	}

	event, err := stream.Next(context.Background())
	if err != nil || event.Type != provider.EventTextDelta || event.Text != "pong" {
		t.Fatalf("text event = %#v, err=%v", event, err)
	}
	event, err = stream.Next(context.Background())
	if err != nil || event.Type != provider.EventUsage || event.Usage.InputTokens != 2 || event.Usage.OutputTokens != 1 {
		t.Fatalf("usage event = %#v, err=%v", event, err)
	}
	event, err = stream.Next(context.Background())
	if err != nil || event.Type != provider.EventDone {
		t.Fatalf("done event = %#v, err=%v", event, err)
	}
	_, err = stream.Next(context.Background())
	if !errors.Is(err, io.EOF) {
		t.Fatalf("after done err = %v, want EOF", err)
	}
}

func TestChatRejectsUnsupportedBlocks(t *testing.T) {
	p, err := NewFromConfig("anthropic", config.ProviderConfig{
		Type:   "anthropic",
		APIKey: "sk-test",
	})
	if err != nil {
		t.Fatalf("NewFromConfig: %v", err)
	}
	_, err = p.Chat(context.Background(), provider.Request{
		Model: "claude-test",
		Messages: []message.Message{
			message.New(message.RoleUser, message.ImageBlock("https://example.test/image.png")),
		},
	})
	if err == nil {
		t.Fatal("expected unsupported block error")
	}
	if !strings.Contains(err.Error(), "does not support content block") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestChatRequiresModelAndMessages(t *testing.T) {
	p, err := NewFromConfig("anthropic", config.ProviderConfig{
		Type:   "anthropic",
		APIKey: "sk-test",
	})
	if err != nil {
		t.Fatalf("NewFromConfig: %v", err)
	}
	if _, err := p.Chat(context.Background(), provider.Request{}); err == nil || !strings.Contains(err.Error(), "model") {
		t.Fatalf("missing model error = %v", err)
	}
	if _, err := p.Chat(context.Background(), provider.Request{Model: "claude-test"}); err == nil || !strings.Contains(err.Error(), "at least one") {
		t.Fatalf("missing message error = %v", err)
	}
}
