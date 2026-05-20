package tooloutput

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/feimingxliu/ub/internal/tool"
)

func TestLimitResultSpillsFullOutputAndCapsVisibleContent(t *testing.T) {
	stateRoot := t.TempDir()
	result, err := LimitResult(tool.Result{Content: strings.Repeat("line\n", 20)}, LimitOptions{
		SessionID: "sess/1",
		ToolUseID: "tool:1",
		StateRoot: stateRoot,
		Limits: Limits{
			InlineMaxBytes:   512,
			InlineMaxLines:   3,
			SpilloverEnabled: true,
		},
	})
	if err != nil {
		t.Fatalf("LimitResult: %v", err)
	}
	if !result.Truncated || result.OriginalBytes == 0 || result.FullOutputPath == "" {
		t.Fatalf("metadata not set: %#v", result)
	}
	if len([]byte(result.Content)) > 512 {
		t.Fatalf("content bytes = %d, want <= 512:\n%s", len([]byte(result.Content)), result.Content)
	}
	if strings.Count(result.Content, "\n") > 5 {
		t.Fatalf("too many visible lines:\n%s", result.Content)
	}
	if !strings.Contains(result.Content, "full_output_path=") {
		t.Fatalf("missing spillover footer:\n%s", result.Content)
	}
	raw, err := os.ReadFile(result.FullOutputPath)
	if err != nil {
		t.Fatalf("read spillover: %v", err)
	}
	if string(raw) != strings.Repeat("line\n", 20) {
		t.Fatalf("spillover content mismatch")
	}
	if filepath.Dir(filepath.Dir(result.FullOutputPath)) != filepath.Join(stateRoot, "tool_outputs") {
		t.Fatalf("spillover path = %s", result.FullOutputPath)
	}
}

func TestLimitResultUsesFullContentFromAlreadyPreviewedTool(t *testing.T) {
	stateRoot := t.TempDir()
	result, err := LimitResult(tool.Result{
		Content:     "1\tfirst\n... (truncated, use offset/limit)",
		FullContent: "1\tfirst\n2\tsecond\n3\tthird",
	}, LimitOptions{
		SessionID: "sess",
		ToolUseID: "read-call",
		StateRoot: stateRoot,
		Limits: Limits{
			InlineMaxBytes:   200,
			InlineMaxLines:   20,
			SpilloverEnabled: true,
		},
	})
	if err != nil {
		t.Fatalf("LimitResult: %v", err)
	}
	if !result.Truncated || result.OriginalBytes != len([]byte("1\tfirst\n2\tsecond\n3\tthird")) {
		t.Fatalf("metadata = %#v", result)
	}
	raw, err := os.ReadFile(result.FullOutputPath)
	if err != nil {
		t.Fatalf("read spillover: %v", err)
	}
	if string(raw) != "1\tfirst\n2\tsecond\n3\tthird" {
		t.Fatalf("spillover = %q", raw)
	}
}
