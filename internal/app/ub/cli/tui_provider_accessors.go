package cli

import (
	"strings"

	"github.com/feimingxliu/ub/internal/pkg/core/reasoning"
	"github.com/feimingxliu/ub/internal/pkg/llm/modelinfo"
)

func (r *tuiAgentRunner) Models() []string {
	if r == nil {
		return nil
	}
	return append([]string(nil), r.models...)
}

func (r *tuiAgentRunner) Provider() string {
	if r == nil {
		return ""
	}
	return r.providerName
}

func (r *tuiAgentRunner) Providers() []string {
	if r == nil || r.cfg == nil {
		return nil
	}
	var out []string
	for _, name := range sortedProviderNames(r.cfg.Providers) {
		if strings.TrimSpace(r.cfg.Providers[name].Type) == "" {
			continue
		}
		out = append(out, name)
	}
	return out
}

func (r *tuiAgentRunner) Effort() string {
	if r == nil || r.reasoning == nil || r.reasoning.Effort == "" {
		return string(reasoning.EffortNone)
	}
	return string(r.reasoning.Effort)
}

func (r *tuiAgentRunner) Efforts() []string {
	if r == nil {
		return nil
	}
	return append([]string(nil), r.efforts...)
}

func (r *tuiAgentRunner) refreshReasoning() {
	role := r.currentModelRole()
	r.efforts = role.Efforts
	r.reasoning = role.cloneReasoning()
}

func (r *tuiAgentRunner) currentModelRole() resolvedModelRole {
	if r == nil {
		return resolvedModelRole{}
	}
	providerName := strings.TrimSpace(r.providerName)
	model := strings.TrimSpace(r.model)
	if r.modelInfo.Provider == providerName && r.modelInfo.ID == model {
		info := cloneModelInfo(r.modelInfo)
		return resolvedModelRoleFromInfo(modelRoleMain, providerName, r.providerCfg, model, info, r.reasoningPref)
	}
	role := resolveModelRole(modelRoleMain, providerName, r.providerCfg, model, r.reasoningPref)
	r.modelInfo = role.Info
	return role
}

func (r *tuiAgentRunner) currentModelInfo() modelinfo.Info {
	if r == nil {
		return modelinfo.Info{}
	}
	return r.currentModelRole().Info
}

func (r *tuiAgentRunner) ApprovalModel() string {
	if r == nil {
		return ""
	}
	return r.approvalModel
}

func (r *tuiAgentRunner) ApprovalModels() []string {
	if r == nil {
		return nil
	}
	return append([]string(nil), r.approvalModels...)
}

func (r *tuiAgentRunner) SmallModel() string {
	if r == nil {
		return ""
	}
	return r.autoMemoryModel
}

func (r *tuiAgentRunner) SmallModels() []string {
	if r == nil {
		return nil
	}
	return append([]string(nil), r.smallModels...)
}
