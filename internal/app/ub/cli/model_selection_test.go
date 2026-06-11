package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/feimingxliu/ub/internal/pkg/core/config"
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
