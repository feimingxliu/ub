package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/feimingxliu/ub/internal/config"
	"github.com/feimingxliu/ub/internal/message"
	"github.com/feimingxliu/ub/internal/provider"
	"github.com/feimingxliu/ub/internal/rollout"
	"github.com/feimingxliu/ub/internal/store"
)

var captureRequests []provider.Request

func init() {
	provider.Register("capture", func(name string, cfg config.ProviderConfig) (provider.Provider, error) {
		return captureProvider{name: name}, nil
	})
}

type captureProvider struct {
	name string
}

func (p captureProvider) Name() string {
	return p.name
}

func (p captureProvider) Caps() provider.Caps {
	return provider.Caps{SupportsStreaming: true}
}

func (p captureProvider) Chat(ctx context.Context, req provider.Request) (provider.Stream, error) {
	copied := provider.Request{Model: req.Model, Messages: cloneTestMessages(req.Messages)}
	captureRequests = append(captureRequests, copied)
	return &captureStream{events: []provider.Event{
		{Type: provider.EventTextDelta, Text: "captured"},
		{Type: provider.EventDone},
	}}, nil
}

type captureStream struct {
	events []provider.Event
	next   int
}

func (s *captureStream) Next(ctx context.Context) (provider.Event, error) {
	if err := ctx.Err(); err != nil {
		return provider.Event{}, err
	}
	if s.next >= len(s.events) {
		return provider.Event{}, io.EOF
	}
	event := s.events[s.next]
	s.next++
	return event, nil
}

func (s *captureStream) Close() error {
	return nil
}

func cloneTestMessages(messages []message.Message) []message.Message {
	out := make([]message.Message, len(messages))
	for i, msg := range messages {
		out[i] = msg.Clone()
	}
	return out
}

func TestChatWithFakeProviderPrintsTextDelta(t *testing.T) {
	temp := t.TempDir()
	writeChatConfig(t, temp, `default_model: fake/test-model
providers:
  fake:
    type: fake
    script:
      - type: text_delta
        text: pong
      - type: done
`)
	t.Chdir(temp)

	cmd := newRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"chat", "--provider", "fake", "ping"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat: %v", err)
	}
	if got := out.String(); got != "pong" {
		t.Fatalf("stdout = %q, want pong", got)
	}
}

func TestChatReadsPromptFromStdin(t *testing.T) {
	temp := t.TempDir()
	writeChatConfig(t, temp, `providers:
  fake:
    type: fake
    script:
      - type: text_delta
        text: stdin-ok
      - type: done
`)
	t.Chdir(temp)

	cmd := newRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(strings.NewReader("hello from stdin"))
	cmd.SetArgs([]string{"chat", "--provider", "fake", "-"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat stdin: %v", err)
	}
	if got := out.String(); got != "stdin-ok" {
		t.Fatalf("stdout = %q, want stdin-ok", got)
	}
}

func TestChatUsesProviderOverride(t *testing.T) {
	temp := t.TempDir()
	writeChatConfig(t, temp, `default_model: first/model
providers:
  first:
    type: fake
    script:
      - type: text_delta
        text: first
  second:
    type: fake
    script:
      - type: text_delta
        text: second
`)
	t.Chdir(temp)

	cmd := newRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"chat", "--provider", "second", "hello"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat provider override: %v", err)
	}
	if got := out.String(); got != "second" {
		t.Fatalf("stdout = %q, want second", got)
	}
}

func TestChatRejectsToolCalls(t *testing.T) {
	temp := t.TempDir()
	writeChatConfig(t, temp, `providers:
  fake:
    type: fake
    script:
      - type: tool_call
        tool_name: fs.read
        input:
          path: main.go
`)
	t.Chdir(temp)

	cmd := newRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"chat", "--provider", "fake", "hello"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected tool call error")
	}
	if !strings.Contains(err.Error(), "does not execute tool calls") || !strings.Contains(err.Error(), "fs.read") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestChatInfersProviderFromDefaultModel(t *testing.T) {
	temp := t.TempDir()
	writeChatConfig(t, temp, `default_model: fake/test-model
providers:
  fake:
    type: fake
    script:
      - type: text_delta
        text: inferred
`)
	t.Chdir(temp)

	cmd := newRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"chat", "hello"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat inferred provider: %v", err)
	}
	if got := out.String(); got != "inferred" {
		t.Fatalf("stdout = %q, want inferred", got)
	}
}

func TestChatAppliesDevProfile(t *testing.T) {
	temp := t.TempDir()
	writeChatConfig(t, temp, `default_model: fake/base
providers:
  fake:
    type: fake
    script:
      - type: text_delta
        text: base
profiles:
  dev:
    default_model: fake/dev
    providers:
      fake:
        type: fake
        script:
          - type: text_delta
            text: dev
`)
	t.Chdir(temp)

	cmd := newRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--dev", "chat", "hello"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat dev profile: %v", err)
	}
	if got := out.String(); got != "dev" {
		t.Fatalf("stdout = %q, want dev", got)
	}
}

func TestChatContinuesSessionWithHistory(t *testing.T) {
	captureRequests = nil
	temp := t.TempDir()
	writeChatConfig(t, temp, `default_model: capture/test-model
providers:
  capture:
    type: capture
`)
	t.Chdir(temp)

	cmd := newRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"chat", "first"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("first chat: %v", err)
	}
	sessions := readOnlySessions(t, temp)
	if len(sessions) != 1 {
		t.Fatalf("sessions len = %d, want 1", len(sessions))
	}
	sessionID := sessions[0].ID

	captureRequests = nil
	cmd = newRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"chat", "--session", sessionID, "second"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("continued chat: %v", err)
	}
	if got := out.String(); got != "captured" {
		t.Fatalf("stdout = %q, want captured", got)
	}
	if len(captureRequests) != 1 {
		t.Fatalf("capture request len = %d, want 1", len(captureRequests))
	}
	messages := captureRequests[0].Messages
	if len(messages) != 3 {
		t.Fatalf("messages len = %d, want 3: %#v", len(messages), messages)
	}
	if messages[0].Role != message.RoleUser || messages[0].Text() != "first" {
		t.Fatalf("first history message = %#v", messages[0])
	}
	if messages[1].Role != message.RoleAssistant || messages[1].Text() != "captured" {
		t.Fatalf("assistant history message = %#v", messages[1])
	}
	if messages[2].Role != message.RoleUser || messages[2].Text() != "second" {
		t.Fatalf("new user message = %#v", messages[2])
	}
	events := readOnlySessionEvents(t, temp)
	if events[len(events)-1].Turn != 2 {
		t.Fatalf("last event turn = %d, want 2; events=%#v", events[len(events)-1].Turn, events)
	}
}

func TestChatNewCreatesDistinctSession(t *testing.T) {
	temp := t.TempDir()
	writeChatConfig(t, temp, `providers:
  fake:
    type: fake
    script:
      - type: text_delta
        text: ok
`)
	t.Chdir(temp)

	for _, prompt := range []string{"first", "second"} {
		cmd := newRootCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"chat", "--provider", "fake", "--new", prompt})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("chat --new %q: %v", prompt, err)
		}
	}
	sessions := readOnlySessions(t, temp)
	if len(sessions) != 2 {
		t.Fatalf("sessions len = %d, want 2: %#v", len(sessions), sessions)
	}
}

func TestChatRejectsSessionErrors(t *testing.T) {
	temp := t.TempDir()
	writeChatConfig(t, temp, `providers:
  fake:
    type: fake
`)
	t.Chdir(temp)

	cmd := newRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"chat", "--provider", "fake", "--session", "missing", "hello"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "session") || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing session error = %v", err)
	}

	cmd = newRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"chat", "--provider", "fake", "--session", "s1", "--new", "hello"})
	err = cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "cannot use --new with --session") {
		t.Fatalf("conflict error = %v", err)
	}
}

func TestChatProviderMissingError(t *testing.T) {
	temp := t.TempDir()
	writeChatConfig(t, temp, `providers: {}`)
	t.Chdir(temp)

	cmd := newRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"chat", "--provider", "missing", "hello"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "provider") || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("provider missing error = %v", err)
	}
}

func TestChatWithAnthropicProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		writeSSE(t, w, "message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-test","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":0}}}`)
		writeSSE(t, w, "content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
		writeSSE(t, w, "content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"anthropic-ok"}}`)
		writeSSE(t, w, "content_block_stop", `{"type":"content_block_stop","index":0}`)
		writeSSE(t, w, "message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":1,"output_tokens":1}}`)
		writeSSE(t, w, "message_stop", `{"type":"message_stop"}`)
	}))
	defer server.Close()

	temp := t.TempDir()
	writeChatConfig(t, temp, `providers:
  anthropic:
    type: anthropic
    api_key: sk-test
    base_url: `+server.URL+`
`)
	t.Chdir(temp)

	cmd := newRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"chat", "--provider", "anthropic", "--model", "claude-test", "hello"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat anthropic: %v", err)
	}
	if got := out.String(); got != "anthropic-ok" {
		t.Fatalf("stdout = %q, want anthropic-ok", got)
	}
}

func TestChatWithOpenAIProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		writeOpenAIChatSSE(t, w, `{"id":"chatcmpl_1","object":"chat.completion.chunk","created":0,"model":"gpt-test","choices":[{"index":0,"delta":{"role":"assistant","content":"openai-ok"},"finish_reason":null}]}`)
		writeOpenAIChatSSE(t, w, `{"id":"chatcmpl_1","object":"chat.completion.chunk","created":0,"model":"gpt-test","choices":[],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
		writeOpenAIChatSSE(t, w, `[DONE]`)
	}))
	defer server.Close()

	temp := t.TempDir()
	writeChatConfig(t, temp, `providers:
  openai:
    type: openai
    api_key: sk-test
    base_url: `+server.URL+`
`)
	t.Chdir(temp)

	cmd := newRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"chat", "--provider", "openai", "--model", "gpt-test", "hello"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat openai: %v", err)
	}
	if got := out.String(); got != "openai-ok" {
		t.Fatalf("stdout = %q, want openai-ok", got)
	}
}

func TestChatWithCompatProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		writeOpenAIChatSSE(t, w, `{"id":"chatcmpl_1","object":"chat.completion.chunk","created":0,"model":"local-test","choices":[{"index":0,"delta":{"role":"assistant","content":"compat-ok"},"finish_reason":null}]}`)
		writeOpenAIChatSSE(t, w, `{"id":"chatcmpl_1","object":"chat.completion.chunk","created":0,"model":"local-test","choices":[],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
		writeOpenAIChatSSE(t, w, `[DONE]`)
	}))
	defer server.Close()

	temp := t.TempDir()
	writeChatConfig(t, temp, `providers:
  compat:
    type: openai-compat
    base_url: `+server.URL+`
`)
	t.Chdir(temp)

	cmd := newRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"chat", "--provider", "compat", "--model", "local-test", "hello"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat compat: %v", err)
	}
	if got := out.String(); got != "compat-ok" {
		t.Fatalf("stdout = %q, want compat-ok", got)
	}
}

func TestChatWithOllamaProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		writeOllamaChatLine(t, w, `{"model":"qwen-test","message":{"role":"assistant","content":"ollama-ok"},"done":false}`)
		writeOllamaChatLine(t, w, `{"model":"qwen-test","done":true,"prompt_eval_count":1,"eval_count":1}`)
	}))
	defer server.Close()

	temp := t.TempDir()
	writeChatConfig(t, temp, `providers:
  ollama:
    type: ollama
    base_url: `+server.URL+`
`)
	t.Chdir(temp)

	cmd := newRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"chat", "--provider", "ollama", "--model", "qwen-test", "hello"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat ollama: %v", err)
	}
	if got := out.String(); got != "ollama-ok" {
		t.Fatalf("stdout = %q, want ollama-ok", got)
	}
}

func TestChatWritesSessionAndRolloutEvents(t *testing.T) {
	temp := t.TempDir()
	writeChatConfig(t, temp, `providers:
  fake:
    type: fake
    script:
      - type: text_delta
        text: pong
      - type: usage
        input_tokens: 2
        output_tokens: 1
      - type: done
`)
	t.Chdir(temp)

	cmd := newRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"chat", "--provider", "fake", "hello rollout"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat: %v", err)
	}
	if got := out.String(); got != "pong" {
		t.Fatalf("stdout should only contain provider text, got %q", got)
	}

	events := readOnlySessionEvents(t, temp)
	assertEventTypes(t, events, []rollout.Type{rollout.TypeUserMessage, rollout.TypeUsage, rollout.TypeAssistantMessage})
	var assistant rollout.MessagePayload
	if err := json.Unmarshal(events[2].Payload, &assistant); err != nil {
		t.Fatalf("assistant payload: %v", err)
	}
	if assistant.Text != "pong" {
		t.Fatalf("assistant text = %q, want pong", assistant.Text)
	}
}

func TestChatWritesProviderErrorEvent(t *testing.T) {
	temp := t.TempDir()
	writeChatConfig(t, temp, `providers:
  fake:
    type: fake
    script:
      - type: error
        error: boom
`)
	t.Chdir(temp)

	cmd := newRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"chat", "--provider", "fake", "hello"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected chat error")
	}
	events := readOnlySessionEvents(t, temp)
	assertEventTypes(t, events, []rollout.Type{rollout.TypeUserMessage, rollout.TypeError})
	var payload rollout.ErrorPayload
	if err := json.Unmarshal(events[1].Payload, &payload); err != nil {
		t.Fatalf("error payload: %v", err)
	}
	if !strings.Contains(payload.Message, "boom") {
		t.Fatalf("error payload = %#v", payload)
	}
}

func TestSelectChatProviderRequiresProvider(t *testing.T) {
	_, _, err := selectChatProvider(nil, "", "")
	if err == nil {
		t.Fatal("expected provider selection error")
	}
}

func TestChatTitle(t *testing.T) {
	if got := chatTitle("  hello\nworld  "); got != "hello world" {
		t.Fatalf("title = %q", got)
	}
	if got := chatTitle(""); got != "(empty prompt)" {
		t.Fatalf("empty title = %q", got)
	}
	if got := chatTitle(strings.Repeat("a", 80)); len(got) != 60 || !strings.HasSuffix(got, "...") {
		t.Fatalf("long title = %q len=%d", got, len(got))
	}
}

func writeChatConfig(t *testing.T, temp, content string) {
	t.Helper()
	xdg := filepath.Join(temp, "xdg")
	configPath := filepath.Join(xdg, "ub", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("XDG_DATA_HOME", filepath.Join(temp, "data"))
}

func readOnlySessionEvents(t *testing.T, workspace string) []rollout.Event {
	t.Helper()
	sessions := readOnlySessions(t, workspace)
	if len(sessions) != 1 {
		t.Fatalf("sessions len = %d, want 1: %#v", len(sessions), sessions)
	}
	path, err := store.DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	st, err := store.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()
	ro, err := rollout.New(st)
	if err != nil {
		t.Fatalf("rollout.New: %v", err)
	}
	var events []rollout.Event
	if err := ro.ForEach(context.Background(), sessions[0].ID, func(event rollout.Event) error {
		events = append(events, event)
		return nil
	}); err != nil {
		t.Fatalf("ForEach: %v", err)
	}
	return events
}

func readOnlySessions(t *testing.T, workspace string) []store.Session {
	t.Helper()
	path, err := store.DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	st, err := store.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()
	sessions, err := st.ListSessions(context.Background(), workspace, 10)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	return sessions
}

func assertEventTypes(t *testing.T, events []rollout.Event, want []rollout.Type) {
	t.Helper()
	if len(events) != len(want) {
		t.Fatalf("events len = %d, want %d: %#v", len(events), len(want), events)
	}
	for i, typ := range want {
		if events[i].Type != typ {
			t.Fatalf("event[%d].Type = %q, want %q; events=%#v", i, events[i].Type, typ, events)
		}
	}
}

func writeSSE(t *testing.T, w io.Writer, event, data string) {
	t.Helper()
	if _, err := io.WriteString(w, "event: "+event+"\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(w, "data: "+data+"\n\n"); err != nil {
		t.Fatal(err)
	}
}

func writeOpenAIChatSSE(t *testing.T, w io.Writer, data string) {
	t.Helper()
	if _, err := io.WriteString(w, "data: "+data+"\n\n"); err != nil {
		t.Fatal(err)
	}
}

func writeOllamaChatLine(t *testing.T, w io.Writer, data string) {
	t.Helper()
	if _, err := io.WriteString(w, data+"\n"); err != nil {
		t.Fatal(err)
	}
}
