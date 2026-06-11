package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/feimingxliu/ub/internal/pkg/core/config"
	"github.com/feimingxliu/ub/internal/pkg/core/execution"
	"github.com/feimingxliu/ub/internal/pkg/llm/provider"
	"github.com/feimingxliu/ub/internal/pkg/runtime/approval"
	"github.com/feimingxliu/ub/internal/pkg/tool"
)

func TestNewApprovalAgentFromConfigUsesSmallModelAndFallbackProvider(t *testing.T) {
	cfg := &config.Config{
		SmallModel: "fake/reviewer",
		Providers: map[string]config.ProviderConfig{
			"fake": {
				Type: "fake",
				Script: []config.ProviderScriptEvent{
					{Type: string(provider.EventTextDelta), Text: `{"decision":"allow","reason":"safe"}`},
					{Type: string(provider.EventDone)},
				},
			},
		},
	}
	agent, err := newApprovalAgentFromConfig(context.Background(), cfg, "fake", "fake/main")
	if err != nil {
		t.Fatalf("newApprovalAgentFromConfig: %v", err)
	}
	if agent == nil {
		t.Fatal("agent is nil, want configured approval agent")
	}
	res, err := agent.ReviewCommand(context.Background(), approval.Request{
		Tool:    "bash",
		Risk:    tool.RiskExec,
		Mode:    execution.ModeAuto,
		Command: "git status",
	})
	if err != nil {
		t.Fatalf("ReviewCommand: %v", err)
	}
	if res.Decision != approval.DecisionAllow || res.Reason != "safe" {
		t.Fatalf("result = %#v, want allow safe", res)
	}
}

func TestNewApprovalAgentSetupPrefersSelectedProviderOverDefaultProvider(t *testing.T) {
	cfg := &config.Config{
		DefaultProvider: "primary",
		SmallModel:      "primary/small",
		Providers: map[string]config.ProviderConfig{
			"primary": {
				Type: "fake",
				Models: map[string]config.ModelConfig{
					"primary/model": {},
					"primary/small": {},
				},
			},
			"manual": {
				Type: "fake",
				Models: map[string]config.ModelConfig{
					"manual/model": {},
					"manual/small": {},
				},
			},
		},
	}
	setup, err := newApprovalAgentSetup(context.Background(), cfg, "manual", "manual/model")
	if err != nil {
		t.Fatalf("newApprovalAgentSetup: %v", err)
	}
	if setup.ProviderName != "manual" || setup.Model != "manual/model" {
		t.Fatalf("approval setup provider/model = %q/%q, want manual/manual/model", setup.ProviderName, setup.Model)
	}
	if !modelInList(setup.Models, "manual/small") || modelInList(setup.Models, "primary/small") {
		t.Fatalf("approval candidates = %#v, want manual provider candidates only", setup.Models)
	}
}

func TestNewApprovalAgentFromConfigErrorsForExplicitMissingProvider(t *testing.T) {
	cfg := &config.Config{
		ApprovalAgent: config.ApprovalAgentConfig{
			Provider: "missing",
			Model:    "fake/reviewer",
		},
		Providers: map[string]config.ProviderConfig{
			"fake": {Type: "fake"},
		},
	}
	_, err := newApprovalAgentFromConfig(context.Background(), cfg, "fake", "fake/main")
	if err == nil || !strings.Contains(err.Error(), `approval_agent provider "missing" not configured`) {
		t.Fatalf("error = %v, want explicit missing provider error", err)
	}
}

func TestNewApprovalAgentFromConfigSelectsProviderModelWhenUnset(t *testing.T) {
	cfg := &config.Config{
		ExecutionMode: config.ModeAuto,
		ApprovalAgent: config.ApprovalAgentConfig{
			Provider: "reviewer",
		},
		Providers: map[string]config.ProviderConfig{
			"reviewer": {
				Type: "fake",
				Models: map[string]config.ModelConfig{
					"z-review": {},
					"a-review": {},
				},
				Script: []config.ProviderScriptEvent{
					{Type: string(provider.EventTextDelta), Text: `{"decision":"allow","reason":"selected fallback"}`},
					{Type: string(provider.EventDone)},
				},
			},
		},
	}
	setup, err := newApprovalAgentSetup(context.Background(), cfg, "", "")
	if err != nil {
		t.Fatalf("newApprovalAgentSetup: %v", err)
	}
	if setup.Agent == nil {
		t.Fatal("agent is nil, want provider-backed approval agent")
	}
	if setup.Model != "a-review" {
		t.Fatalf("approval model = %q, want a-review", setup.Model)
	}
	res, err := setup.Agent.ReviewCommand(context.Background(), approval.Request{
		Tool:    "bash",
		Risk:    tool.RiskExec,
		Mode:    execution.ModeAuto,
		Command: "git status",
	})
	if err != nil {
		t.Fatalf("ReviewCommand: %v", err)
	}
	if res.Decision != approval.DecisionAllow {
		t.Fatalf("decision = %q, want allow", res.Decision)
	}
}
