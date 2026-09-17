package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/feimingxliu/ub/internal/config"
	"github.com/feimingxliu/ub/internal/message"
	"github.com/feimingxliu/ub/internal/provider"
)

func TestChatSystemMessageCompatibility(t *testing.T) {
	for _, factory := range []struct {
		name string
		new  func(string, config.ProviderConfig) (provider.Provider, error)
	}{{"openai", NewFromConfig}, {"openai-compat", NewCompatibleFromConfig}} {
		for _, enabled := range []bool{false, true} {
			name := factory.name + "/preserve"
			if enabled {
				name = factory.name + "/merge"
			}
			t.Run(name, func(t *testing.T) {
				var body struct {
					Messages []struct {
						Role       string `json:"role"`
						Content    string `json:"content"`
						ToolCallID string `json:"tool_call_id"`
					} `json:"messages"`
				}
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					w.Header().Set("Content-Type", "text/event-stream")
					writeOpenAISSE(t, w, `[DONE]`)
				}))
				defer server.Close()
				p, err := factory.new("local", config.ProviderConfig{APIKey: "test", BaseURL: server.URL, MergeSystemMessages: enabled})
				if err != nil {
					t.Fatal(err)
				}
				history := []message.Message{
					message.Text(message.RoleSystem, "instructions"),
					message.Text(message.RoleSystem, "environment"),
					message.Text(message.RoleUser, "read file"),
					message.New(message.RoleAssistant, message.ToolUseBlock("call_1", "read", json.RawMessage(`{"path":"test.txt"}`))),
					message.New(message.RoleTool, message.ToolResultBlock("call_1", "file body", false)),
					message.Text(message.RoleSystem, "finish now"),
				}
				before, _ := json.Marshal(history)
				stream, err := p.Chat(context.Background(), provider.Request{Model: "qwen3.5-4b", Messages: history})
				if err != nil {
					t.Fatal(err)
				}
				defer stream.Close()
				var roles []string
				for _, msg := range body.Messages {
					roles = append(roles, msg.Role)
				}
				want := []string{"system", "system", "user", "assistant", "tool", "system"}
				toolIndex := 4
				if enabled {
					want = []string{"system", "user", "assistant", "tool"}
					toolIndex = 3
					if body.Messages[0].Content != "instructions\n\nenvironment\n\nfinish now" {
						t.Fatalf("system = %q", body.Messages[0].Content)
					}
				}
				if !reflect.DeepEqual(roles, want) {
					t.Fatalf("roles = %v, want %v", roles, want)
				}
				if body.Messages[toolIndex].ToolCallID != "call_1" || body.Messages[toolIndex].Content != "file body" {
					t.Fatalf("tool result = %#v", body.Messages[toolIndex])
				}
				after, _ := json.Marshal(history)
				if string(before) != string(after) {
					t.Fatal("request conversion mutated history")
				}
			})
		}
	}
}

func TestMergeSystemMessagesEdgeCases(t *testing.T) {
	for _, msgs := range [][]message.Message{
		nil,
		{message.Text(message.RoleUser, "hello")},
		{message.Text(message.RoleSystem, "")},
		{message.Text(message.RoleSystem, "one")},
	} {
		got, err := mergeSystemMessages(msgs)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != len(msgs) {
			t.Fatalf("got %v, want %v", got, msgs)
		}
		if len(msgs) > 0 && !reflect.DeepEqual(got, msgs) {
			t.Fatalf("got %v, want %v", got, msgs)
		}
	}
	_, err := mergeSystemMessages([]message.Message{message.New(message.RoleSystem, message.ImageBlock("https://example.test/image.png"))})
	if err == nil {
		t.Fatal("unsupported system blocks must not be silently discarded")
	}
}

func TestToolSchemaPreservesNestedDefinitions(t *testing.T) {
	for _, namespace := range []string{"$defs", "definitions"} {
		t.Run(namespace, func(t *testing.T) {
			raw := strings.ReplaceAll(`{
    "$ref":"#/DEFS/Args",
    "DEFS":{
     "Args":{"type":"object","properties":{"questions":{"type":"array","items":{"$ref":"#/DEFS/Question"}}}},
     "Question":{"type":"object","properties":{"options":{"type":"array","items":{"$ref":"#/DEFS/Option"}}}},
     "Option":{"type":"object","properties":{"label":{"type":"string"},"next":{"$ref":"#/DEFS/Option"}}}
    }
   }`, "DEFS", namespace)
			tools, err := toToolParams([]provider.ToolDefinition{{Name: "ask", Schema: json.RawMessage(raw)}})
			if err != nil {
				t.Fatal(err)
			}
			wire, err := json.Marshal(tools)
			if err != nil {
				t.Fatal(err)
			}
			var decoded []struct {
				Function struct {
					Parameters map[string]any `json:"parameters"`
				} `json:"function"`
			}
			if err := json.Unmarshal(wire, &decoded); err != nil {
				t.Fatal(err)
			}
			root := decoded[0].Function.Parameters
			if root["type"] != "object" || root["$ref"] != nil {
				t.Fatalf("root = %v", root)
			}
			var original map[string]any
			if err := json.Unmarshal([]byte(raw), &original); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(root[namespace], original[namespace]) {
				t.Fatalf("definitions lost: %s", wire)
			}
			// Check the actual nested reference is still present, not just unused defs.
			props := root["properties"].(map[string]any)
			items := props["questions"].(map[string]any)["items"].(map[string]any)
			if items["$ref"] != "#/"+namespace+"/Question" {
				t.Fatalf("items = %v", items)
			}
		})
	}
}

func TestChatIgnoresSSEHeartbeats(t *testing.T) {
	for _, malformed := range []bool{false, true} {
		name := "text-and-tools"
		if malformed {
			name = "malformed-data"
		}
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
				// llama.cpp sends comment-only frames during long prompt processing.
				_, _ = w.Write([]byte(":\n\n: keepalive\r\n\r\n\n"))
				if malformed {
					writeOpenAISSE(t, w, `{"choices":`)
					return
				}
				writeOpenAISSE(t, w, `{"choices":[{"index":0,"delta":{"content":"checking"}}]}`)
				_, _ = w.Write([]byte(": ping\n\n"))
				writeOpenAISSE(t, w, `{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`)
				_, _ = w.Write([]byte(":\n\n"))
				writeOpenAISSE(t, w, `{"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":2}}`)
				writeOpenAISSE(t, w, `[DONE]`)
			}))
			defer server.Close()
			p, err := NewCompatibleFromConfig("local", config.ProviderConfig{BaseURL: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			stream, err := p.Chat(context.Background(), provider.Request{Model: "qwen", Messages: []message.Message{message.Text(message.RoleUser, "read")}})
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			if malformed {
				if _, err := stream.Next(context.Background()); err == nil {
					t.Fatal("malformed JSON data must still fail")
				}
				return
			}
			for _, want := range []provider.EventType{provider.EventTextDelta, provider.EventToolCall, provider.EventUsage, provider.EventDone} {
				got, err := stream.Next(context.Background())
				if err != nil || got.Type != want {
					t.Fatalf("event = %#v, error = %v, want %s", got, err, want)
				}
			}
		})
	}
}
