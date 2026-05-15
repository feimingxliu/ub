package execution

import (
	"strings"
	"testing"

	"github.com/feimingxliu/ub/internal/tool"
)

func TestParseMode(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want Mode
	}{
		{name: "empty", raw: "", want: ModeDefault},
		{name: "default", raw: "default", want: ModeDefault},
		{name: "plan", raw: " plan ", want: ModePlan},
		{name: "agent approve", raw: "agent-approve", want: ModeAgentApprove},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseMode(tt.raw)
			if err != nil {
				t.Fatalf("ParseMode: %v", err)
			}
			if got != tt.want {
				t.Fatalf("mode = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseModeUnknown(t *testing.T) {
	_, err := ParseMode("danger")
	if err == nil || !strings.Contains(err.Error(), "danger") {
		t.Fatalf("unknown mode error = %v", err)
	}
}

func TestGatePlanRejectsWrite(t *testing.T) {
	err := Gate(ModePlan, tool.RiskWrite)
	if err == nil || !strings.Contains(err.Error(), "plan mode") {
		t.Fatalf("Gate error = %v", err)
	}
}

func TestGateAllowsExecForApproval(t *testing.T) {
	for _, mode := range []Mode{ModeDefault, ModePlan, ModeAgentApprove} {
		if err := Gate(mode, tool.RiskExec); err != nil {
			t.Fatalf("Gate(%s, exec): %v", mode, err)
		}
	}
}
