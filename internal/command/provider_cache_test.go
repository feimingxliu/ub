package command

import (
	"testing"

	"github.com/feimingxliu/ub/internal/config"
	"github.com/feimingxliu/ub/internal/provider"
	"github.com/feimingxliu/ub/internal/provider/fake"
)

func TestProviderCacheReusesProviderForSameConfig(t *testing.T) {
	cache := newProviderCache()
	var calls int
	cache.factory = func(name string, cfg config.ProviderConfig) (provider.Provider, error) {
		calls++
		return fake.New(nil), nil
	}
	cfg := config.ProviderConfig{Type: "openai", APIKey: "key", BaseURL: "https://example.invalid/v1"}

	first, err := cache.Get("main", cfg)
	if err != nil {
		t.Fatalf("Get first: %v", err)
	}
	second, err := cache.Get("main", cfg)
	if err != nil {
		t.Fatalf("Get second: %v", err)
	}
	if first != second {
		t.Fatalf("cache returned different providers for same config")
	}
	if calls != 1 {
		t.Fatalf("factory calls = %d, want 1", calls)
	}

	changed := cfg
	changed.BaseURL = "https://other.invalid/v1"
	third, err := cache.Get("main", changed)
	if err != nil {
		t.Fatalf("Get changed: %v", err)
	}
	if third == first {
		t.Fatalf("cache reused provider after config changed")
	}
	if calls != 2 {
		t.Fatalf("factory calls after config change = %d, want 2", calls)
	}
}

func TestProviderCacheDoesNotCacheFakeProvider(t *testing.T) {
	cache := newProviderCache()
	cache.factory = func(name string, cfg config.ProviderConfig) (provider.Provider, error) {
		t.Fatalf("fake provider should bypass cache factory")
		return nil, nil
	}
	cfg := config.ProviderConfig{Type: "fake"}

	first, err := cache.Get("fake", cfg)
	if err != nil {
		t.Fatalf("Get first: %v", err)
	}
	second, err := cache.Get("fake", cfg)
	if err != nil {
		t.Fatalf("Get second: %v", err)
	}
	if first == second {
		t.Fatalf("fake provider should not be cached")
	}
}
