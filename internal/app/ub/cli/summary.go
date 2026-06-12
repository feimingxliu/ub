package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/feimingxliu/ub/internal/pkg/core/config"
	"github.com/feimingxliu/ub/internal/pkg/core/reasoning"
	"github.com/feimingxliu/ub/internal/pkg/llm/provider"
)

type summarySetup struct {
	Provider         provider.Provider
	Model            string
	UsesCurrentModel bool
}

func newSummarySetup(ctx context.Context, cfg *config.Config, providerName string, providerCfg config.ProviderConfig, fallbackModel string, caches ...*providerCache) (summarySetup, error) {
	return newProviderModelSetup(ctx, modelRoleSummary, providerName, providerCfg, strings.TrimSpace(fallbackModel), true, "summary", firstProviderCache(caches))
}

func newAutoMemorySetup(ctx context.Context, cfg *config.Config, providerName string, providerCfg config.ProviderConfig, fallbackModel string, caches ...*providerCache) (summarySetup, error) {
	return newAutoMemorySetupWithOptions(ctx, cfg, providerName, providerCfg, fallbackModel, true, caches...)
}

func newAutoMemorySetupForStartup(ctx context.Context, cfg *config.Config, providerName string, providerCfg config.ProviderConfig, fallbackModel string, caches ...*providerCache) (summarySetup, error) {
	return newAutoMemorySetupWithOptions(ctx, cfg, providerName, providerCfg, fallbackModel, false, caches...)
}

func newAutoMemorySetupWithOptions(ctx context.Context, cfg *config.Config, providerName string, providerCfg config.ProviderConfig, fallbackModel string, verifySmallModel bool, caches ...*providerCache) (summarySetup, error) {
	if cfg == nil {
		return summarySetup{}, nil
	}
	model, usesCurrent, err := selectSmallModel(ctx, cfg, providerName, providerCfg, fallbackModel, verifySmallModel)
	if err != nil {
		return summarySetup{}, err
	}
	return newProviderModelSetup(ctx, modelRoleAutoMemory, providerName, providerCfg, model, usesCurrent, "auto memory", firstProviderCache(caches))
}

func newProviderModelSetup(ctx context.Context, role modelRole, providerName string, providerCfg config.ProviderConfig, model string, usesCurrent bool, purpose string, cache *providerCache) (summarySetup, error) {
	model, err := selectProviderModel(ctx, providerName, providerCfg, model)
	if err != nil {
		return summarySetup{}, fmt.Errorf("select %s model: %w", purpose, err)
	}
	if strings.TrimSpace(model) == "" {
		return summarySetup{}, nil
	}
	resolved := resolveModelRole(role, providerName, providerCfg, model, reasoning.Config{})
	p, err := cachedProvider(cache, providerName, providerCfg)
	if err != nil {
		return summarySetup{}, fmt.Errorf("create %s provider %q: %w", purpose, providerName, err)
	}
	return summarySetup{
		Provider:         p,
		Model:            resolved.Model,
		UsesCurrentModel: usesCurrent,
	}, nil
}

func firstProviderCache(caches []*providerCache) *providerCache {
	for _, cache := range caches {
		if cache != nil {
			return cache
		}
	}
	return nil
}

func selectSmallModel(ctx context.Context, cfg *config.Config, providerName string, providerCfg config.ProviderConfig, fallbackModel string, verifySmallModel bool) (string, bool, error) {
	smallModel := strings.TrimSpace(cfg.SmallModel)
	if smallModel == "" {
		return strings.TrimSpace(fallbackModel), true, nil
	}
	if !verifySmallModel || smallModelAvailable(ctx, providerName, providerCfg, smallModel) {
		return smallModel, false, nil
	}
	return strings.TrimSpace(fallbackModel), true, nil
}

func smallModelAvailable(ctx context.Context, providerName string, providerCfg config.ProviderConfig, model string) bool {
	model = strings.TrimSpace(model)
	if model == "" {
		return false
	}
	candidates := mergeModelCandidates("", configuredProviderModels(providerCfg, ""), checkProvider(ctx, providerName, providerCfg).Models)
	if len(candidates) == 0 {
		return true
	}
	return modelInList(candidates, model)
}
