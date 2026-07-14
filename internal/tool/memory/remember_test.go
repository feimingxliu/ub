package memory

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/feimingxliu/ub/internal/tool"
	"github.com/feimingxliu/ub/internal/workspace/memory"
)

func execTool(t *testing.T, tl tool.Tool, args any) (tool.Result, error) {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return tl.Execute(context.Background(), raw)
}

func TestRemember_AutoScopeDefault(t *testing.T) {
	ws := t.TempDir()
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	tl := newRememberTool(ws)
	res, err := execTool(t, tl, rememberArgs{Text: "build is `make build`", Category: "project"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	// Read via memory.Path to get the actual file location.
	path, err := memory.Path(ws, memory.ScopeAuto)
	if err != nil {
		t.Fatalf("memory path: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read memory: %v", err)
	}
	if !strings.Contains(string(body), "build is `make build`") {
		t.Fatalf("memory missing text:\n%s", body)
	}
	if !strings.Contains(res.Content, "remembered (auto, project)") {
		t.Fatalf("Content = %q", res.Content)
	}
	if len(res.Files) != 1 {
		t.Fatalf("Files = %+v", res.Files)
	}
	if res.Metadata["memory_scope"] != "auto" ||
		res.Metadata["memory_category"] != "project" ||
		res.Metadata["memory_text"] != "build is `make build`" ||
		res.Metadata["memory_path"] == "" ||
		res.Metadata["memory_action"] == "" {
		t.Fatalf("memory metadata = %#v", res.Metadata)
	}
}

func TestRemember_WorkspaceBackwardCompat(t *testing.T) {
	ws := t.TempDir()
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	tl := newRememberTool(ws)
	res, err := execTool(t, tl, rememberArgs{Text: "test fact", Scope: "workspace"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(res.Content, "remembered (auto, general)") {
		t.Fatalf("workspace scope should map to auto: Content = %q", res.Content)
	}
}

func TestRemember_GlobalScope(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	tl := newRememberTool(t.TempDir())
	res, err := execTool(t, tl, rememberArgs{Text: "prefer pnpm", Scope: "global", Category: "preference"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(cfg, "ub", "instructions.md"))
	if err != nil {
		t.Fatalf("read global instructions: %v", err)
	}
	if !strings.Contains(string(body), "prefer pnpm") {
		t.Fatalf("global instructions missing text:\n%s", body)
	}
	if !strings.Contains(res.Content, "remembered (global, preference)") {
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

func TestRemember_InvalidCategory(t *testing.T) {
	tl := newRememberTool(t.TempDir())
	_, err := execTool(t, tl, rememberArgs{Text: "x", Category: "nope"})
	if err == nil || !strings.Contains(err.Error(), "invalid category") {
		t.Fatalf("expected invalid-category, got: %v", err)
	}
}

func TestRecall_ByQuery(t *testing.T) {
	ws := t.TempDir()
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	// Write some entries first.
	rt := newRememberTool(ws)
	execTool(t, rt, rememberArgs{Text: "build is `make build`", Category: "project"})
	execTool(t, rt, rememberArgs{Text: "prefer pnpm", Category: "preference"})

	ct := newRecallTool(ws)
	res, err := execTool(t, ct, recallArgs{Query: "build"})
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if !strings.Contains(res.Content, "build is `make build`") {
		t.Fatalf("recall should find build entry: %q", res.Content)
	}
	if strings.Contains(res.Content, "prefer pnpm") {
		t.Fatalf("recall should not include unrelated entry: %q", res.Content)
	}
}

func TestRecall_ByCategory(t *testing.T) {
	ws := t.TempDir()
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	rt := newRememberTool(ws)
	execTool(t, rt, rememberArgs{Text: "build is make", Category: "project"})
	execTool(t, rt, rememberArgs{Text: "prefer pnpm", Category: "preference"})

	ct := newRecallTool(ws)
	res, err := execTool(t, ct, recallArgs{Query: "", Category: "preference"})
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if !strings.Contains(res.Content, "prefer pnpm") {
		t.Fatalf("recall should find preference entry: %q", res.Content)
	}
	if strings.Contains(res.Content, "build is make") {
		t.Fatalf("recall should not include project entry: %q", res.Content)
	}
}

func TestRecall_NoMatch(t *testing.T) {
	ws := t.TempDir()
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	ct := newRecallTool(ws)
	res, err := execTool(t, ct, recallArgs{Query: "nonexistent"})
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if !strings.Contains(res.Content, "no matching") {
		t.Fatalf("expected no-match message: %q", res.Content)
	}
}

func TestForget_RemovesExactAutoMemoryAndReturnsAuditMetadata(t *testing.T) {
	ws := t.TempDir()
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if _, err := execTool(t, newRememberTool(ws), rememberArgs{Text: "build is make", Category: "project"}); err != nil {
		t.Fatalf("seed memory: %v", err)
	}
	res, err := execTool(t, newForgetTool(ws), forgetArgs{Text: "build is make", Category: "project"})
	if err != nil {
		t.Fatalf("forget: %v", err)
	}
	if !strings.Contains(res.Content, "forgot (auto, project)") || res.Metadata["memory_action"] != "deleted" {
		t.Fatalf("forget result = %#v", res)
	}
	if got := memory.Read(ws, 0); strings.Contains(got, "build is make") {
		t.Fatalf("forgotten entry still present:\n%s", got)
	}
}

func TestRegister_AddsTools(t *testing.T) {
	reg := tool.New()
	if err := Register(reg, t.TempDir()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, ok := reg.Get("remember"); !ok {
		t.Fatalf("remember not registered")
	}
	if _, ok := reg.Get("forget"); !ok {
		t.Fatalf("forget not registered")
	}
	if _, ok := reg.Get("recall"); !ok {
		t.Fatalf("recall not registered")
	}
}
