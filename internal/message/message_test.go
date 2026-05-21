package message

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestTextMessage(t *testing.T) {
	msg := Text(RoleUser, "hello")
	if msg.Role != RoleUser {
		t.Fatalf("Role = %q, want %q", msg.Role, RoleUser)
	}
	if len(msg.Content) != 1 {
		t.Fatalf("Content len = %d, want 1", len(msg.Content))
	}
	block := msg.Content[0]
	if block.Type != BlockText || block.Text != "hello" {
		t.Fatalf("block = %+v, want text hello", block)
	}
}

func TestRoleAndBlockTypeJSON(t *testing.T) {
	msg := New(RoleAssistant, TextBlock("hi"))
	raw, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("Unmarshal map: %v", err)
	}
	if decoded["role"] != "assistant" {
		t.Fatalf("role JSON = %v, want assistant; raw=%s", decoded["role"], raw)
	}
	content := decoded["content"].([]any)
	block := content[0].(map[string]any)
	if block["type"] != "text" {
		t.Fatalf("block type JSON = %v, want text; raw=%s", block["type"], raw)
	}
}

func TestTextBlockJSONOmitEmpty(t *testing.T) {
	raw, err := json.Marshal(TextBlock("hello"))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("Unmarshal map: %v", err)
	}
	if decoded["type"] != "text" || decoded["text"] != "hello" {
		t.Fatalf("unexpected text block JSON: %s", raw)
	}
	for _, omitted := range []string{"image_url", "tool_use_id", "tool_name", "input", "output", "is_error"} {
		if _, ok := decoded[omitted]; ok {
			t.Fatalf("text block JSON includes %q: %s", omitted, raw)
		}
	}
}

func TestToolUseInputRawJSONAndRoundTrip(t *testing.T) {
	input := json.RawMessage(`{"path":"README.md"}`)
	block := ToolUseBlock("call-1", "fs.read", input)
	raw, err := json.Marshal(block)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(raw), `"{\"path\"`) {
		t.Fatalf("input was encoded as a string: %s", raw)
	}
	if !bytes.Contains(raw, []byte(`"input":{"path":"README.md"}`)) {
		t.Fatalf("input object missing from JSON: %s", raw)
	}

	var decoded ContentBlock
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("Unmarshal ContentBlock: %v", err)
	}
	if decoded.Type != BlockToolUse ||
		decoded.ToolUseID != "call-1" ||
		decoded.ToolName != "fs.read" ||
		string(decoded.Input) != string(input) {
		t.Fatalf("round-trip block mismatch: %+v", decoded)
	}
}

func TestToolResultErrorJSON(t *testing.T) {
	block := ToolResultBlock("call-1", "failed", true)
	raw, err := json.Marshal(block)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("Unmarshal map: %v", err)
	}
	if decoded["type"] != "tool_result" ||
		decoded["tool_use_id"] != "call-1" ||
		decoded["output"] != "failed" ||
		decoded["is_error"] != true {
		t.Fatalf("unexpected tool_result JSON: %s", raw)
	}
}

func TestMixedContentJSONRoundTrip(t *testing.T) {
	msg := New(
		RoleAssistant,
		TextBlock("reading"),
		ToolUseBlock("call-1", "fs.read", json.RawMessage(`{"path":"README.md"}`)),
		ToolResultBlock("call-1", "contents", false),
	)
	raw, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got Message
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("Unmarshal Message: %v", err)
	}
	if !reflect.DeepEqual(got, msg) {
		t.Fatalf("round-trip mismatch\ngot:  %#v\nwant: %#v\nraw: %s", got, msg, raw)
	}
	if got.Content[0].Type != BlockText ||
		got.Content[1].Type != BlockToolUse ||
		got.Content[2].Type != BlockToolResult {
		t.Fatalf("content order changed: %+v", got.Content)
	}
}

func TestMessageText(t *testing.T) {
	cases := []struct {
		name string
		msg  Message
		want string
	}{
		{
			name: "single text",
			msg:  New(RoleUser, TextBlock("a")),
			want: "a",
		},
		{
			name: "mixed blocks",
			msg: New(
				RoleAssistant,
				TextBlock("a"),
				ToolUseBlock("call-1", "tool", json.RawMessage(`{}`)),
				TextBlock("b"),
			),
			want: "a\nb",
		},
		{
			name: "no text",
			msg:  New(RoleAssistant, ToolResultBlock("call-1", "ok", false)),
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.msg.Text(); got != tc.want {
				t.Fatalf("Text() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAppendKeepsOrderAndOriginal(t *testing.T) {
	original := New(RoleAssistant, TextBlock("a"))
	appended := original.Append(ToolUseBlock("call-1", "tool", json.RawMessage(`{}`)))

	if len(original.Content) != 1 {
		t.Fatalf("original mutated: %+v", original.Content)
	}
	if len(appended.Content) != 2 {
		t.Fatalf("appended len = %d, want 2", len(appended.Content))
	}
	if appended.Content[0].Type != BlockText || appended.Content[1].Type != BlockToolUse {
		t.Fatalf("append order wrong: %+v", appended.Content)
	}
}

func TestCloneDeepCopiesContentAndRawMessage(t *testing.T) {
	original := New(
		RoleAssistant,
		TextBlock("a"),
		ToolUseBlock("call-1", "tool", json.RawMessage(`{"x":1}`)),
	)
	clone := original.Clone()

	clone.Content[0].Text = "changed"
	clone.Content[1].Input[5] = '2'

	if original.Content[0].Text != "a" {
		t.Fatalf("original text changed: %+v", original.Content[0])
	}
	if string(original.Content[1].Input) != `{"x":1}` {
		t.Fatalf("original raw input changed: %s", original.Content[1].Input)
	}
}

func TestConstructorsCloneRawInput(t *testing.T) {
	input := json.RawMessage(`{"x":1}`)
	block := ToolUseBlock("call-1", "tool", input)
	input[5] = '2'
	if string(block.Input) != `{"x":1}` {
		t.Fatalf("ToolUseBlock kept caller-owned input: %s", block.Input)
	}

	msg := New(RoleAssistant, block)
	block.Input[5] = '3'
	if string(msg.Content[0].Input) != `{"x":1}` {
		t.Fatalf("New kept caller-owned block input: %s", msg.Content[0].Input)
	}
}
