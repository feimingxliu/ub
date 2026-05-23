package openai

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
	"github.com/feimingxliu/ub/internal/reasoning"
)

func TestNewFromConfigRequiresAPIKey(t *testing.T) {
	_, err := NewFromConfig("openai", config.ProviderConfig{Type: "openai"})
	if err == nil {
		t.Fatal("expected missing api key error")
	}
	if !strings.Contains(err.Error(), "api_key") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFactoryCreatesOpenAIProvider(t *testing.T) {
	p, err := provider.New("openai", config.ProviderConfig{
		Type:   "openai",
		APIKey: "sk-test",
	})
	if err != nil {
		t.Fatalf("provider.New: %v", err)
	}
	if p.Name() != "openai" {
		t.Fatalf("Name() = %q", p.Name())
	}
	if !p.Caps().SupportsStreaming {
		t.Fatalf("openai provider should support streaming")
	}
}

func TestToChatCompletionParamsSetsReasoningEffort(t *testing.T) {
	params, err := toChatCompletionParams(provider.Request{
		Model:     "gpt-test",
		Messages:  []message.Message{message.Text(message.RoleUser, "ping")},
		Reasoning: &reasoning.Config{Effort: reasoning.EffortHigh},
	})
	if err != nil {
		t.Fatalf("toChatCompletionParams: %v", err)
	}
	if string(params.ReasoningEffort) != "high" {
		t.Fatalf("ReasoningEffort = %q, want high", params.ReasoningEffort)
	}
}

func TestToChatCompletionParamsOmitsReasoningEffortForNone(t *testing.T) {
	params, err := toChatCompletionParams(provider.Request{
		Model:     "gpt-test",
		Messages:  []message.Message{message.Text(message.RoleUser, "ping")},
		Reasoning: &reasoning.Config{Effort: reasoning.EffortNone},
	})
	if err != nil {
		t.Fatalf("toChatCompletionParams: %v", err)
	}
	if params.ReasoningEffort != "" {
		t.Fatalf("ReasoningEffort = %q, want empty", params.ReasoningEffort)
	}
}

func TestChatSendsStreamingRequestAndReturnsEvents(t *testing.T) {
	var requestPath string
	var requestHeader string
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		requestHeader = r.Header.Get("x-org-id")
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		writeOpenAISSE(t, w, `{"id":"chatcmpl_1","object":"chat.completion.chunk","created":0,"model":"gpt-test","choices":[{"index":0,"delta":{"role":"assistant","content":"po"},"finish_reason":null}]}`)
		writeOpenAISSE(t, w, `{"id":"chatcmpl_1","object":"chat.completion.chunk","created":0,"model":"gpt-test","choices":[{"index":0,"delta":{"content":"ng"},"finish_reason":null}]}`)
		writeOpenAISSE(t, w, `{"id":"chatcmpl_1","object":"chat.completion.chunk","created":0,"model":"gpt-test","choices":[],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`)
		writeOpenAISSE(t, w, `[DONE]`)
	}))
	defer server.Close()

	p, err := NewFromConfig("openai", config.ProviderConfig{
		Type:    "openai",
		APIKey:  "sk-test",
		BaseURL: server.URL,
		Headers: map[string]string{"x-org-id": "org-1"},
		Timeout: time.Second,
	})
	if err != nil {
		t.Fatalf("NewFromConfig: %v", err)
	}
	stream, err := p.Chat(context.Background(), provider.Request{
		Model: "gpt-test",
		Messages: []message.Message{
			message.Text(message.RoleSystem, "system prompt"),
			message.Text(message.RoleUser, "ping"),
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	defer stream.Close()

	if requestPath != "/chat/completions" {
		t.Fatalf("request path = %q", requestPath)
	}
	if requestHeader != "org-1" {
		t.Fatalf("x-org-id = %q", requestHeader)
	}
	if requestBody["model"] != "gpt-test" {
		t.Fatalf("model = %#v", requestBody["model"])
	}
	if requestBody["stream"] != true {
		t.Fatalf("stream = %#v, want true", requestBody["stream"])
	}
	streamOptions := requestBody["stream_options"].(map[string]any)
	if streamOptions["include_usage"] != true {
		t.Fatalf("include_usage = %#v, want true", streamOptions["include_usage"])
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

func TestStreamCancelAndClose(t *testing.T) {
	stream := newSDKStream(nil)
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

func TestChatStreamsReasoningContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		writeOpenAISSE(t, w, `{"id":"chatcmpl_1","object":"chat.completion.chunk","created":0,"model":"gpt-test","choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":" checking"},"finish_reason":null}]}`)
		writeOpenAISSE(t, w, `{"id":"chatcmpl_1","object":"chat.completion.chunk","created":0,"model":"gpt-test","choices":[{"index":0,"delta":{"content":"answer"},"finish_reason":null}]}`)
		writeOpenAISSE(t, w, `[DONE]`)
	}))
	defer server.Close()

	p, err := NewFromConfig("openai", config.ProviderConfig{
		Type:    "openai",
		APIKey:  "sk-test",
		BaseURL: server.URL,
	})
	if err != nil {
		t.Fatalf("NewFromConfig: %v", err)
	}
	stream, err := p.Chat(context.Background(), provider.Request{
		Model:    "gpt-test",
		Messages: []message.Message{message.Text(message.RoleUser, "think")},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	defer stream.Close()

	event, err := stream.Next(context.Background())
	if err != nil || event.Type != provider.EventReasoningDelta || event.Reasoning != " checking" {
		t.Fatalf("reasoning event = %#v, err=%v", event, err)
	}
	event, err = stream.Next(context.Background())
	if err != nil || event.Type != provider.EventTextDelta || event.Text != "answer" {
		t.Fatalf("text event = %#v, err=%v", event, err)
	}
}

func TestChatSendsToolsAndToolMessages(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		writeOpenAISSE(t, w, `{"id":"chatcmpl_1","object":"chat.completion.chunk","created":0,"model":"gpt-test","choices":[{"index":0,"delta":{"role":"assistant","content":"ok"},"finish_reason":null}]}`)
		writeOpenAISSE(t, w, `[DONE]`)
	}))
	defer server.Close()

	p, err := NewFromConfig("openai", config.ProviderConfig{
		Type:    "openai",
		APIKey:  "sk-test",
		BaseURL: server.URL,
	})
	if err != nil {
		t.Fatalf("NewFromConfig: %v", err)
	}
	stream, err := p.Chat(context.Background(), provider.Request{
		Model: "gpt-test",
		Tools: []provider.ToolDefinition{{
			Name:        "read",
			Description: "Read a file.",
			Schema:      json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`),
		}},
		Messages: []message.Message{
			message.Text(message.RoleUser, "read main.go"),
			message.New(message.RoleAssistant, message.ToolUseBlock("call_1", "read", json.RawMessage(`{"path":"main.go"}`))),
			message.New(message.RoleTool, message.ToolResultBlock("call_1", "file content", false)),
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	defer stream.Close()

	tools := requestBody["tools"].([]any)
	fn := tools[0].(map[string]any)["function"].(map[string]any)
	if fn["name"] != "read" || fn["description"] != "Read a file." {
		t.Fatalf("tool function = %#v", fn)
	}
	messages := requestBody["messages"].([]any)
	assistant := messages[1].(map[string]any)
	calls := assistant["tool_calls"].([]any)
	call := calls[0].(map[string]any)
	if call["id"] != "call_1" || call["type"] != "function" {
		t.Fatalf("assistant tool call = %#v", call)
	}
	toolMsg := messages[2].(map[string]any)
	if toolMsg["role"] != "tool" || toolMsg["tool_call_id"] != "call_1" || toolMsg["content"] != "file content" {
		t.Fatalf("tool message = %#v", toolMsg)
	}
}

func TestChatStreamsToolCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		writeOpenAISSE(t, w, `{"id":"chatcmpl_1","object":"chat.completion.chunk","created":0,"model":"gpt-test","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read","arguments":"{\"path\""}}]},"finish_reason":null}]}`)
		writeOpenAISSE(t, w, `{"id":"chatcmpl_1","object":"chat.completion.chunk","created":0,"model":"gpt-test","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":":\"main.go\"}"}}]},"finish_reason":"tool_calls"}]}`)
		writeOpenAISSE(t, w, `[DONE]`)
	}))
	defer server.Close()

	p, err := NewFromConfig("openai", config.ProviderConfig{
		Type:    "openai",
		APIKey:  "sk-test",
		BaseURL: server.URL,
	})
	if err != nil {
		t.Fatalf("NewFromConfig: %v", err)
	}
	stream, err := p.Chat(context.Background(), provider.Request{
		Model:    "gpt-test",
		Messages: []message.Message{message.Text(message.RoleUser, "read")},
		Tools:    []provider.ToolDefinition{{Name: "read", Schema: json.RawMessage(`{"type":"object"}`)}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	defer stream.Close()

	event, err := stream.Next(context.Background())
	if err != nil || event.Type != provider.EventToolCall || event.ToolUseID != "call_1" || event.ToolName != "read" || string(event.Input) != `{"path":"main.go"}` {
		t.Fatalf("tool event = %#v, err=%v", event, err)
	}
}

func TestChatToolCallTruncatedArgsEmitsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// First chunk delivers a partial JSON fragment; stream then ends with
		// finish_reason="length" (max_output_tokens) before the args complete.
		writeOpenAISSE(t, w, `{"id":"chatcmpl_1","object":"chat.completion.chunk","created":0,"model":"gpt-test","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read","arguments":"{\"path\":\"mai"}}]},"finish_reason":null}]}`)
		writeOpenAISSE(t, w, `{"id":"chatcmpl_1","object":"chat.completion.chunk","created":0,"model":"gpt-test","choices":[{"index":0,"delta":{},"finish_reason":"length"}]}`)
		writeOpenAISSE(t, w, `[DONE]`)
	}))
	defer server.Close()

	p, err := NewFromConfig("openai", config.ProviderConfig{
		Type:    "openai",
		APIKey:  "sk-test",
		BaseURL: server.URL,
	})
	if err != nil {
		t.Fatalf("NewFromConfig: %v", err)
	}
	stream, err := p.Chat(context.Background(), provider.Request{
		Model:    "gpt-test",
		Messages: []message.Message{message.Text(message.RoleUser, "read")},
		Tools:    []provider.ToolDefinition{{Name: "read", Schema: json.RawMessage(`{"type":"object"}`)}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	defer stream.Close()

	event, err := stream.Next(context.Background())
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if event.Type != provider.EventError {
		t.Fatalf("event = %#v, want EventError", event)
	}
	if event.Err == nil || !strings.Contains(event.Err.Error(), "truncated") {
		t.Fatalf("err = %v, want truncated marker", event.Err)
	}
}

func TestChatRejectsUnsupportedBlocks(t *testing.T) {
	p, err := NewFromConfig("openai", config.ProviderConfig{
		Type:   "openai",
		APIKey: "sk-test",
	})
	if err != nil {
		t.Fatalf("NewFromConfig: %v", err)
	}
	_, err = p.Chat(context.Background(), provider.Request{
		Model: "gpt-test",
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
	p, err := NewFromConfig("openai", config.ProviderConfig{
		Type:   "openai",
		APIKey: "sk-test",
	})
	if err != nil {
		t.Fatalf("NewFromConfig: %v", err)
	}
	if _, err := p.Chat(context.Background(), provider.Request{}); err == nil || !strings.Contains(err.Error(), "model") {
		t.Fatalf("missing model error = %v", err)
	}
	if _, err := p.Chat(context.Background(), provider.Request{Model: "gpt-test"}); err == nil || !strings.Contains(err.Error(), "at least one") {
		t.Fatalf("missing message error = %v", err)
	}
}

func writeOpenAISSE(t *testing.T, w io.Writer, data string) {
	t.Helper()
	if _, err := io.WriteString(w, "data: "+data+"\n\n"); err != nil {
		t.Fatal(err)
	}
}
