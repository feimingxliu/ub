package provider_test

import (
	"strings"
	"testing"

	"github.com/feimingxliu/ub/internal/config"
	"github.com/feimingxliu/ub/internal/provider"
	_ "github.com/feimingxliu/ub/internal/provider/fake"
)

func TestNewCreatesFakeProvider(t *testing.T) {
	p, err := provider.New("test", config.ProviderConfig{Type: "fake"})
	if err != nil {
		t.Fatalf("New(fake): %v", err)
	}
	if p.Name() != "test" {
		t.Fatalf("Name() = %q", p.Name())
	}
	if !p.Caps().SupportsStreaming {
		t.Fatalf("fake provider should support streaming caps: %#v", p.Caps())
	}
}

func TestNewUnknownType(t *testing.T) {
	_, err := provider.New("bad", config.ProviderConfig{Type: "unknown"})
	if err == nil {
		t.Fatal("expected error for unknown provider type")
	}
	if !strings.Contains(err.Error(), "bad") || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("error should include name and type, got %q", err)
	}
}

func TestNewMissingType(t *testing.T) {
	_, err := provider.New("bad", config.ProviderConfig{})
	if err == nil {
		t.Fatal("expected missing type error")
	}
	if !strings.Contains(err.Error(), "missing type") {
		t.Fatalf("unexpected error: %v", err)
	}
}
