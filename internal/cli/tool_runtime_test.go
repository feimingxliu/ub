package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/feimingxliu/ub/internal/config"
	"github.com/feimingxliu/ub/internal/tool"
)

func TestToolRuntimeKeepsLocalToolsWhenMCPServerFails(t *testing.T) {
	cfg := config.Defaults()
	cfg.MCPServers = map[string]config.MCPServerConfig{
		"offline": {Type: "http", URL: "http://127.0.0.1:1/mcp"},
	}

	runtime, err := newToolRuntime(context.Background(), cfg)
	if err != nil {
		t.Fatalf("newToolRuntime: %v", err)
	}
	defer runtime.Close()
	if len(runtime.Warnings) != 1 {
		t.Fatalf("warnings = %d, want 1: %#v", len(runtime.Warnings), runtime.Warnings)
	}
	if _, ok := runtime.Registry.Get("read"); !ok {
		t.Fatalf("local read tool missing after MCP failure")
	}
	if _, ok := runtime.Registry.Get("todo_write"); !ok {
		t.Fatalf("local todo_write tool missing after MCP failure")
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	if runtime.Workspace != cwd {
		t.Fatalf("workspace = %q, want %q", runtime.Workspace, cwd)
	}
}

func TestToolRuntimeUsesConfiguredSpilloverDirForReadTools(t *testing.T) {
	cfg := config.Defaults()
	customDir := t.TempDir()
	cfg.Context.ToolResults.SpilloverDir = customDir

	runtime, err := newToolRuntime(context.Background(), cfg)
	if err != nil {
		t.Fatalf("newToolRuntime: %v", err)
	}
	defer runtime.Close()

	outputPath := filepath.Join(customDir, "sess-1", "tool-1.txt")
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(outputPath, []byte("full output\n"), 0o600); err != nil {
		t.Fatalf("write output: %v", err)
	}

	readTool, ok := runtime.Registry.Get("read")
	if !ok {
		t.Fatalf("read tool missing")
	}
	raw, _ := json.Marshal(map[string]any{"path": outputPath})
	res, err := readTool.Execute(context.Background(), raw)
	if err != nil {
		t.Fatalf("read custom spillover: %v", err)
	}
	if !strings.Contains(res.Content, "full output") {
		t.Fatalf("read result = %q, want custom spillover content", res.Content)
	}

	resultTool, ok := runtime.Registry.Get("tool_result")
	if !ok {
		t.Fatalf("tool_result missing")
	}
	raw, _ = json.Marshal(map[string]any{"tool_use_id": "tool-1"})
	res, err = resultTool.Execute(tool.WithSessionID(context.Background(), "sess-1"), raw)
	if err != nil {
		t.Fatalf("tool_result custom spillover: %v", err)
	}
	if !strings.Contains(res.Content, "full output") {
		t.Fatalf("tool_result = %q, want custom spillover content", res.Content)
	}
}
