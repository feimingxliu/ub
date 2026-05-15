package shell_test

import (
	"strings"
	"testing"

	"github.com/feimingxliu/ub/internal/tool"
	"github.com/feimingxliu/ub/internal/tool/shell"
)

func TestRegister_AddsBash(t *testing.T) {
	reg := tool.New()
	if err := shell.Register(reg, t.TempDir()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	got, ok := reg.Get("bash")
	if !ok || got == nil {
		t.Fatalf("bash not registered")
	}
	if got.Risk() != tool.RiskExec {
		t.Fatalf("bash Risk = %q want exec", got.Risk())
	}
}

func TestRegister_Conflict(t *testing.T) {
	reg := tool.New()
	if err := shell.Register(reg, t.TempDir()); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	err := shell.Register(reg, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("expected duplicate error, got %v", err)
	}
}

func TestRegister_NilOrEmpty(t *testing.T) {
	if err := shell.Register(nil, "/tmp"); err == nil {
		t.Fatalf("expected nil-registry error")
	}
	if err := shell.Register(tool.New(), ""); err == nil {
		t.Fatalf("expected empty-root error")
	}
}
