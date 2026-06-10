package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/feimingxliu/ub/internal/config"
	"github.com/feimingxliu/ub/internal/provider"
)

type summarySetup struct {
	Provider         provider.Provider
	Model            string
	UsesCurrentModel bool
}

func newSummarySetup(ctx context.Context, cfg *config.Config, providerName string, providerCfg config.ProviderConfig, fallbackModel string) (summarySetup, error) {
	return newProviderModelSetup(ctx, providerName, providerCfg, strings.TrimSpace(fallbackModel), true, "summary")
}

func newAutoMemorySetup(ctx context.Context, cfg *config.Config, providerName string, providerCfg config.ProviderConfig, fallbackModel string) (summarySetup, error) {
	if cfg == nil {
		return summarySetup{}, nil
	}
	model, usesCurrent, err := selectSmallModel(ctx, cfg, providerName, providerCfg, fallbackModel)
	if err != nil {
		return summarySetup{}, err
	}
	return newProviderModelSetup(ctx, providerName, providerCfg, model, usesCurrent, "auto memory")
}

func newProviderModelSetup(ctx context.Context, providerName string, providerCfg config.ProviderConfig, model string, usesCurrent bool, purpose string) (summarySetup, error) {
	model, err := selectProviderModel(ctx, providerName, providerCfg, model)
	if err != nil {
		return summarySetup{}, fmt.Errorf("select %s model: %w", purpose, err)
	}
	if strings.TrimSpace(model) == "" {
		return summarySetup{}, nil
	}
	p, err := provider.New(providerName, providerCfg)
	if err != nil {
		return summarySetup{}, fmt.Errorf("create %s provider %q: %w", purpose, providerName, err)
	}
	return summarySetup{
		Provider:         p,
		Model:            model,
		UsesCurrentModel: usesCurrent,
	}, nil
}

func selectSmallModel(ctx context.Context, cfg *config.Config, providerName string, providerCfg config.ProviderConfig, fallbackModel string) (string, bool, error) {
	smallModel := strings.TrimSpace(cfg.SmallModel)
	if smallModel == "" {
		return strings.TrimSpace(fallbackModel), true, nil
	}
	if smallModelAvailable(ctx, providerName, providerCfg, smallModel) {
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
