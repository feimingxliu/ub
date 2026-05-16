package cli

import (
	"github.com/feimingxliu/ub/internal/config"
	"github.com/feimingxliu/ub/internal/modelinfo"
	"github.com/feimingxliu/ub/internal/reasoning"
)

func chatReasoningConfig(cfg *config.Config, providerName string, providerCfg config.ProviderConfig, model string) *reasoning.Config {
	if cfg == nil {
		return nil
	}
	return modelinfo.RequestConfig(cfg.Reasoning, modelinfo.Resolve(providerName, providerCfg, model))
}

func cloneReasoningConfig(cfg *reasoning.Config) *reasoning.Config {
	if cfg == nil {
		return nil
	}
	cp := *cfg
	return &cp
}
