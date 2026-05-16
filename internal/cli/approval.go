package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/feimingxliu/ub/internal/approval"
	"github.com/feimingxliu/ub/internal/config"
	"github.com/feimingxliu/ub/internal/modelinfo"
	"github.com/feimingxliu/ub/internal/provider"
	"github.com/feimingxliu/ub/internal/reasoning"
)

type approvalAgentSetup struct {
	Agent          approval.Agent
	ProviderName   string
	ProviderConfig config.ProviderConfig
	Model          string
	Models         []string
	Reasoning      *reasoning.Config
}

func newApprovalAgentFromConfig(ctx context.Context, cfg *config.Config, fallbackProvider, fallbackModel string) (approval.Agent, error) {
	setup, err := newApprovalAgentSetup(ctx, cfg, fallbackProvider, fallbackModel)
	if err != nil {
		return nil, err
	}
	return setup.Agent, nil
}

func newApprovalAgentSetup(ctx context.Context, cfg *config.Config, fallbackProvider, fallbackModel string) (approvalAgentSetup, error) {
	if cfg == nil {
		return approvalAgentSetup{}, nil
	}
	providerName := strings.TrimSpace(cfg.ApprovalAgent.Provider)
	model := strings.TrimSpace(cfg.ApprovalAgent.Model)
	explicitProvider := providerName != ""

	if providerName == "" {
		providerName = strings.TrimSpace(cfg.DefaultProvider)
	}
	if providerName == "" {
		providerName = strings.TrimSpace(fallbackProvider)
	}
	if model == "" {
		model = strings.TrimSpace(cfg.SmallModel)
	}
	if model == "" && providerName == strings.TrimSpace(fallbackProvider) {
		model = strings.TrimSpace(fallbackModel)
	}
	if providerName == "" {
		return approvalAgentSetup{}, nil
	}

	providerCfg, ok := cfg.Providers[providerName]
	if !ok {
		if explicitProvider {
			return approvalAgentSetup{}, fmt.Errorf("approval_agent provider %q not configured; check `ub config show`", providerName)
		}
		return approvalAgentSetup{}, nil
	}
	var err error
	model, err = selectProviderModel(ctx, providerName, providerCfg, model)
	if err != nil {
		if explicitProvider || strings.TrimSpace(cfg.ExecutionMode) == config.ModeAuto {
			return approvalAgentSetup{}, fmt.Errorf("select approval model: %w", err)
		}
		return approvalAgentSetup{}, nil
	}
	if model == "" {
		return approvalAgentSetup{}, nil
	}
	p, err := provider.New(providerName, providerCfg)
	if err != nil {
		return approvalAgentSetup{}, fmt.Errorf("create approval provider %q: %w", providerName, err)
	}
	reasoningCfg := modelinfo.RequestConfig(cfg.ApprovalAgent.Reasoning, modelinfo.Resolve(providerName, providerCfg, model))
	agent, err := approval.NewProviderAgentWithReasoning(p, model, reasoningCfg)
	if err != nil {
		return approvalAgentSetup{}, err
	}
	return approvalAgentSetup{
		Agent:          agent,
		ProviderName:   providerName,
		ProviderConfig: providerCfg,
		Model:          model,
		Models:         providerModels(ctx, providerName, providerCfg, model),
		Reasoning:      reasoningCfg,
	}, nil
}
