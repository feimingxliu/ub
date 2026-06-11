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

	"github.com/feimingxliu/ub/internal/pkg/core/config"
	"github.com/feimingxliu/ub/internal/pkg/core/message"
	"github.com/feimingxliu/ub/internal/pkg/core/reasoning"
	"github.com/feimingxliu/ub/internal/pkg/llm/provider"
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

func TestToChatCompletionParamsIgnoresHiddenReasoningBlocks(t *testing.T) {
	params, err := toChatCompletionParams(provider.Request{
		Model: "gpt-test",
		Messages: []message.Message{
			message.New(
				message.RoleAssistant,
				message.ReasoningBlock("hidden thinking", "sig"),
				message.TextBlock("visible answer"),
			),
		},
	})
	if err != nil {
		t.Fatalf("toChatCompletionParams: %v", err)
	}
	raw, err := json.Marshal(params.Messages)
	if err != nil {
		t.Fatalf("Marshal messages: %v", err)
	}
	body := string(raw)
	if strings.Contains(body, "hidden thinking") || strings.Contains(body, "reasoning") {
		t.Fatalf("messages JSON = %s, want hidden reasoning omitted", body)
	}
	if !strings.Contains(body, "visible answer") {
		t.Fatalf("messages JSON = %s, want visible answer", body)
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

func TestChatFlattensRefToolSchema(t *testing.T) {
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
			Name: "bash",
			Schema: json.RawMessage(`{
				"$ref": "#/$defs/bashArgs",
				"$defs": {
					"bashArgs": {
						"type": "object",
						"properties": {
							"command": {"type": "string"}
						},
						"required": ["command"]
					}
				}
			}`),
		}},
		Messages: []message.Message{message.Text(message.RoleUser, "hello")},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	defer stream.Close()

	tools := requestBody["tools"].([]any)
	fn := tools[0].(map[string]any)["function"].(map[string]any)
	parameters := fn["parameters"].(map[string]any)
	if parameters["type"] != "object" {
		t.Fatalf("parameters type = %#v, want object; parameters=%#v", parameters["type"], parameters)
	}
	if _, ok := parameters["$ref"]; ok {
		t.Fatalf("parameters should not keep top-level $ref: %#v", parameters)
	}
	props := parameters["properties"].(map[string]any)
	if _, ok := props["command"].(map[string]any); !ok {
		t.Fatalf("parameters missing command property: %#v", parameters)
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

func TestCapsForModel(t *testing.T) {
	p, err := NewFromConfig("openai", config.ProviderConfig{
		Type:   "openai",
		APIKey: "sk-test",
	})
	if err != nil {
		t.Fatalf("NewFromConfig: %v", err)
	}
	defaultCaps := p.Caps()
	if defaultCaps.MaxContextTokens != 128_000 {
		t.Fatalf("default MaxContextTokens = %d, want 128000", defaultCaps.MaxContextTokens)
	}
	concrete := p.(*Provider)
	// o-series reasoning models should get 200K.
	o1Caps := concrete.CapsForModel("o1-preview")
	if o1Caps.MaxContextTokens != 200_000 {
		t.Fatalf("o1 MaxContextTokens = %d, want 200000", o1Caps.MaxContextTokens)
	}
	// GPT-4o should get 128K.
	gpt4oCaps := concrete.CapsForModel("gpt-4o")
	if gpt4oCaps.MaxContextTokens != 128_000 {
		t.Fatalf("gpt-4o MaxContextTokens = %d, want 128000", gpt4oCaps.MaxContextTokens)
	}
	// Unknown model should defer to default.
	unknownCaps := concrete.CapsForModel("unknown-model")
	if unknownCaps.MaxContextTokens != 128_000 {
		t.Fatalf("unknown MaxContextTokens = %d, want 128000 (default)", unknownCaps.MaxContextTokens)
	}
}

func TestOpenAIModelContextTokens(t *testing.T) {
	tests := []struct {
		model string
		want  int
	}{
		{"o1-preview", 200_000},
		{"o1-mini", 200_000},
		{"o3-mini", 200_000},
		{"o4-mini", 200_000},
		{"gpt-4o", 128_000},
		{"gpt-4o-mini", 128_000},
		{"gpt-4-turbo", 128_000},
		{"gpt-5", 200_000},
		{"gpt-3.5-turbo", 16_385},
		{"openai/gpt-4o", 128_000},
		{"unknown", 0},
	}
	for _, tt := range tests {
		got := openAIModelContextTokens(tt.model)
		if got != tt.want {
			t.Errorf("openAIModelContextTokens(%q) = %d, want %d", tt.model, got, tt.want)
		}
	}
}

func writeOpenAISSE(t *testing.T, w io.Writer, data string) {
	t.Helper()
	if _, err := io.WriteString(w, "data: "+data+"\n\n"); err != nil {
		t.Fatal(err)
	}
}
