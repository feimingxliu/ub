package cli

import (
	"github.com/feimingxliu/ub/internal/pkg/core/config"
	"github.com/feimingxliu/ub/internal/pkg/core/reasoning"
	"github.com/feimingxliu/ub/internal/pkg/llm/modelinfo"
)

func chatReasoningConfig(cfg *config.Config, providerName string, providerCfg config.ProviderConfig, model string) *reasoning.Config {
	if cfg == nil {
		return nil
	}
	return modelinfo.RequestConfig(cfg.Reasoning, modelinfo.Resolve(providerName, providerCfg, model))
}

func chatMaxContextTokens(providerName string, providerCfg config.ProviderConfig, model string) int {
	return modelinfo.Resolve(providerName, providerCfg, model).MaxContextTokens
}

func cloneReasoningConfig(cfg *reasoning.Config) *reasoning.Config {
	if cfg == nil {
		return nil
	}
	cp := *cfg
	return &cp
}
