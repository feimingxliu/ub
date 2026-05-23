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

	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/feimingxliu/ub/internal/config"
	"github.com/feimingxliu/ub/internal/message"
	"github.com/feimingxliu/ub/internal/provider"
	"github.com/feimingxliu/ub/internal/reasoning"
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
	if !p.Caps().SupportsStreaming {
		t.Fatalf("I-10 provider should support streaming")
	}
}

func TestToMessageParamsSetsThinkingBudget(t *testing.T) {
	params, err := toMessageParams(provider.Request{
		Model:     "claude-test",
		Messages:  []message.Message{message.Text(message.RoleUser, "ping")},
		Reasoning: &reasoning.Config{Effort: reasoning.EffortHigh},
	})
	if err != nil {
		t.Fatalf("toMessageParams: %v", err)
	}
	budget := params.Thinking.GetBudgetTokens()
	if budget == nil || *budget != 4096 {
		t.Fatalf("thinking budget = %v, want 4096", budget)
	}
	if params.MaxTokens <= 4096 {
		t.Fatalf("MaxTokens = %d, want greater than thinking budget", params.MaxTokens)
	}
}

func TestBuildClientSendsBothAuthHeaders(t *testing.T) {
	var apiKey, auth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiKey = r.Header.Get("X-Api-Key")
		auth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[],"has_more":false}`))
	}))
	defer server.Close()

	client := BuildClient(config.ProviderConfig{
		Type:    "anthropic",
		APIKey:  "sk-test",
		BaseURL: server.URL,
	})
	_, err := client.Models.List(context.Background(), sdk.ModelListParams{})
	if err != nil {
		t.Fatalf("Models.List: %v", err)
	}
	if apiKey != "sk-test" {
		t.Fatalf("X-Api-Key = %q, want sk-test", apiKey)
	}
	if auth != "Bearer sk-test" {
		t.Fatalf("Authorization = %q, want Bearer sk-test", auth)
	}
}

func TestToMessageParamsOmitsThinkingForNone(t *testing.T) {
	params, err := toMessageParams(provider.Request{
		Model:     "claude-test",
		Messages:  []message.Message{message.Text(message.RoleUser, "ping")},
		Reasoning: &reasoning.Config{Effort: reasoning.EffortNone},
	})
	if err != nil {
		t.Fatalf("toMessageParams: %v", err)
	}
	if budget := params.Thinking.GetBudgetTokens(); budget != nil {
		t.Fatalf("thinking budget = %v, want nil", *budget)
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
		w.Header().Set("Content-Type", "text/event-stream")
		writeAnthropicSSE(t, w, "message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-test","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":2,"output_tokens":0}}}`)
		writeAnthropicSSE(t, w, "content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
		writeAnthropicSSE(t, w, "content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"po"}}`)
		writeAnthropicSSE(t, w, "content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ng"}}`)
		writeAnthropicSSE(t, w, "content_block_stop", `{"type":"content_block_stop","index":0}`)
		writeAnthropicSSE(t, w, "message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":2,"output_tokens":1}}`)
		writeAnthropicSSE(t, w, "message_stop", `{"type":"message_stop"}`)
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
	if requestBody["stream"] != true {
		t.Fatalf("stream = %#v, want true", requestBody["stream"])
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

func TestChatSendsToolsAndToolMessages(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		writeAnthropicSSE(t, w, "message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-test","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":2,"output_tokens":0}}}`)
		writeAnthropicSSE(t, w, "content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
		writeAnthropicSSE(t, w, "content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}`)
		writeAnthropicSSE(t, w, "message_stop", `{"type":"message_stop"}`)
	}))
	defer server.Close()

	p, err := NewFromConfig("anthropic", config.ProviderConfig{
		Type:    "anthropic",
		APIKey:  "sk-test",
		BaseURL: server.URL,
	})
	if err != nil {
		t.Fatalf("NewFromConfig: %v", err)
	}
	stream, err := p.Chat(context.Background(), provider.Request{
		Model: "claude-test",
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
	toolDef := tools[0].(map[string]any)
	if toolDef["name"] != "read" || toolDef["description"] != "Read a file." {
		t.Fatalf("tool = %#v", toolDef)
	}
	messages := requestBody["messages"].([]any)
	assistantContent := messages[1].(map[string]any)["content"].([]any)[0].(map[string]any)
	if assistantContent["type"] != "tool_use" || assistantContent["id"] != "call_1" || assistantContent["name"] != "read" {
		t.Fatalf("assistant tool_use = %#v", assistantContent)
	}
	toolContent := messages[2].(map[string]any)["content"].([]any)[0].(map[string]any)
	if toolContent["type"] != "tool_result" || toolContent["tool_use_id"] != "call_1" {
		t.Fatalf("tool_result = %#v", toolContent)
	}
}

func TestChatStreamsToolCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		writeAnthropicSSE(t, w, "message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-test","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":2,"output_tokens":0}}}`)
		writeAnthropicSSE(t, w, "content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"call_1","name":"read","input":{}}}`)
		writeAnthropicSSE(t, w, "content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"path\""}}`)
		writeAnthropicSSE(t, w, "content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":":\"main.go\"}"}}`)
		writeAnthropicSSE(t, w, "content_block_stop", `{"type":"content_block_stop","index":0}`)
		writeAnthropicSSE(t, w, "message_stop", `{"type":"message_stop"}`)
	}))
	defer server.Close()

	p, err := NewFromConfig("anthropic", config.ProviderConfig{
		Type:    "anthropic",
		APIKey:  "sk-test",
		BaseURL: server.URL,
	})
	if err != nil {
		t.Fatalf("NewFromConfig: %v", err)
	}
	stream, err := p.Chat(context.Background(), provider.Request{
		Model:    "claude-test",
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
		writeAnthropicSSE(t, w, "message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-test","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":2,"output_tokens":0}}}`)
		writeAnthropicSSE(t, w, "content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"call_1","name":"read","input":{}}}`)
		// Only the opening fragment of the args is delivered; content_block_stop
		// arrives before the JSON is well-formed (e.g., max_output_tokens hit).
		writeAnthropicSSE(t, w, "content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"path\":\"mai"}}`)
		writeAnthropicSSE(t, w, "content_block_stop", `{"type":"content_block_stop","index":0}`)
		writeAnthropicSSE(t, w, "message_stop", `{"type":"message_stop"}`)
	}))
	defer server.Close()

	p, err := NewFromConfig("anthropic", config.ProviderConfig{
		Type:    "anthropic",
		APIKey:  "sk-test",
		BaseURL: server.URL,
	})
	if err != nil {
		t.Fatalf("NewFromConfig: %v", err)
	}
	stream, err := p.Chat(context.Background(), provider.Request{
		Model:    "claude-test",
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

func TestChatStreamsThinkingDelta(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		writeAnthropicSSE(t, w, "message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-test","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":2,"output_tokens":0}}}`)
		writeAnthropicSSE(t, w, "content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"initial"}}`)
		writeAnthropicSSE(t, w, "content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":" step"}}`)
		writeAnthropicSSE(t, w, "content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig"}}`)
		writeAnthropicSSE(t, w, "content_block_stop", `{"type":"content_block_stop","index":0}`)
		writeAnthropicSSE(t, w, "message_stop", `{"type":"message_stop"}`)
	}))
	defer server.Close()

	p, err := NewFromConfig("anthropic", config.ProviderConfig{
		Type:    "anthropic",
		APIKey:  "sk-test",
		BaseURL: server.URL,
	})
	if err != nil {
		t.Fatalf("NewFromConfig: %v", err)
	}
	stream, err := p.Chat(context.Background(), provider.Request{
		Model:    "claude-test",
		Messages: []message.Message{message.Text(message.RoleUser, "think")},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	defer stream.Close()

	event, err := stream.Next(context.Background())
	if err != nil || event.Type != provider.EventReasoningDelta || event.Reasoning != "initial" {
		t.Fatalf("initial thinking event = %#v, err=%v", event, err)
	}
	event, err = stream.Next(context.Background())
	if err != nil || event.Type != provider.EventReasoningDelta || event.Reasoning != " step" {
		t.Fatalf("thinking delta event = %#v, err=%v", event, err)
	}
	event, err = stream.Next(context.Background())
	if err != nil || event.Type != provider.EventDone {
		t.Fatalf("done event = %#v, err=%v", event, err)
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

func writeAnthropicSSE(t *testing.T, w io.Writer, event, data string) {
	t.Helper()
	if _, err := io.WriteString(w, "event: "+event+"\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(w, "data: "+data+"\n\n"); err != nil {
		t.Fatal(err)
	}
}
