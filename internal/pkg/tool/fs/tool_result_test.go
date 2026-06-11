package fs

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/feimingxliu/ub/internal/pkg/tool"
	"github.com/feimingxliu/ub/internal/pkg/workspace/tooloutput"
)

// writeSpilloverFile mirrors tooloutput.writeSpillover for tests so we can
// pre-seed spillover content without going through the agent.
func writeSpilloverFile(t *testing.T, outputRoot, sessionID, toolUseID, content string) string {
	t.Helper()
	dir := filepath.Join(outputRoot, tooloutput.SafePathPart(sessionID))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dir, tooloutput.SafePathPart(toolUseID)+".txt")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func TestToolResult_HappyPath(t *testing.T) {
	outputRoot := t.TempDir()
	writeSpilloverFile(t, outputRoot, "sess-1", "tool-A", "alpha\nbeta\ngamma\n")

	tr := newToolResultTool(outputRoot, 100)
	raw, _ := json.Marshal(toolResultArgs{ToolUseID: "tool-A"})
	ctx := tool.WithSessionID(context.Background(), "sess-1")
	res, err := tr.Execute(ctx, raw)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(res.Content, "alpha") ||
		!strings.Contains(res.Content, "beta") ||
		!strings.Contains(res.Content, "gamma") {
		t.Fatalf("Content missing lines: %q", res.Content)
	}
}

func TestToolResult_MissingSessionID(t *testing.T) {
	tr := newToolResultTool(t.TempDir(), 100)
	raw, _ := json.Marshal(toolResultArgs{ToolUseID: "tool-A"})
	_, err := tr.Execute(context.Background(), raw)
	if err == nil || !strings.Contains(err.Error(), "session id") {
		t.Fatalf("expected session-id error, got: %v", err)
	}
}

func TestToolResult_MissingToolUseID(t *testing.T) {
	tr := newToolResultTool(t.TempDir(), 100)
	raw, _ := json.Marshal(toolResultArgs{})
	ctx := tool.WithSessionID(context.Background(), "sess-1")
	_, err := tr.Execute(ctx, raw)
	if err == nil || !strings.Contains(err.Error(), "tool_use_id is required") {
		t.Fatalf("expected required error, got: %v", err)
	}
}

func TestToolResult_FileNotFound(t *testing.T) {
	tr := newToolResultTool(t.TempDir(), 100)
	raw, _ := json.Marshal(toolResultArgs{ToolUseID: "missing"})
	ctx := tool.WithSessionID(context.Background(), "sess-1")
	_, err := tr.Execute(ctx, raw)
	if err == nil || !strings.Contains(err.Error(), "not found or output was not spilled") {
		t.Fatalf("expected not-found error, got: %v", err)
	}
}

func TestToolResult_OffsetLimit(t *testing.T) {
	outputRoot := t.TempDir()
	writeSpilloverFile(t, outputRoot, "sess-1", "tool-A", "1\n2\n3\n4\n5\n")

	tr := newToolResultTool(outputRoot, 100)
	raw, _ := json.Marshal(toolResultArgs{ToolUseID: "tool-A", Offset: 2, Limit: 2})
	ctx := tool.WithSessionID(context.Background(), "sess-1")
	res, err := tr.Execute(ctx, raw)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	// Must include lines 2 and 3, must exclude 1 and 4
	if !strings.Contains(res.Content, "\t2") || !strings.Contains(res.Content, "\t3") {
		t.Fatalf("missing requested lines: %q", res.Content)
	}
	for _, exclude := range []string{"\t1\n", "\t4\n", "\t5"} {
		if strings.Contains(res.Content, exclude) {
			t.Fatalf("unexpected line %q in:\n%s", exclude, res.Content)
		}
	}
}

func TestToolResult_DefaultMaxLinesTruncates(t *testing.T) {
	outputRoot := t.TempDir()
	var b strings.Builder
	for i := 1; i <= 50; i++ {
		b.WriteString("x\n")
	}
	writeSpilloverFile(t, outputRoot, "sess-1", "tool-A", b.String())

	tr := newToolResultTool(outputRoot, 5)
	raw, _ := json.Marshal(toolResultArgs{ToolUseID: "tool-A"})
	ctx := tool.WithSessionID(context.Background(), "sess-1")
	res, err := tr.Execute(ctx, raw)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(res.Content, "truncated") {
		t.Fatalf("expected truncation hint: %q", res.Content)
	}
	if res.FullContent == "" {
		t.Fatalf("expected FullContent to be populated for spillover handoff")
	}
}

func TestToolResult_SessionScoped(t *testing.T) {
	// A spillover file written under sess-A must not be readable when the
	// caller is in sess-B, even if the tool_use_id matches.
	outputRoot := t.TempDir()
	writeSpilloverFile(t, outputRoot, "sess-A", "tool-X", "secret\n")

	tr := newToolResultTool(outputRoot, 100)
	raw, _ := json.Marshal(toolResultArgs{ToolUseID: "tool-X"})
	ctx := tool.WithSessionID(context.Background(), "sess-B")
	_, err := tr.Execute(ctx, raw)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not-found for cross-session, got: %v", err)
	}
}

func TestToolResult_SanitizedIDsRoundTrip(t *testing.T) {
	// Session ID and tool use ID with characters that get sanitized must
	// still find the file (the writer uses the same SafePathPart).
	outputRoot := t.TempDir()
	sessionID := "sess/with:funny#chars"
	toolUseID := "tu_id with spaces & quotes"
	writeSpilloverFile(t, outputRoot, sessionID, toolUseID, "ok\n")

	tr := newToolResultTool(outputRoot, 100)
	raw, _ := json.Marshal(toolResultArgs{ToolUseID: toolUseID})
	ctx := tool.WithSessionID(context.Background(), sessionID)
	res, err := tr.Execute(ctx, raw)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(res.Content, "ok") {
		t.Fatalf("expected ok in content: %q", res.Content)
	}
}
