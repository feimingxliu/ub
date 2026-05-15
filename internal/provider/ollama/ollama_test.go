package ollama

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

func TestFactoryCreatesOllamaProvider(t *testing.T) {
	p, err := provider.New("ollama", config.ProviderConfig{Type: "ollama"})
	if err != nil {
		t.Fatalf("provider.New: %v", err)
	}
	if p.Name() != "ollama" {
		t.Fatalf("Name() = %q", p.Name())
	}
	if !p.Caps().SupportsStreaming {
		t.Fatalf("ollama provider should support streaming")
	}
}

func TestNewFromConfigUsesDefaultBaseURL(t *testing.T) {
	p, err := NewFromConfig("ollama", config.ProviderConfig{Type: "ollama"})
	if err != nil {
		t.Fatalf("NewFromConfig: %v", err)
	}
	op := p.(*Provider)
	if op.baseURL != defaultBaseURL {
		t.Fatalf("baseURL = %q, want %q", op.baseURL, defaultBaseURL)
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
		w.Header().Set("Content-Type", "application/x-ndjson")
		writeOllamaLine(t, w, `{"model":"qwen-test","message":{"role":"assistant","content":"po"},"done":false}`)
		writeOllamaLine(t, w, `{"model":"qwen-test","message":{"role":"assistant","content":"ng"},"done":false}`)
		writeOllamaLine(t, w, `{"model":"qwen-test","done":true,"prompt_eval_count":2,"eval_count":1}`)
	}))
	defer server.Close()

	p, err := NewFromConfig("ollama", config.ProviderConfig{
		Type:    "ollama",
		BaseURL: server.URL,
		Headers: map[string]string{"x-org-id": "org-1"},
		Timeout: time.Second,
	})
	if err != nil {
		t.Fatalf("NewFromConfig: %v", err)
	}
	stream, err := p.Chat(context.Background(), provider.Request{
		Model: "qwen-test",
		Messages: []message.Message{
			message.Text(message.RoleSystem, "system prompt"),
			message.Text(message.RoleUser, "ping"),
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	defer stream.Close()

	if requestPath != "/api/chat" {
		t.Fatalf("request path = %q", requestPath)
	}
	if requestHeader != "org-1" {
		t.Fatalf("x-org-id = %q", requestHeader)
	}
	if requestBody["model"] != "qwen-test" || requestBody["stream"] != true {
		t.Fatalf("request body = %#v", requestBody)
	}
	messages := requestBody["messages"].([]any)
	system := messages[0].(map[string]any)
	if system["role"] != "system" || system["content"] != "system prompt" {
		t.Fatalf("system message = %#v", system)
	}
	user := messages[1].(map[string]any)
	if user["role"] != "user" || user["content"] != "ping" {
		t.Fatalf("user message = %#v", user)
	}

	event, err := stream.Next(context.Background())
	if err != nil || event.Type != provider.EventTextDelta || event.Text != "po" {
		t.Fatalf("text event = %#v, err=%v", event, err)
	}
	event, err = stream.Next(context.Background())
	if err != nil || event.Type != provider.EventTextDelta || event.Text != "ng" {
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
	p, err := NewFromConfig("ollama", config.ProviderConfig{Type: "ollama"})
	if err != nil {
		t.Fatalf("NewFromConfig: %v", err)
	}
	_, err = p.Chat(context.Background(), provider.Request{
		Model: "qwen-test",
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
	p, err := NewFromConfig("ollama", config.ProviderConfig{Type: "ollama"})
	if err != nil {
		t.Fatalf("NewFromConfig: %v", err)
	}
	if _, err := p.Chat(context.Background(), provider.Request{}); err == nil || !strings.Contains(err.Error(), "model") {
		t.Fatalf("missing model error = %v", err)
	}
	if _, err := p.Chat(context.Background(), provider.Request{Model: "qwen-test"}); err == nil || !strings.Contains(err.Error(), "at least one") {
		t.Fatalf("missing message error = %v", err)
	}
}

func TestChatStatusError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad model", http.StatusBadRequest)
	}))
	defer server.Close()

	p, err := NewFromConfig("ollama", config.ProviderConfig{
		Type:    "ollama",
		BaseURL: server.URL,
	})
	if err != nil {
		t.Fatalf("NewFromConfig: %v", err)
	}
	_, err = p.Chat(context.Background(), provider.Request{
		Model:    "missing",
		Messages: []message.Message{message.Text(message.RoleUser, "ping")},
	})
	if err == nil {
		t.Fatal("expected status error")
	}
	if !strings.Contains(err.Error(), "status 400") || !strings.Contains(err.Error(), "bad model") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStreamCancelAndClose(t *testing.T) {
	stream := newStream(io.NopCloser(strings.NewReader("")))
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
}

func writeOllamaLine(t *testing.T, w io.Writer, data string) {
	t.Helper()
	if _, err := io.WriteString(w, data+"\n"); err != nil {
		t.Fatal(err)
	}
}
