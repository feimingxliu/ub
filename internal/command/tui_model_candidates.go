package command

import (
	"context"
	"sort"
	"strings"

	"github.com/feimingxliu/ub/internal/config"
)

func configuredProviderModels(providerCfg config.ProviderConfig, current string) []string {
	models := make([]string, 0, len(providerCfg.Models))
	for model := range providerCfg.Models {
		models = append(models, model)
	}
	sort.Strings(models)
	return appendModelCandidate(models, current)
}

func (r *tuiAgentRunner) providerModels(ctx context.Context, providerName string, providerCfg config.ProviderConfig, current string) []string {
	result := r.checkProvider(ctx, providerName, providerCfg)
	return mergeModelCandidates(current, configuredProviderModels(providerCfg, ""), result.Models)
}

func (r *tuiAgentRunner) selectProviderModel(ctx context.Context, providerName string, providerCfg config.ProviderConfig, model string) (string, error) {
	model = strings.TrimSpace(model)
	if model != "" {
		return model, nil
	}
	if configured := firstConfiguredProviderModel(providerCfg); configured != "" {
		return configured, nil
	}
	check := r.checkProvider(ctx, providerName, providerCfg)
	if len(check.Models) > 0 {
		return check.Models[0], nil
	}
	if !providerRequiresModel(providerCfg.Type) {
		return "", nil
	}
	return "", missingModelError(providerName, check)
}

func (r *tuiAgentRunner) checkProvider(ctx context.Context, providerName string, providerCfg config.ProviderConfig) providerCheck {
	if r == nil {
		return checkProvider(ctx, providerName, providerCfg)
	}
	key := providerCheckKey(providerName, providerCfg)
	if cached, ok := r.cachedProviderCheck(key); ok {
		return cached
	}

	value, _, _ := r.providerCheckGroup.Do(key, func() (any, error) {
		if cached, ok := r.cachedProviderCheck(key); ok {
			return cached, nil
		}
		result := checkProvider(ctx, providerName, providerCfg)
		r.storeProviderCheck(key, result)
		return result, nil
	})
	if result, ok := value.(providerCheck); ok {
		return result
	}

	result := checkProvider(ctx, providerName, providerCfg)
	r.storeProviderCheck(key, result)
	return result
}

func (r *tuiAgentRunner) cachedProviderCheck(key string) (providerCheck, bool) {
	if r == nil {
		return providerCheck{}, false
	}
	r.providerCheckMu.Lock()
	defer r.providerCheckMu.Unlock()
	if r.providerChecks == nil {
		return providerCheck{}, false
	}
	cached, ok := r.providerChecks[key]
	return cached, ok
}

func (r *tuiAgentRunner) storeProviderCheck(key string, result providerCheck) {
	if r == nil {
		return
	}
	r.providerCheckMu.Lock()
	defer r.providerCheckMu.Unlock()
	if r.providerChecks == nil {
		r.providerChecks = map[string]providerCheck{}
	}
	r.providerChecks[key] = result
}

func providerCheckKey(providerName string, providerCfg config.ProviderConfig) string {
	return strings.Join([]string{
		strings.TrimSpace(providerName),
		strings.TrimSpace(providerCfg.Type),
		strings.TrimSpace(providerCfg.BaseURL),
	}, "\x00")
}

func appendModelCandidate(models []string, model string) []string {
	model = strings.TrimSpace(model)
	seen := map[string]struct{}{}
	var out []string
	add := func(candidate string) {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			return
		}
		if _, ok := seen[candidate]; ok {
			return
		}
		seen[candidate] = struct{}{}
		out = append(out, candidate)
	}
	add(model)
	for _, candidate := range models {
		add(candidate)
	}
	return out
}

func mergeModelCandidates(current string, groups ...[]string) []string {
	current = strings.TrimSpace(current)
	seen := map[string]struct{}{}
	var out []string
	add := func(candidate string) {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			return
		}
		if _, ok := seen[candidate]; ok {
			return
		}
		seen[candidate] = struct{}{}
		out = append(out, candidate)
	}
	add(current)
	for _, group := range groups {
		for _, candidate := range group {
			add(candidate)
		}
	}
	return out
}

func modelInList(models []string, model string) bool {
	for _, candidate := range models {
		if candidate == model {
			return true
		}
	}
	return false
}
