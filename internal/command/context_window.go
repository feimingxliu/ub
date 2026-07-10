package command

import (
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/feimingxliu/ub/internal/config"
	"github.com/feimingxliu/ub/internal/context"
	"github.com/feimingxliu/ub/internal/provider"
	"github.com/feimingxliu/ub/internal/workspace/paths"
)

const contextWindowCacheDir = "context-windows"

func newContextWindowResolver(providerName string, providerCfg config.ProviderConfig, model string, configuredTokens int, p provider.Provider) *contextwindow.Resolver {
	opts := contextwindow.Options{
		Key:              contextwindow.NewKey(providerName, providerCfg.BaseURL, model),
		ConfiguredTokens: configuredTokens,
	}
	if p != nil {
		opts.ProviderTokens = provider.CapsForModel(p, model).MaxContextTokens
	}
	// Fake scripts are deterministic test/development fixtures, not evidence
	// about a real endpoint. Keep their observations in memory only.
	if strings.EqualFold(strings.TrimSpace(providerCfg.Type), "fake") {
		resolver, _ := contextwindow.New(opts)
		return resolver
	}
	stateRoot, err := paths.StateRoot()
	if err != nil {
		slog.Warn("locate context window cache", "provider", providerName, "model", model, "err", err)
		resolver, _ := contextwindow.New(opts)
		return resolver
	}
	opts.Store = contextwindow.NewFileStore(filepath.Join(stateRoot, contextWindowCacheDir))
	resolver, err := contextwindow.New(opts)
	if err == nil {
		return resolver
	}
	slog.Warn("load context window cache", "provider", providerName, "model", model, "err", err)
	opts.Store = nil
	resolver, _ = contextwindow.New(opts)
	return resolver
}
