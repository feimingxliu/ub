package command

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/feimingxliu/ub/internal/config"
	"github.com/feimingxliu/ub/internal/context"
	"github.com/feimingxliu/ub/internal/provider/fake"
)

func TestNewContextWindowResolverPersistsByEndpointAndModel(t *testing.T) {
	stateHome := filepath.Join(t.TempDir(), "state")
	t.Setenv("XDG_STATE_HOME", stateHome)
	p := fake.New(fake.Script{fake.Done()})
	cfg := config.ProviderConfig{Type: "openai-compat", BaseURL: "https://a.example/v1"}
	resolver := newContextWindowResolver("compat", cfg, "org/model", 0, p)
	if resolver == nil {
		t.Fatal("resolver = nil")
	}
	if err := resolver.ObserveOverflow(errors.New("maximum context length is 8192 tokens"), 9000); err != nil {
		t.Fatalf("ObserveOverflow: %v", err)
	}

	reloaded := newContextWindowResolver("compat", cfg, "org/model", 0, p)
	if got := reloaded.Resolve(); got.MaxTokens != 8192 || got.Source != contextwindow.SourceLearnedOverflow {
		t.Fatalf("reloaded Resolve() = %#v", got)
	}
	other := newContextWindowResolver("compat", config.ProviderConfig{Type: "openai-compat", BaseURL: "https://b.example/v1"}, "org/model", 0, p)
	if got := other.Resolve(); got.MaxTokens == 8192 || got.Source == contextwindow.SourceLearnedOverflow {
		t.Fatalf("other endpoint Resolve() = %#v", got)
	}
}

func TestNewContextWindowResolverKeepsExplicitConfigAuthoritative(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	p := fake.New(fake.Script{fake.Done()})
	resolver := newContextWindowResolver(
		"compat",
		config.ProviderConfig{Type: "openai-compat", BaseURL: "https://example.test/v1"},
		"org/model",
		200000,
		p,
	)
	if err := resolver.ObserveOverflow(errors.New("maximum context length is 8192 tokens"), 9000); err != nil {
		t.Fatalf("ObserveOverflow: %v", err)
	}
	want := contextwindow.Resolution{MaxTokens: 200000, Source: contextwindow.SourceConfig, Confidence: contextwindow.ConfidenceExact}
	if got := resolver.Resolve(); got != want {
		t.Fatalf("Resolve() = %#v, want %#v", got, want)
	}
}
