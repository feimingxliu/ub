package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/feimingxliu/ub/internal/pkg/core/config"
	"github.com/feimingxliu/ub/internal/pkg/llm/provider"
)

type providerFactoryFunc func(string, config.ProviderConfig) (provider.Provider, error)

type providerCache struct {
	mu      sync.Mutex
	factory providerFactoryFunc
	entries map[string]provider.Provider
}

func newProviderCache() *providerCache {
	return &providerCache{
		factory: provider.New,
		entries: map[string]provider.Provider{},
	}
}

func (c *providerCache) Get(name string, cfg config.ProviderConfig) (provider.Provider, error) {
	if c == nil || !cacheableProviderConfig(cfg) {
		return provider.New(name, cfg)
	}
	key, err := providerCacheKey(name, cfg)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = map[string]provider.Provider{}
	}
	if existing := c.entries[key]; existing != nil {
		return existing, nil
	}
	factory := c.factory
	if factory == nil {
		factory = provider.New
	}
	p, err := factory(name, cfg)
	if err != nil {
		return nil, err
	}
	c.entries[key] = p
	return p, nil
}

func cacheableProviderConfig(cfg config.ProviderConfig) bool {
	// The fake provider is intentionally stateful: each Chat consumes scripted
	// rounds. Caching it would couple unrelated tests and scripted runs.
	return strings.TrimSpace(cfg.Type) != "fake"
}

func providerCacheKey(name string, cfg config.ProviderConfig) (string, error) {
	raw, err := json.Marshal(struct {
		Name   string                `json:"name"`
		Config config.ProviderConfig `json:"config"`
	}{
		Name:   strings.TrimSpace(name),
		Config: cfg,
	})
	if err != nil {
		return "", fmt.Errorf("provider cache key %q: %w", name, err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func cachedProvider(cache *providerCache, name string, cfg config.ProviderConfig) (provider.Provider, error) {
	if cache == nil {
		return provider.New(name, cfg)
	}
	return cache.Get(name, cfg)
}
