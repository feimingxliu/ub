package command

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/feimingxliu/ub/internal/config"
	"github.com/feimingxliu/ub/internal/reasoning"
)

func TestSelectProviderModelUsesAnthropicModels(t *testing.T) {
	var apiKey, version string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("path = %q, want /v1/models", r.URL.Path)
		}
		apiKey = r.Header.Get("x-api-key")
		version = r.Header.Get("anthropic-version")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"claude-z"},{"id":"claude-a"}]}`))
	}))
	defer server.Close()

	model, err := selectProviderModel(context.Background(), "anthropic", config.ProviderConfig{
		Type:    "anthropic",
		APIKey:  "sk-test",
		BaseURL: server.URL,
	}, "")
	if err != nil {
		t.Fatalf("selectProviderModel: %v", err)
	}
	if model != "claude-a" {
		t.Fatalf("model = %q, want claude-a", model)
	}
	if apiKey != "sk-test" {
		t.Fatalf("x-api-key = %q, want sk-test", apiKey)
	}
	if version == "" {
		t.Fatal("anthropic-version header is empty")
	}
}

func TestSelectProviderModelRequiresModelWhenListUnavailable(t *testing.T) {
	_, err := selectProviderModel(context.Background(), "anthropic", config.ProviderConfig{
		Type: "anthropic",
	}, "")
	if err == nil || !strings.Contains(err.Error(), "model required") {
		t.Fatalf("error = %v, want missing model error", err)
	}
}

func TestResolveMainModelRoleBuildsCapabilityReasoningAndLimit(t *testing.T) {
	cfg := &config.Config{
		DefaultProvider: "fake",
		DefaultModel:    "main/model",
		Reasoning:       reasoning.Config{Effort: reasoning.EffortHigh},
		Providers: map[string]config.ProviderConfig{
			"fake": {
				Type: "fake",
				Models: map[string]config.ModelConfig{
					"main/model": {
						SupportsReasoning: true,
						SupportedEfforts:  []reasoning.Effort{reasoning.EffortLow, reasoning.EffortHigh},
						DefaultEffort:     reasoning.EffortLow,
						MaxContextTokens:  12345,
					},
				},
			},
		},
	}

	role, err := resolveMainModelRole(context.Background(), cfg, "", "")
	if err != nil {
		t.Fatalf("resolveMainModelRole: %v", err)
	}
	if role.Role != modelRoleMain || role.ProviderName != "fake" || role.Model != "main/model" {
		t.Fatalf("role identity = %#v", role)
	}
	if role.MaxContextTokens != 12345 || role.Info.MaxContextTokens != 12345 {
		t.Fatalf("max context = role %d info %d, want 12345", role.MaxContextTokens, role.Info.MaxContextTokens)
	}
	if role.Reasoning == nil || role.Reasoning.Effort != reasoning.EffortHigh {
		t.Fatalf("reasoning = %#v, want high", role.Reasoning)
	}
	if got := strings.Join(role.Efforts, ","); got != "none,low,high" {
		t.Fatalf("efforts = %q", got)
	}
}
