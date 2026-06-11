package search_test

import (
	"strings"
	"testing"

	"github.com/feimingxliu/ub/internal/pkg/tool"
	"github.com/feimingxliu/ub/internal/pkg/tool/search"
)

func TestRegister_AddsGrep(t *testing.T) {
	reg := tool.New()
	if err := search.Register(reg, t.TempDir()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	got, ok := reg.Get("grep")
	if !ok || got == nil {
		t.Fatalf("grep tool not registered")
	}
	if got.Risk() != tool.RiskSafe {
		t.Fatalf("grep Risk = %q want safe", got.Risk())
	}
}

func TestRegister_Conflict(t *testing.T) {
	reg := tool.New()
	if err := search.Register(reg, t.TempDir()); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	if err := search.Register(reg, t.TempDir()); err == nil || !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("expected duplicate error, got %v", err)
	}
}

func TestRegister_NilOrEmpty(t *testing.T) {
	if err := search.Register(nil, "/tmp"); err == nil {
		t.Fatalf("expected nil-registry error")
	}
	if err := search.Register(tool.New(), ""); err == nil {
		t.Fatalf("expected empty-root error")
	}
}
