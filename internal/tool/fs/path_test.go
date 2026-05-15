package fs

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestResolve_RejectsRelativeEscape(t *testing.T) {
	root := t.TempDir()
	if _, err := resolve(root, "../etc/passwd"); err == nil {
		t.Fatalf("expected error for ../escape")
	} else if !strings.Contains(err.Error(), "outside workspace") {
		t.Fatalf("expected workspace error, got: %v", err)
	}
}

func TestResolve_RejectsAbsoluteEscape(t *testing.T) {
	root := t.TempDir()
	if _, err := resolve(root, "/etc/passwd"); err == nil {
		t.Fatalf("expected error for absolute escape")
	}
}

func TestResolve_AcceptsInsideAbsolute(t *testing.T) {
	root := t.TempDir()
	abs, err := resolve(root, filepath.Join(root, "a.txt"))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !strings.HasPrefix(abs, root) {
		t.Fatalf("resolved path %q not under root %q", abs, root)
	}
}

func TestResolve_AcceptsRelative(t *testing.T) {
	root := t.TempDir()
	abs, err := resolve(root, "a/b.txt")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !strings.HasPrefix(abs, root) {
		t.Fatalf("resolved %q not under root %q", abs, root)
	}
}

func TestResolve_EmptyRoot(t *testing.T) {
	if _, err := resolve("", "a"); err == nil {
		t.Fatalf("empty root should error")
	}
}
