package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func freezeTime(t *testing.T, instant time.Time) {
	t.Helper()
	orig := nowFunc
	nowFunc = func() time.Time { return instant }
	t.Cleanup(func() { nowFunc = orig })
}

func TestPath_WorkspaceRequiresRoot(t *testing.T) {
	if _, err := Path("", ScopeWorkspace); err == nil {
		t.Fatalf("expected error for empty workspace root")
	}
}

func TestPath_WorkspaceUnderRoot(t *testing.T) {
	ws := t.TempDir()
	p, err := Path(ws, ScopeWorkspace)
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if p != filepath.Join(ws, ".ub", "memory.md") {
		t.Fatalf("path = %s", p)
	}
}

func TestPath_GlobalUsesXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/cfg")
	p, err := Path("", ScopeGlobal)
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if p != filepath.Join("/tmp/cfg", "ub", "memory.md") {
		t.Fatalf("path = %s", p)
	}
}

func TestAppend_CreatesFileAndAppendsOrdered(t *testing.T) {
	ws := t.TempDir()
	freezeTime(t, time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC))
	_, h1, err := Append(ws, ScopeWorkspace, "first")
	if err != nil {
		t.Fatalf("append 1: %v", err)
	}
	if h1 != "## 2026-05-27T10:00:00Z" {
		t.Fatalf("heading = %q", h1)
	}
	freezeTime(t, time.Date(2026, 5, 27, 10, 5, 0, 0, time.UTC))
	if _, _, err := Append(ws, ScopeWorkspace, "second"); err != nil {
		t.Fatalf("append 2: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(ws, ".ub/memory.md"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(body), "first") || !strings.Contains(string(body), "second") {
		t.Fatalf("file missing entries:\n%s", body)
	}
	if strings.Index(string(body), "first") > strings.Index(string(body), "second") {
		t.Fatalf("ordering reversed:\n%s", body)
	}
}

func TestAppend_EmptyTextRejected(t *testing.T) {
	if _, _, err := Append(t.TempDir(), ScopeWorkspace, "   "); err == nil {
		t.Fatalf("expected empty-text error")
	}
}

func TestRead_MissingFilesReturnEmpty(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	got := Read(t.TempDir(), 0)
	if got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestRead_ConcatsGlobalThenWorkspace(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	ws := t.TempDir()
	freezeTime(t, time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC))
	if _, _, err := Append(ws, ScopeGlobal, "global-fact"); err != nil {
		t.Fatalf("append global: %v", err)
	}
	if _, _, err := Append(ws, ScopeWorkspace, "ws-fact"); err != nil {
		t.Fatalf("append ws: %v", err)
	}
	got := Read(ws, 0)
	if !strings.Contains(got, "<!-- global memory -->") || !strings.Contains(got, "<!-- workspace memory -->") {
		t.Fatalf("missing scope markers:\n%s", got)
	}
	if strings.Index(got, "global-fact") > strings.Index(got, "ws-fact") {
		t.Fatalf("global must come before workspace:\n%s", got)
	}
}

func TestRead_TruncatesKeepsTail(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	ws := t.TempDir()
	if _, _, err := Append(ws, ScopeWorkspace, strings.Repeat("a", 5000)); err != nil {
		t.Fatalf("append: %v", err)
	}
	if _, _, err := Append(ws, ScopeWorkspace, "TAIL-MARKER"); err != nil {
		t.Fatalf("append: %v", err)
	}
	got := Read(ws, 200)
	if len(got) > 250 {
		t.Fatalf("not truncated: len=%d\n%s", len(got), got)
	}
	if !strings.Contains(got, "memory truncated") {
		t.Fatalf("missing truncation marker:\n%s", got)
	}
	if !strings.Contains(got, "TAIL-MARKER") {
		t.Fatalf("expected tail preserved:\n%s", got)
	}
}

func TestValidScope(t *testing.T) {
	for _, s := range []string{"workspace", "global"} {
		if !ValidScope(s) {
			t.Errorf("ValidScope(%q) = false", s)
		}
	}
	for _, s := range []string{"", "session", "nope"} {
		if ValidScope(s) {
			t.Errorf("ValidScope(%q) = true", s)
		}
	}
}
