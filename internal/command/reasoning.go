package command

import (
	"github.com/feimingxliu/ub/internal/config"
	"github.com/feimingxliu/ub/internal/reasoning"
)

func chatReasoningConfig(cfg *config.Config, providerName string, providerCfg config.ProviderConfig, model string) *reasoning.Config {
	if cfg == nil {
		return nil
	}
	return resolveModelRole(modelRoleMain, providerName, providerCfg, model, cfg.Reasoning).cloneReasoning()
}

func chatMaxContextTokens(providerName string, providerCfg config.ProviderConfig, model string) int {
	return resolveModelRole(modelRoleMain, providerName, providerCfg, model, reasoning.Config{}).MaxContextTokens
}

func cloneReasoningConfig(cfg *reasoning.Config) *reasoning.Config {
	if cfg == nil {
		return nil
	}
	cp := *cfg
	return &cp
}
