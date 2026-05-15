package compat

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/feimingxliu/ub/internal/config"
	"github.com/feimingxliu/ub/internal/message"
	"github.com/feimingxliu/ub/internal/provider"
)

func TestNewFromConfigRequiresBaseURL(t *testing.T) {
	_, err := NewFromConfig("compat", config.ProviderConfig{Type: "openai-compat"})
	if err == nil {
		t.Fatal("expected missing base_url error")
	}
	if !strings.Contains(err.Error(), "base_url") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFactoryCreatesCompatProviderWithoutAPIKey(t *testing.T) {
	p, err := provider.New("compat", config.ProviderConfig{
		Type:    "openai-compat",
		BaseURL: "http://127.0.0.1:1",
	})
	if err != nil {
		t.Fatalf("provider.New: %v", err)
	}
	if p.Name() != "compat" {
		t.Fatalf("Name() = %q", p.Name())
	}
	if !p.Caps().SupportsStreaming {
		t.Fatalf("compat provider should support streaming")
	}
}

func TestChatReturnsOpenAICompatibleEvents(t *testing.T) {
	var requestPath string
	var authHeader string
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		authHeader = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		writeCompatSSE(t, w, `{"id":"chatcmpl_1","object":"chat.completion.chunk","created":0,"model":"local-test","choices":[{"index":0,"delta":{"role":"assistant","content":"po"},"finish_reason":null}]}`)
		writeCompatSSE(t, w, `{"id":"chatcmpl_1","object":"chat.completion.chunk","created":0,"model":"local-test","choices":[{"index":0,"delta":{"content":"ng"},"finish_reason":null}]}`)
		writeCompatSSE(t, w, `{"id":"chatcmpl_1","object":"chat.completion.chunk","created":0,"model":"local-test","choices":[],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`)
		writeCompatSSE(t, w, `[DONE]`)
	}))
	defer server.Close()

	p, err := NewFromConfig("compat", config.ProviderConfig{
		Type:    "openai-compat",
		BaseURL: server.URL,
	})
	if err != nil {
		t.Fatalf("NewFromConfig: %v", err)
	}
	stream, err := p.Chat(context.Background(), provider.Request{
		Model: "local-test",
		Tools: []provider.ToolDefinition{{
			Name:   "read",
			Schema: json.RawMessage(`{"type":"object"}`),
		}},
		Messages: []message.Message{
			message.Text(message.RoleUser, "ping"),
			message.New(message.RoleAssistant, message.ToolUseBlock("call_1", "read", json.RawMessage(`{"path":"main.go"}`))),
			message.New(message.RoleTool, message.ToolResultBlock("call_1", "file content", false)),
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	defer stream.Close()

	if requestPath != "/chat/completions" {
		t.Fatalf("request path = %q", requestPath)
	}
	if authHeader != "Bearer unused" {
		t.Fatalf("Authorization = %q", authHeader)
	}
	if requestBody["model"] != "local-test" || requestBody["stream"] != true {
		t.Fatalf("request body = %#v", requestBody)
	}
	if len(requestBody["tools"].([]any)) != 1 {
		t.Fatalf("tools = %#v", requestBody["tools"])
	}
	messages := requestBody["messages"].([]any)
	toolMsg := messages[2].(map[string]any)
	if toolMsg["role"] != "tool" || toolMsg["tool_call_id"] != "call_1" {
		t.Fatalf("tool message = %#v", toolMsg)
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
	p, err := NewFromConfig("compat", config.ProviderConfig{
		Type:    "openai-compat",
		BaseURL: "http://127.0.0.1:1",
	})
	if err != nil {
		t.Fatalf("NewFromConfig: %v", err)
	}
	_, err = p.Chat(context.Background(), provider.Request{
		Model: "local-test",
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

func writeCompatSSE(t *testing.T, w io.Writer, data string) {
	t.Helper()
	if _, err := io.WriteString(w, "data: "+data+"\n\n"); err != nil {
		t.Fatal(err)
	}
}
