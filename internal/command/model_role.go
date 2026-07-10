package command

import (
	"context"
	"fmt"
	"strings"

	"github.com/feimingxliu/ub/internal/config"
	"github.com/feimingxliu/ub/internal/modelinfo"
	"github.com/feimingxliu/ub/internal/reasoning"
)

type modelRole string

const (
	modelRoleMain       modelRole = "main"
	modelRoleApproval   modelRole = "approval"
	modelRoleSummary    modelRole = "summary"
	modelRoleAutoMemory modelRole = "auto memory"
	modelRoleSmall      modelRole = "small"
)

type resolvedModelRole struct {
	Role             modelRole
	ProviderName     string
	ProviderConfig   config.ProviderConfig
	Model            string
	Info             modelinfo.Info
	Reasoning        *reasoning.Config
	Efforts          []string
	MaxContextTokens int
}

func resolveMainModelRole(ctx context.Context, cfg *config.Config, providerFlag, modelFlag string) (resolvedModelRole, error) {
	providerName, model, err := selectChatProvider(cfg, providerFlag, modelFlag)
	if err != nil {
		return resolvedModelRole{}, err
	}
	if cfg == nil {
		return resolvedModelRole{}, fmt.Errorf("provider %q not configured; check `ub config show`", providerName)
	}
	providerCfg, ok := cfg.Providers[providerName]
	if !ok {
		return resolvedModelRole{}, fmt.Errorf("provider %q not configured; check `ub config show`", providerName)
	}
	model, err = selectProviderModel(ctx, providerName, providerCfg, model)
	if err != nil {
		return resolvedModelRole{}, err
	}
	return resolveModelRole(modelRoleMain, providerName, providerCfg, model, cfg.Reasoning), nil
}

func resolveModelRole(role modelRole, providerName string, providerCfg config.ProviderConfig, model string, preferred reasoning.Config) resolvedModelRole {
	providerName = strings.TrimSpace(providerName)
	model = strings.TrimSpace(model)
	info := modelinfo.Resolve(providerName, providerCfg, model)
	return resolvedModelRoleFromInfo(role, providerName, providerCfg, model, info, preferred)
}

func resolvedModelRoleFromInfo(role modelRole, providerName string, providerCfg config.ProviderConfig, model string, info modelinfo.Info, preferred reasoning.Config) resolvedModelRole {
	providerName = strings.TrimSpace(providerName)
	model = strings.TrimSpace(model)
	reasoningCfg := modelinfo.RequestConfig(preferred, info)
	return resolvedModelRole{
		Role:             role,
		ProviderName:     providerName,
		ProviderConfig:   providerCfg,
		Model:            model,
		Info:             cloneModelInfo(info),
		Reasoning:        cloneReasoningConfig(reasoningCfg),
		Efforts:          modelinfo.EffortOptions(info),
		MaxContextTokens: info.MaxContextTokens,
	}
}

func (r resolvedModelRole) cloneReasoning() *reasoning.Config {
	return cloneReasoningConfig(r.Reasoning)
}

func cloneModelInfo(info modelinfo.Info) modelinfo.Info {
	info.SupportedEfforts = append([]reasoning.Effort(nil), info.SupportedEfforts...)
	return info
}
