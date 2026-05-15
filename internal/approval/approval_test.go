package approval

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/feimingxliu/ub/internal/execution"
	"github.com/feimingxliu/ub/internal/provider/fake"
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

func TestProviderAgentReviewCommandAllowsJSONResponse(t *testing.T) {
	provider := fake.New(fake.Script{
		fake.TextDelta(`{"decision":"allow","reason":"read-only inspection"}`),
		fake.Done(),
	})
	agent, err := NewProviderAgent(provider, "fake/reviewer")
	if err != nil {
		t.Fatalf("NewProviderAgent: %v", err)
	}
	res, err := agent.ReviewCommand(context.Background(), Request{
		Tool:    "bash",
		Risk:    tool.RiskExec,
		Mode:    execution.ModeAuto,
		Command: "git status",
	})
	if err != nil {
		t.Fatalf("ReviewCommand: %v", err)
	}
	if res.Decision != DecisionAllow || res.Reason != "read-only inspection" {
		t.Fatalf("result = %#v, want allow with reason", res)
	}
}

func TestProviderAgentReviewCommandParsesFencedResponse(t *testing.T) {
	provider := fake.New(fake.Script{
		fake.TextDelta("```json\n{\"decision\":\"unsure\",\"reason\":\"needs context\"}\n```"),
		fake.Done(),
	})
	agent, err := NewProviderAgent(provider, "fake/reviewer")
	if err != nil {
		t.Fatalf("NewProviderAgent: %v", err)
	}
	res, err := agent.ReviewCommand(context.Background(), Request{
		Tool:    "bash",
		Risk:    tool.RiskExec,
		Mode:    execution.ModeAuto,
		Command: "make deploy",
	})
	if err != nil {
		t.Fatalf("ReviewCommand: %v", err)
	}
	if res.Decision != DecisionUnsure || res.Reason != "needs context" {
		t.Fatalf("result = %#v, want unsure with reason", res)
	}
}

func TestProviderAgentRejectsInvalidDecision(t *testing.T) {
	provider := fake.New(fake.Script{
		fake.TextDelta(`{"decision":"maybe","reason":"bad"}`),
		fake.Done(),
	})
	agent, err := NewProviderAgent(provider, "fake/reviewer")
	if err != nil {
		t.Fatalf("NewProviderAgent: %v", err)
	}
	_, err = agent.ReviewCommand(context.Background(), Request{
		Tool:    "bash",
		Risk:    tool.RiskExec,
		Mode:    execution.ModeAuto,
		Command: "git status",
	})
	if err == nil || !strings.Contains(err.Error(), "invalid decision") {
		t.Fatalf("error = %v, want invalid decision", err)
	}
}

func TestProviderAgentRejectsToolCall(t *testing.T) {
	provider := fake.New(fake.Script{
		fake.ToolCall("bash", map[string]string{"command": "git status"}),
	})
	agent, err := NewProviderAgent(provider, "fake/reviewer")
	if err != nil {
		t.Fatalf("NewProviderAgent: %v", err)
	}
	_, err = agent.ReviewCommand(context.Background(), Request{
		Tool:    "bash",
		Risk:    tool.RiskExec,
		Mode:    execution.ModeAuto,
		Command: "git status",
	})
	if err == nil || !strings.Contains(err.Error(), "attempted tool call") {
		t.Fatalf("error = %v, want tool call rejection", err)
	}
}
