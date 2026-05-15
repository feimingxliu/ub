package approval

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/feimingxliu/ub/internal/execution"
	"github.com/feimingxliu/ub/internal/tool"
)

type mockAgent struct{}

func (mockAgent) ReviewCommand(context.Context, Request) (Result, error) {
	return Result{Decision: DecisionAllow, Reason: "ok"}, nil
}

func TestAgentInterfaceAndJSON(t *testing.T) {
	var _ Agent = mockAgent{}
	raw, err := json.Marshal(Request{
		Tool:    "bash",
		Risk:    tool.RiskExec,
		Mode:    execution.ModeAuto,
		Command: "git status",
	})
	if err != nil {
		t.Fatalf("marshal Request: %v", err)
	}
	var req Request
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatalf("unmarshal Request: %v", err)
	}
	if req.Tool != "bash" || req.Command != "git status" {
		t.Fatalf("request roundtrip = %#v", req)
	}
}
