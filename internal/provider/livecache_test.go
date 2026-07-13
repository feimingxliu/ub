package provider_test

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/feimingxliu/ub/internal/config"
	"github.com/feimingxliu/ub/internal/message"
	"github.com/feimingxliu/ub/internal/provider"
	_ "github.com/feimingxliu/ub/internal/provider/anthropic" // register anthropic
	_ "github.com/feimingxliu/ub/internal/provider/compat"    // register openai-compat
	_ "github.com/feimingxliu/ub/internal/provider/openai"    // register openai
)

// TestLiveCacheHitProbe sends two consecutive requests with an identical stable
// prefix to a real provider endpoint and verifies that the second response
// reports cache_read_tokens > 0.
//
// This test is skipped unless UB_LIVE_CACHE_TEST=1 is set. It reads the provider
// configuration from ~/.config/ub/config.yaml via config.Load().
//
// Usage:
//
//	UB_LIVE_CACHE_TEST=1 go test ./internal/provider/ -run TestLiveCacheHitProbe -v -timeout 120s
//
// The test selects the first reachable openai, openai-compat, or anthropic
// provider from the config and uses its first configured model. It sends a
// system prompt followed by a user message, drains the stream, then sends a
// second request with the same prefix plus one additional user turn. The second
// response should show cache_read_tokens > 0 because the stable prefix is
// identical.
func TestLiveCacheHitProbe(t *testing.T) {
	if os.Getenv("UB_LIVE_CACHE_TEST") == "" {
		t.Skip("set UB_LIVE_CACHE_TEST=1 to run the live cache probe")
	}

	cfg, _, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}

	// Find the first cache-capable provider with a model.
	providerName, providerCfg, modelName := findLiveProvider(t, cfg)
	if providerName == "" {
		t.Skip("no cache-capable provider with models found in config")
	}

	p, err := provider.New(providerName, providerCfg)
	if err != nil {
		t.Fatalf("provider.New(%q): %v", providerName, err)
	}

	// Build a list of models to test. Prefer the default model, but also
	// include DeepSeek models which are known to report cache hit tokens.
	candidates := []string{modelName}
	if strings.Contains(modelName, "glm") || strings.Contains(modelName, "qwen") {
		for m := range providerCfg.Models {
			if strings.Contains(m, "deepseek") {
				candidates = append(candidates, m)
				break
			}
		}
	}

	// A stable system prompt long enough to exceed the minimum cacheable
	// prefix (1024 tokens for most providers).
	stableSystem := strings.Repeat("You are a helpful coding assistant. Follow project conventions. ", 50) +
		"\nGuidelines:\n" + strings.Repeat("- Keep functions short and focused.\n", 20)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	for _, model := range candidates {
		caps := provider.CapsForModel(p, model)
		if !caps.SupportsPromptCache {
			t.Logf("model %q does not support prompt cache, skipping", model)
			continue
		}
		t.Logf("testing model=%q max_context=%d", model, caps.MaxContextTokens)

		// --- First request: establish the cache entry. ---
		req1 := provider.Request{
			Model: model,
			Messages: []message.Message{
				message.Text(message.RoleSystem, stableSystem),
				message.Text(message.RoleUser, "What is 1+1? Answer with just the number."),
			},
		}
		usage1 := drainStream(t, ctx, p, req1)
		t.Logf("  request 1: input=%d output=%d cache_read=%d cache_write=%d",
			usage1.InputTokens, usage1.OutputTokens, usage1.CacheReadTokens, usage1.CacheWriteTokens)

		// --- Second request: same prefix + one more turn. Should hit cache. ---
		req2 := provider.Request{
			Model: model,
			Messages: []message.Message{
				message.Text(message.RoleSystem, stableSystem),
				message.Text(message.RoleUser, "What is 1+1? Answer with just the number."),
				message.Text(message.RoleAssistant, "2"),
				message.Text(message.RoleUser, "What is 2+2? Answer with just the number."),
			},
		}
		usage2 := drainStream(t, ctx, p, req2)
		t.Logf("  request 2: input=%d output=%d cache_read=%d cache_write=%d",
			usage2.InputTokens, usage2.OutputTokens, usage2.CacheReadTokens, usage2.CacheWriteTokens)

		if usage2.CacheReadTokens > 0 {
			t.Logf("✓ model=%q: cache hit confirmed: %d tokens read from cache on second request", model, usage2.CacheReadTokens)
			return // success
		}
		t.Logf("  model=%q: cache_read=0 on second request (may not support reporting cache hits)", model)
	}
	t.Error("none of the tested models returned cache_read_tokens > 0 on the second request; " +
		"the provider may not support cache reporting, or the prefix was too short")
}

// findLiveProvider returns the first openai, openai-compat, or anthropic
// provider name, config, and model from the config. It prefers the configured
// default_provider/default_model when they are available and the provider
// supports prompt cache, then falls back to iterating over all providers.
func findLiveProvider(t *testing.T, cfg *config.Config) (string, config.ProviderConfig, string) {
	t.Helper()
	// Prefer the configured default provider/model.
	if cfg.DefaultProvider != "" && cfg.DefaultModel != "" {
		if pcfg, ok := cfg.Providers[cfg.DefaultProvider]; ok {
			switch pcfg.Type {
			case "openai", "openai-compat", "anthropic":
				return cfg.DefaultProvider, pcfg, cfg.DefaultModel
			}
		}
	}
	// Fall back to the first cache-capable provider with configured models.
	for name, pcfg := range cfg.Providers {
		switch pcfg.Type {
		case "openai", "openai-compat", "anthropic":
			if len(pcfg.Models) > 0 {
				for model := range pcfg.Models {
					return name, pcfg, model
				}
			}
		}
	}
	return "", config.ProviderConfig{}, ""
}

// drainStream sends the request, drains all events, and returns the usage.
func drainStream(t *testing.T, ctx context.Context, p provider.Provider, req provider.Request) *provider.Usage {
	t.Helper()
	stream, err := p.Chat(ctx, req)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	defer stream.Close()
	var usage *provider.Usage
	for {
		event, err := stream.Next(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("stream.Next: %v", err)
		}
		if event.Type == provider.EventUsage && event.Usage != nil {
			usage = event.Usage
		}
		if event.Type == provider.EventDone {
			break
		}
	}
	if usage == nil {
		t.Fatal("no usage event received from provider")
	}
	return usage
}
