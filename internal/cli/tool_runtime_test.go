package cli

import (
	"context"
	"testing"

	"github.com/feimingxliu/ub/internal/config"
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
}
