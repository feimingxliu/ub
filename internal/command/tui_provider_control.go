package command

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/feimingxliu/ub/internal/approval"
	"github.com/feimingxliu/ub/internal/config"
	"github.com/feimingxliu/ub/internal/modelinfo"
	"github.com/feimingxliu/ub/internal/tui"
)

func (r *tuiAgentRunner) SetModel(model string) error {
	model = strings.TrimSpace(model)
	if model == "" {
		return fmt.Errorf("model cannot be empty")
	}
	candidates := r.models
	if r != nil && r.cmd != nil {
		candidates = mergeModelCandidates(model, candidates, r.providerModels(r.cmd.Context(), r.providerName, r.providerCfg, ""))
	}
	if !modelInList(candidates, model) {
		return fmt.Errorf("model %q is not available for the current provider", model)
	}
	r.models = candidates
	r.model = model
	r.models = appendModelCandidate(r.models, model)
	r.summaryModel = model
	if r.smallUsesCurrent {
		r.autoMemoryModel = model
	}
	r.smallModels = mergeModelCandidates(r.autoMemoryModel, []string{r.model}, r.smallModels, r.models)
	r.refreshReasoning()
	return r.persistSessionProviderModel(r.cmd.Context())
}

func (r *tuiAgentRunner) persistSessionProviderModel(ctx context.Context) error {
	if r == nil || r.state == nil || r.closedStore {
		return nil
	}
	r.state.session.Provider = r.providerName
	r.state.session.Model = r.model
	r.state.session.UpdatedAt = time.Now().UTC()
	return r.state.store.UpdateSession(ctx, r.state.session)
}

func (r *tuiAgentRunner) providerSelection() tui.ProviderSelection {
	return tui.ProviderSelection{
		Provider:  r.providerName,
		Providers: r.Providers(),
		Model:     r.model,
		Models:    r.Models(),
		Effort:    r.Effort(),
		Efforts:   r.Efforts(),
	}
}

func (r *tuiAgentRunner) RefreshModels(ctx context.Context) ([]string, error) {
	if r == nil {
		return nil, fmt.Errorf("model refresh is unavailable")
	}
	return r.providerModels(ctx, r.providerName, r.providerCfg, r.model), nil
}

func (r *tuiAgentRunner) SetProvider(providerName, model string) (tui.ProviderSelection, error) {
	if r == nil || r.cmd == nil {
		return tui.ProviderSelection{}, fmt.Errorf("provider switching is unavailable")
	}
	return r.setProviderModel(r.cmd.Context(), providerName, model)
}

func (r *tuiAgentRunner) setProviderModel(ctx context.Context, providerName, model string) (tui.ProviderSelection, error) {
	providerName = strings.TrimSpace(providerName)
	if providerName == "" {
		return tui.ProviderSelection{}, fmt.Errorf("provider cannot be empty")
	}
	if r == nil || r.cfg == nil {
		return tui.ProviderSelection{}, fmt.Errorf("provider switching is unavailable")
	}
	providerCfg, ok := r.cfg.Providers[providerName]
	if !ok {
		return tui.ProviderSelection{}, fmt.Errorf("provider %q not configured; check `ub config show`", providerName)
	}
	selectedModel, err := r.modelForProviderSwitch(providerName, providerCfg, model)
	if err != nil {
		return tui.ProviderSelection{}, err
	}
	p, err := cachedProvider(r.providerCache, providerName, providerCfg)
	if err != nil {
		return tui.ProviderSelection{}, fmt.Errorf("create provider %q: %w", providerName, err)
	}
	models := r.providerModels(r.cmd.Context(), providerName, providerCfg, selectedModel)
	mainRole := resolveModelRole(modelRoleMain, providerName, providerCfg, selectedModel, r.reasoningPref)
	summarySetup, err := newSummarySetup(r.cmd.Context(), r.cfg, providerName, providerCfg, selectedModel, r.providerCache)
	if err != nil {
		return tui.ProviderSelection{}, err
	}
	autoMemorySetup, err := newAutoMemorySetup(r.cmd.Context(), r.cfg, providerName, providerCfg, selectedModel, r.providerCache)
	if err != nil {
		return tui.ProviderSelection{}, err
	}
	approvalSetup, err := newApprovalAgentSetup(r.cmd.Context(), r.cfg, providerName, selectedModel, r.providerCache)
	if err != nil {
		return tui.ProviderSelection{}, err
	}
	r.provider = p
	r.providerName = providerName
	r.providerCfg = providerCfg
	r.model = selectedModel
	r.modelInfo = mainRole.Info
	r.models = models
	r.efforts = mainRole.Efforts
	r.summaryProvider = summarySetup.Provider
	r.summaryModel = summarySetup.Model
	r.autoMemoryProvider = autoMemorySetup.Provider
	r.autoMemoryModel = autoMemorySetup.Model
	r.smallModels = mergeModelCandidates(autoMemorySetup.Model, []string{selectedModel}, models)
	r.smallUsesCurrent = autoMemorySetup.UsesCurrentModel
	r.approvalProviderName = approvalSetup.ProviderName
	r.approvalProviderCfg = approvalSetup.ProviderConfig
	r.approvalModel = approvalSetup.Model
	r.approvalModels = approvalSetup.Models
	if r.approvalProviderName != "" {
		r.approvalModels = r.providerModels(r.cmd.Context(), r.approvalProviderName, r.approvalProviderCfg, r.approvalModel)
	}
	r.approvalReasoning = r.cfg.ApprovalAgent.Reasoning
	if r.permission != nil {
		r.permission.SetApprovalAgent(approvalSetup.Agent)
	}
	r.refreshReasoning()
	if err := r.persistSessionProviderModel(ctx); err != nil {
		return tui.ProviderSelection{}, err
	}
	return r.providerSelection(), nil
}

func (r *tuiAgentRunner) modelForProviderSwitch(providerName string, providerCfg config.ProviderConfig, model string) (string, error) {
	model = strings.TrimSpace(model)
	if model != "" {
		return model, nil
	}
	if r != nil && r.cfg != nil && strings.TrimSpace(r.cfg.DefaultProvider) == providerName && strings.TrimSpace(r.cfg.DefaultModel) != "" {
		return strings.TrimSpace(r.cfg.DefaultModel), nil
	}
	candidates := r.providerModels(r.cmd.Context(), providerName, providerCfg, "")
	currentModel := strings.TrimSpace(r.model)
	if currentModel != "" && (len(candidates) == 0 || modelInList(candidates, currentModel)) {
		return currentModel, nil
	}
	if configured := firstConfiguredProviderModel(providerCfg); configured != "" {
		return configured, nil
	}
	if len(candidates) > 0 {
		return candidates[0], nil
	}
	return r.selectProviderModel(r.cmd.Context(), providerName, providerCfg, "")
}

func (r *tuiAgentRunner) SetEffort(effort string) error {
	info := r.currentModelInfo()
	parsed, err := modelinfo.ValidateEffort(info, effort)
	if err != nil {
		return err
	}
	r.reasoningPref.Effort = parsed
	r.refreshReasoning()
	return nil
}

func (r *tuiAgentRunner) SetApprovalModel(model string) error {
	model = strings.TrimSpace(model)
	if model == "" {
		return fmt.Errorf("approval model cannot be empty")
	}
	if r.approvalProviderName == "" {
		return fmt.Errorf("approval provider is not configured")
	}
	ctx := context.Background()
	if r.cmd != nil {
		ctx = r.cmd.Context()
	}
	r.approvalModels = mergeModelCandidates(r.approvalModel, r.approvalModels, r.providerModels(ctx, r.approvalProviderName, r.approvalProviderCfg, ""))
	if !modelInList(r.approvalModels, model) {
		r.approvalModels = r.providerModels(ctx, r.approvalProviderName, r.approvalProviderCfg, "")
	}
	if !modelInList(r.approvalModels, model) {
		return fmt.Errorf("approval model %q is not available for the current approval provider", model)
	}
	agent, err := r.newApprovalAgent(model)
	if err != nil {
		return err
	}
	r.permission.SetApprovalAgent(agent)
	r.approvalModel = model
	r.approvalModels = appendModelCandidate(r.approvalModels, model)
	return nil
}

func (r *tuiAgentRunner) RefreshApprovalModels(ctx context.Context) ([]string, error) {
	if r == nil || r.approvalProviderName == "" {
		return nil, fmt.Errorf("approval provider is not configured")
	}
	r.approvalModels = r.providerModels(ctx, r.approvalProviderName, r.approvalProviderCfg, r.approvalModel)
	return append([]string(nil), r.approvalModels...), nil
}

func (r *tuiAgentRunner) SetSmallModel(model string) error {
	model = strings.TrimSpace(model)
	if model == "" {
		return fmt.Errorf("small model cannot be empty")
	}
	if r == nil || r.providerName == "" {
		return fmt.Errorf("small model switching is unavailable")
	}
	ctx := context.Background()
	if r.cmd != nil {
		ctx = r.cmd.Context()
	}
	r.smallModels = mergeModelCandidates(r.autoMemoryModel, []string{r.model}, r.smallModels, r.providerModels(ctx, r.providerName, r.providerCfg, ""))
	if !modelInList(r.smallModels, model) {
		return fmt.Errorf("small model %q is not available for the current provider", model)
	}
	r.autoMemoryModel = model
	r.smallUsesCurrent = false
	r.smallModels = appendModelCandidate(r.smallModels, model)
	return nil
}

func (r *tuiAgentRunner) RefreshSmallModels(ctx context.Context) ([]string, error) {
	if r == nil || r.providerName == "" {
		return nil, fmt.Errorf("small model switching is unavailable")
	}
	r.smallModels = mergeModelCandidates(r.autoMemoryModel, []string{r.model}, r.providerModels(ctx, r.providerName, r.providerCfg, ""))
	return append([]string(nil), r.smallModels...), nil
}

func (r *tuiAgentRunner) newApprovalAgent(model string) (approval.Agent, error) {
	p, err := cachedProvider(r.providerCache, r.approvalProviderName, r.approvalProviderCfg)
	if err != nil {
		return nil, fmt.Errorf("create approval provider %q: %w", r.approvalProviderName, err)
	}
	role := resolveModelRole(modelRoleApproval, r.approvalProviderName, r.approvalProviderCfg, model, r.approvalReasoning)
	return approval.NewProviderAgentWithReasoning(p, role.Model, role.cloneReasoning())
}
