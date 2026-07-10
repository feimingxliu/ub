package job

import (
	"strings"
	"testing"

	"github.com/feimingxliu/ub/internal/tool"
)

func TestRegister_AddsJobTools(t *testing.T) {
	reg := tool.New()
	if err := Register(reg, t.TempDir()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	for _, name := range []string{"job_run", "job_output", "job_kill"} {
		got, ok := reg.Get(name)
		if !ok || got == nil {
			t.Fatalf("%s not registered", name)
		}
	}
	run, _ := reg.Get("job_run")
	if run.Risk() != tool.RiskExec {
		t.Fatalf("job_run risk = %q, want exec", run.Risk())
	}
	output, _ := reg.Get("job_output")
	if output.Risk() != tool.RiskSafe {
		t.Fatalf("job_output risk = %q, want safe", output.Risk())
	}
	kill, _ := reg.Get("job_kill")
	if kill.Risk() != tool.RiskExec {
		t.Fatalf("job_kill risk = %q, want exec", kill.Risk())
	}
}

func TestRegister_Conflict(t *testing.T) {
	reg := tool.New()
	if err := Register(reg, t.TempDir()); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	err := Register(reg, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("expected duplicate registration error, got %v", err)
	}
}

func TestRegister_NilOrEmpty(t *testing.T) {
	if err := Register(nil, t.TempDir()); err == nil {
		t.Fatalf("expected nil registry error")
	}
	if err := Register(tool.New(), ""); err == nil {
		t.Fatalf("expected empty root error")
	}
}
