package provider

import (
	"encoding/json"
	"testing"

	"github.com/feimingxliu/ub/internal/message"
)

func TestSanitizeToolPairingWellFormed(t *testing.T) {
	msgs := []message.Message{
		message.Text(message.RoleUser, "hello"),
		message.New(message.RoleAssistant, message.ToolUseBlock("call_1", "read", json.RawMessage(`{"path":"a"}`))),
		message.New(message.RoleTool, message.ToolResultBlock("call_1", "content", false)),
	}
	out := SanitizeToolPairing(msgs)
	if len(out) != 3 {
		t.Fatalf("well-formed: got %d messages, want 3", len(out))
	}
	if out[1].Content[0].ToolUseID != "call_1" {
		t.Fatalf("assistant tool_use id = %q, want call_1", out[1].Content[0].ToolUseID)
	}
	if out[2].Content[0].ToolUseID != "call_1" {
		t.Fatalf("tool result id = %q, want call_1", out[2].Content[0].ToolUseID)
	}
}

func TestSanitizeToolPairingBackfillsMissingResult(t *testing.T) {
	msgs := []message.Message{
		message.Text(message.RoleUser, "hello"),
		message.New(message.RoleAssistant, message.ToolUseBlock("call_1", "read", json.RawMessage(`{"path":"a"}`))),
		// No tool result — interrupted session.
		message.Text(message.RoleUser, "continue"),
	}
	out := SanitizeToolPairing(msgs)
	// Expect: user, assistant+tool_use, tool_result(backfill), user
	if len(out) != 4 {
		t.Fatalf("missing result: got %d messages, want 4", len(out))
	}
	if out[2].Role != message.RoleTool {
		t.Fatalf("backfill role = %q, want tool", out[2].Role)
	}
	if out[2].Content[0].ToolUseID != "call_1" {
		t.Fatalf("backfill tool_use_id = %q, want call_1", out[2].Content[0].ToolUseID)
	}
	if out[2].Content[0].Output != interruptedToolResult {
		t.Fatalf("backfill output = %q, want interrupted placeholder", out[2].Content[0].Output)
	}
}

func TestSanitizeToolPairingDropsOrphanToolResult(t *testing.T) {
	msgs := []message.Message{
		message.Text(message.RoleUser, "hello"),
		message.New(message.RoleTool, message.ToolResultBlock("orphan_1", "leaked", false)),
		message.Text(message.RoleUser, "next"),
	}
	out := SanitizeToolPairing(msgs)
	// Expect: user, user (orphan dropped)
	if len(out) != 2 {
		t.Fatalf("orphan: got %d messages, want 2", len(out))
	}
	if out[0].Role != message.RoleUser || out[1].Role != message.RoleUser {
		t.Fatalf("orphan: roles = %q, %q; want user, user", out[0].Role, out[1].Role)
	}
}

func TestSanitizeToolPairingMultipleCalls(t *testing.T) {
	msgs := []message.Message{
		message.New(
			message.RoleAssistant,
			message.ToolUseBlock("call_1", "read", json.RawMessage(`{}`)),
			message.ToolUseBlock("call_2", "bash", json.RawMessage(`{}`)),
		),
		message.New(message.RoleTool, message.ToolResultBlock("call_1", "file", false)),
		// call_2 is missing its result
	}
	out := SanitizeToolPairing(msgs)
	// Expect: assistant, tool_result(call_1), tool_result(call_2 backfill)
	if len(out) != 3 {
		t.Fatalf("multiple: got %d messages, want 3", len(out))
	}
	if out[1].Content[0].ToolUseID != "call_1" {
		t.Fatalf("first result id = %q, want call_1", out[1].Content[0].ToolUseID)
	}
	if out[2].Content[0].ToolUseID != "call_2" {
		t.Fatalf("second result id = %q, want call_2", out[2].Content[0].ToolUseID)
	}
	if out[2].Content[0].Output != interruptedToolResult {
		t.Fatalf("second result output = %q, want interrupted placeholder", out[2].Content[0].Output)
	}
}

func TestSanitizeToolPairingReorderedResults(t *testing.T) {
	msgs := []message.Message{
		message.New(
			message.RoleAssistant,
			message.ToolUseBlock("call_1", "read", json.RawMessage(`{}`)),
			message.ToolUseBlock("call_2", "bash", json.RawMessage(`{}`)),
		),
		// Results arrive in reverse order
		message.New(message.RoleTool, message.ToolResultBlock("call_2", "output2", false)),
		message.New(message.RoleTool, message.ToolResultBlock("call_1", "output1", false)),
	}
	out := SanitizeToolPairing(msgs)
	// Results should be re-sorted to call order
	if len(out) != 3 {
		t.Fatalf("reordered: got %d messages, want 3", len(out))
	}
	if out[1].Content[0].ToolUseID != "call_1" {
		t.Fatalf("first result id = %q, want call_1", out[1].Content[0].ToolUseID)
	}
	if out[2].Content[0].ToolUseID != "call_2" {
		t.Fatalf("second result id = %q, want call_2", out[2].Content[0].ToolUseID)
	}
}

func TestSanitizeToolPairingNoToolCalls(t *testing.T) {
	msgs := []message.Message{
		message.Text(message.RoleUser, "hello"),
		message.Text(message.RoleAssistant, "world"),
	}
	out := SanitizeToolPairing(msgs)
	if len(out) != 2 {
		t.Fatalf("no tools: got %d messages, want 2", len(out))
	}
}
