package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":"z-review"},{"id":"a-review"}]}`))
		case "/chat/completions":
			if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			writeOpenAIChatSSE(t, w, `{"id":"chatcmpl_1","object":"chat.completion.chunk","created":0,"model":"a-review","choices":[{"index":0,"delta":{"role":"assistant","content":"{\"decision\":\"allow\",\"reason\":\"selected fallback\"}"},"finish_reason":null}]}`)
			writeOpenAIChatSSE(t, w, `[DONE]`)
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		ExecutionMode: config.ModeAuto,
		ApprovalAgent: config.ApprovalAgentConfig{
			Provider: "reviewer",
		},
		Providers: map[string]config.ProviderConfig{
			"reviewer": {
				Type:    "openai-compat",
				BaseURL: server.URL,
			},
		},
	}
	agent, err := newApprovalAgentFromConfig(context.Background(), cfg, "", "")
	if err != nil {
		t.Fatalf("newApprovalAgentFromConfig: %v", err)
	}
	if agent == nil {
		t.Fatal("agent is nil, want provider-backed approval agent")
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
	if res.Decision != approval.DecisionAllow {
		t.Fatalf("decision = %q, want allow", res.Decision)
	}
	if requestBody["model"] != "a-review" {
		t.Fatalf("approval model = %#v, want a-review", requestBody["model"])
	}
}
