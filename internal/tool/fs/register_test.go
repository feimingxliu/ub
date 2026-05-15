package fs_test

import (
	"strings"
	"testing"

	"github.com/feimingxliu/ub/internal/tool"
	"github.com/feimingxliu/ub/internal/tool/fs"
)

func TestRegister_FiveTools(t *testing.T) {
	reg := tool.New()
	if err := fs.Register(reg, t.TempDir()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	want := []string{"edit", "glob", "ls", "read", "write"}
	all := reg.All()
	got := make([]string, len(all))
	for i, tl := range all {
		got[i] = tl.Name()
	}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

func TestRegister_ConflictReturnsError(t *testing.T) {
	reg := tool.New()
	if err := fs.Register(reg, t.TempDir()); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	err := fs.Register(reg, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("expected duplicate-tool error, got: %v", err)
	}
}

func TestRegister_NilRegistry(t *testing.T) {
	if err := fs.Register(nil, "/tmp"); err == nil {
		t.Fatalf("expected nil-registry error")
	}
}

func TestRegister_EmptyRoot(t *testing.T) {
	if err := fs.Register(tool.New(), ""); err == nil {
		t.Fatalf("expected empty-root error")
	}
}
