package memory

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/feimingxliu/ub/internal/tool"
)

func execTool(t *testing.T, tl tool.Tool, args any) (tool.Result, error) {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return tl.Execute(context.Background(), raw)
}

func TestRemember_WorkspaceScopeDefault(t *testing.T) {
	ws := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	tl := newRememberTool(ws)
	res, err := execTool(t, tl, rememberArgs{Text: "build is `make build`"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(ws, ".ub/memory.md"))
	if err != nil {
		t.Fatalf("read memory: %v", err)
	}
	if !strings.Contains(string(body), "build is `make build`") {
		t.Fatalf("memory missing text:\n%s", body)
	}
	if !strings.Contains(res.Content, "remembered (workspace)") {
		t.Fatalf("Content = %q", res.Content)
	}
	if len(res.Files) != 1 || !strings.HasSuffix(res.Files[0].Path, ".ub/memory.md") {
		t.Fatalf("Files = %+v", res.Files)
	}
}

func TestRemember_GlobalScope(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	tl := newRememberTool(t.TempDir())
	res, err := execTool(t, tl, rememberArgs{Text: "prefer pnpm", Scope: "global"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(cfg, "ub/memory.md"))
	if err != nil {
		t.Fatalf("read global memory: %v", err)
	}
	if !strings.Contains(string(body), "prefer pnpm") {
		t.Fatalf("global memory missing text:\n%s", body)
	}
	if !strings.Contains(res.Content, "remembered (global)") {
		t.Fatalf("Content = %q", res.Content)
	}
}

func TestRemember_EmptyText(t *testing.T) {
	tl := newRememberTool(t.TempDir())
	_, err := execTool(t, tl, rememberArgs{Text: ""})
	if err == nil || !strings.Contains(err.Error(), "text is required") {
		t.Fatalf("expected text-required, got: %v", err)
	}
}

func TestRemember_InvalidScope(t *testing.T) {
	tl := newRememberTool(t.TempDir())
	_, err := execTool(t, tl, rememberArgs{Text: "x", Scope: "session"})
	if err == nil || !strings.Contains(err.Error(), "invalid scope") {
		t.Fatalf("expected invalid-scope, got: %v", err)
	}
}

func TestRegister_AddsTool(t *testing.T) {
	reg := tool.New()
	if err := Register(reg, t.TempDir()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, ok := reg.Get("remember"); !ok {
		t.Fatalf("remember not registered")
	}
}
