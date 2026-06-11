package tooloutput

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/feimingxliu/ub/internal/pkg/tool"
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

func TestLimitResult_FullMaxBytesCap(t *testing.T) {
	stateRoot := t.TempDir()
	// 200KB content with a 50KB cap. Content lines use a stable prefix so we
	// can check that we truncated on a UTF-8 boundary.
	full := strings.Repeat("abcdefghijklmnop\n", 200*1024/17+1)
	cap := 50 * 1024
	result, err := LimitResult(tool.Result{Content: full}, LimitOptions{
		SessionID: "s",
		ToolUseID: "t",
		StateRoot: stateRoot,
		Limits: Limits{
			InlineMaxBytes:   512,
			InlineMaxLines:   3,
			SpilloverEnabled: true,
			FullMaxBytes:     cap,
		},
	})
	if err != nil {
		t.Fatalf("LimitResult: %v", err)
	}
	if result.OriginalBytes != len([]byte(full)) {
		t.Fatalf("OriginalBytes = %d, want %d", result.OriginalBytes, len([]byte(full)))
	}
	raw, err := os.ReadFile(result.FullOutputPath)
	if err != nil {
		t.Fatalf("read spillover: %v", err)
	}
	if !strings.Contains(string(raw), "spillover truncated") {
		t.Fatalf("expected truncation footer in spillover, got tail:\n%q", string(raw[max(0, len(raw)-200):]))
	}
	// File size should be roughly cap + footer line (much smaller than full).
	if len(raw) > cap+200 {
		t.Fatalf("spillover file too large: %d > cap %d + slack", len(raw), cap)
	}
}

func TestLimitResult_CustomSpilloverDir(t *testing.T) {
	stateRoot := t.TempDir()
	customDir := t.TempDir()
	result, err := LimitResult(tool.Result{Content: strings.Repeat("line\n", 100)}, LimitOptions{
		SessionID: "sess",
		ToolUseID: "tool",
		StateRoot: stateRoot,
		Limits: Limits{
			InlineMaxBytes:   512,
			InlineMaxLines:   3,
			SpilloverEnabled: true,
			SpilloverDir:     customDir,
		},
	})
	if err != nil {
		t.Fatalf("LimitResult: %v", err)
	}
	if !strings.HasPrefix(result.FullOutputPath, customDir) {
		t.Fatalf("spillover not under custom dir: %s (custom=%s)", result.FullOutputPath, customDir)
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
