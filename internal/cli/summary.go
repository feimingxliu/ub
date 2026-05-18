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
	if cfg == nil {
		return summarySetup{}, nil
	}
	model := strings.TrimSpace(cfg.SmallModel)
	usesCurrent := model == ""
	if usesCurrent {
		model = strings.TrimSpace(fallbackModel)
	}
	model, err := selectProviderModel(ctx, providerName, providerCfg, model)
	if err != nil {
		return summarySetup{}, fmt.Errorf("select summary model: %w", err)
	}
	if strings.TrimSpace(model) == "" {
		return summarySetup{}, nil
	}
	p, err := provider.New(providerName, providerCfg)
	if err != nil {
		return summarySetup{}, fmt.Errorf("create summary provider %q: %w", providerName, err)
	}
	return summarySetup{
		Provider:         p,
		Model:            model,
		UsesCurrentModel: usesCurrent,
	}, nil
}
