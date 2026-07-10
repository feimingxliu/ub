package paths

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStateRoot_XDG(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/tmp/xdg-state")
	got, err := StateRoot()
	if err != nil {
		t.Fatalf("StateRoot: %v", err)
	}
	want := filepath.Join("/tmp/xdg-state", "ub")
	if got != want {
		t.Fatalf("StateRoot() = %q, want %q", got, want)
	}
}

func TestStateRoot_Default(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	home, _ := os.UserHomeDir()
	got, err := StateRoot()
	if err != nil {
		t.Fatalf("StateRoot: %v", err)
	}
	want := filepath.Join(home, ".local", "state", "ub")
	if got != want {
		t.Fatalf("StateRoot() = %q, want %q", got, want)
	}
}

func TestConfigHome_XDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg-cfg")
	got, err := ConfigHome()
	if err != nil {
		t.Fatalf("ConfigHome: %v", err)
	}
	if got != "/tmp/xdg-cfg" {
		t.Fatalf("ConfigHome() = %q, want /tmp/xdg-cfg", got)
	}
}

func TestDataHome_Default(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")
	home, _ := os.UserHomeDir()
	got, err := DataHome()
	if err != nil {
		t.Fatalf("DataHome: %v", err)
	}
	want := filepath.Join(home, ".local", "share")
	if got != want {
		t.Fatalf("DataHome() = %q, want %q", got, want)
	}
}

func TestProjectKey_Empty(t *testing.T) {
	if _, err := ProjectKey(""); err == nil {
		t.Fatalf("expected error for empty workspace")
	}
}

func TestProjectKey_Deterministic(t *testing.T) {
	ws := t.TempDir()
	k1, err := ProjectKey(ws)
	if err != nil {
		t.Fatalf("ProjectKey: %v", err)
	}
	k2, err := ProjectKey(ws)
	if err != nil {
		t.Fatalf("ProjectKey: %v", err)
	}
	if k1 != k2 {
		t.Fatalf("ProjectKey not deterministic: %q != %q", k1, k2)
	}
	if len(k1) != 16 {
		t.Fatalf("ProjectKey length = %d, want 16", len(k1))
	}
}

func TestProjectKey_DifferentWorkspaces(t *testing.T) {
	ws1 := t.TempDir()
	ws2 := t.TempDir()
	k1, _ := ProjectKey(ws1)
	k2, _ := ProjectKey(ws2)
	if k1 == k2 {
		t.Fatalf("different workspaces should have different keys")
	}
}

func TestProjectKey_GitRoot(t *testing.T) {
	ws := t.TempDir()
	gitDir := filepath.Join(ws, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatalf("write HEAD: %v", err)
	}
	sub := filepath.Join(ws, "sub", "dir")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}
	rootKey, err := ProjectKey(ws)
	if err != nil {
		t.Fatalf("ProjectKey root: %v", err)
	}
	subKey, err := ProjectKey(sub)
	if err != nil {
		t.Fatalf("ProjectKey sub: %v", err)
	}
	if rootKey != subKey {
		t.Fatalf("sub-directory should resolve to git root key: root=%s sub=%s", rootKey, subKey)
	}
}
