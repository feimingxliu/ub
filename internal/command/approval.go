package command

import (
	"context"
	"fmt"
	"strings"

	"github.com/feimingxliu/ub/internal/approval"
	"github.com/feimingxliu/ub/internal/config"
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

func newApprovalAgentFromConfig(ctx context.Context, cfg *config.Config, fallbackProvider, fallbackModel string, caches ...*providerCache) (approval.Agent, error) {
	setup, err := newApprovalAgentSetup(ctx, cfg, fallbackProvider, fallbackModel, caches...)
	if err != nil {
		return nil, err
	}
	return setup.Agent, nil
}

func newApprovalAgentSetup(ctx context.Context, cfg *config.Config, fallbackProvider, fallbackModel string, caches ...*providerCache) (approvalAgentSetup, error) {
	return newApprovalAgentSetupWithOptions(ctx, cfg, fallbackProvider, fallbackModel, approvalSetupOptions{VerifySmallModel: true, AllowRemoteModelSelection: true}, caches...)
}

func newApprovalAgentSetupForStartup(ctx context.Context, cfg *config.Config, fallbackProvider, fallbackModel string, caches ...*providerCache) (approvalAgentSetup, error) {
	return newApprovalAgentSetupWithOptions(ctx, cfg, fallbackProvider, fallbackModel, approvalSetupOptions{}, caches...)
}

type approvalSetupOptions struct {
	VerifySmallModel          bool
	AllowRemoteModelSelection bool
}

func newApprovalAgentSetupWithOptions(ctx context.Context, cfg *config.Config, fallbackProvider, fallbackModel string, opts approvalSetupOptions, caches ...*providerCache) (approvalAgentSetup, error) {
	if cfg == nil {
		return approvalAgentSetup{}, nil
	}
	providerName := strings.TrimSpace(cfg.ApprovalAgent.Provider)
	model := strings.TrimSpace(cfg.ApprovalAgent.Model)
	explicitProvider := providerName != ""
	explicitModel := model != ""

	if providerName == "" {
		providerName = strings.TrimSpace(fallbackProvider)
	}
	if providerName == "" {
		providerName = strings.TrimSpace(cfg.DefaultProvider)
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
	if model == "" {
		smallModel := strings.TrimSpace(cfg.SmallModel)
		if smallModel != "" && (!opts.VerifySmallModel || smallModelAvailable(ctx, providerName, providerCfg, smallModel)) {
			model = smallModel
		}
	}
	if !explicitModel && model == "" && providerName == strings.TrimSpace(fallbackProvider) {
		model = strings.TrimSpace(fallbackModel)
	}
	var err error
	if opts.AllowRemoteModelSelection {
		model, err = selectProviderModel(ctx, providerName, providerCfg, model)
	} else {
		model, err = selectConfiguredProviderModel(providerName, providerCfg, model)
	}
	if err != nil {
		if explicitProvider || strings.TrimSpace(cfg.ExecutionMode) == config.ModeAuto {
			return approvalAgentSetup{}, fmt.Errorf("select approval model: %w", err)
		}
		return approvalAgentSetup{}, nil
	}
	if model == "" {
		return approvalAgentSetup{}, nil
	}
	role := resolveModelRole(modelRoleApproval, providerName, providerCfg, model, cfg.ApprovalAgent.Reasoning)
	p, err := cachedProvider(firstProviderCache(caches), providerName, providerCfg)
	if err != nil {
		return approvalAgentSetup{}, fmt.Errorf("create approval provider %q: %w", providerName, err)
	}
	agent, err := approval.NewProviderAgentWithReasoning(p, role.Model, role.cloneReasoning())
	if err != nil {
		return approvalAgentSetup{}, err
	}
	return approvalAgentSetup{
		Agent:          agent,
		ProviderName:   role.ProviderName,
		ProviderConfig: role.ProviderConfig,
		Model:          role.Model,
		Models:         configuredProviderModels(role.ProviderConfig, role.Model),
		Reasoning:      role.cloneReasoning(),
	}, nil
}
