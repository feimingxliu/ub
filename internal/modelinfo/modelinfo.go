// Package modelinfo resolves model capability metadata.
package modelinfo

import (
	"fmt"
	"strings"

	"github.com/feimingxliu/ub/internal/config"
	"github.com/feimingxliu/ub/internal/reasoning"
)

// Info is the capability view ub needs at runtime for a selected model.
type Info struct {
	ID                string
	Provider          string
	SupportsReasoning bool
	SupportedEfforts  []reasoning.Effort
	DefaultEffort     reasoning.Effort
	MaxContextTokens  int
}

// Resolve returns model capabilities from user config, built-ins, and a
// conservative fallback for unknown models.
func Resolve(providerName string, providerCfg config.ProviderConfig, model string) Info {
	info := Info{
		ID:            strings.TrimSpace(model),
		Provider:      strings.TrimSpace(providerName),
		DefaultEffort: reasoning.EffortNone,
	}
	if cfg, ok := userModelConfig(providerCfg, model); ok {
		return mergeConfigInfo(info, cfg)
	}
	if builtin, ok := builtinInfo(providerCfg.Type, model); ok {
		builtin.ID = info.ID
		builtin.Provider = info.Provider
		return builtin
	}
	return info
}

// RequestConfig returns the provider request reasoning config for a selected
// model and user-configured preference.
func RequestConfig(preferred reasoning.Config, info Info) *reasoning.Config {
	effort := EffectiveEffort(preferred, info)
	if effort == reasoning.EffortNone {
		return nil
	}
	return &reasoning.Config{Effort: effort, Summary: preferred.Summary}
}

// EffectiveEffort returns the configured effort if valid, otherwise the model
// default, otherwise none.
func EffectiveEffort(preferred reasoning.Config, info Info) reasoning.Effort {
	if !info.SupportsReasoning {
		return reasoning.EffortNone
	}
	requested, err := reasoning.NormalizeEffort(string(preferred.Effort))
	if err != nil {
		requested = reasoning.EffortNone
	}
	if requested != reasoning.EffortNone && reasoning.Contains(info.SupportedEfforts, requested) {
		return requested
	}
	if info.DefaultEffort != "" && info.DefaultEffort != reasoning.EffortNone && reasoning.Contains(info.SupportedEfforts, info.DefaultEffort) {
		return info.DefaultEffort
	}
	if len(info.SupportedEfforts) > 0 {
		return info.SupportedEfforts[0]
	}
	return reasoning.EffortNone
}

// EffortOptions returns valid user-facing choices for the model.
func EffortOptions(info Info) []string {
	out := []string{string(reasoning.EffortNone)}
	if !info.SupportsReasoning {
		return out
	}
	seen := map[reasoning.Effort]struct{}{reasoning.EffortNone: {}}
	for _, effort := range info.SupportedEfforts {
		effort, err := reasoning.NormalizeEffort(string(effort))
		if err != nil || effort == reasoning.EffortNone {
			continue
		}
		if _, ok := seen[effort]; ok {
			continue
		}
		seen[effort] = struct{}{}
		out = append(out, string(effort))
	}
	return out
}

// ValidateEffort canonicalizes value and verifies it is available for info.
func ValidateEffort(info Info, value string) (reasoning.Effort, error) {
	effort, err := reasoning.NormalizeEffort(value)
	if err != nil {
		return "", err
	}
	if effort == reasoning.EffortNone {
		return effort, nil
	}
	if info.SupportsReasoning && reasoning.Contains(info.SupportedEfforts, effort) {
		return effort, nil
	}
	return "", fmt.Errorf("reasoning effort %q is not available for model %q", effort, info.ID)
}

func userModelConfig(providerCfg config.ProviderConfig, model string) (config.ModelConfig, bool) {
	for _, key := range modelKeys(model) {
		if cfg, ok := providerCfg.Models[key]; ok {
			return cfg, true
		}
	}
	return config.ModelConfig{}, false
}

func mergeConfigInfo(base Info, cfg config.ModelConfig) Info {
	if cfg.MaxContextTokens > 0 {
		base.MaxContextTokens = cfg.MaxContextTokens
	}
	efforts := normalizeEfforts(cfg.SupportedEfforts)
	supports := cfg.SupportsReasoning || len(efforts) > 0 || cfg.DefaultEffort != ""
	base.SupportsReasoning = supports
	if !supports {
		base.SupportedEfforts = nil
		base.DefaultEffort = reasoning.EffortNone
		return base
	}
	if len(efforts) == 0 {
		efforts = standardEfforts()
	}
	base.SupportedEfforts = efforts
	defaultEffort, err := reasoning.NormalizeEffort(string(cfg.DefaultEffort))
	if err != nil || defaultEffort == reasoning.EffortNone || !reasoning.Contains(efforts, defaultEffort) {
		defaultEffort = firstPreferredDefault(efforts)
	}
	base.DefaultEffort = defaultEffort
	return base
}

func builtinInfo(providerType, model string) (Info, bool) {
	model = canonicalModelID(model)
	switch strings.TrimSpace(providerType) {
	case "openai":
		if hasAnyPrefix(model, "o1", "o3", "o4", "gpt-5") {
			return reasoningInfo(reasoning.MustEfforts(reasoning.EffortLow, reasoning.EffortMedium, reasoning.EffortHigh), reasoning.EffortMedium), true
		}
	case "anthropic":
		if strings.Contains(model, "claude-3-7") || strings.Contains(model, "claude-sonnet-4") || strings.Contains(model, "claude-opus-4") {
			return reasoningInfo(standardEfforts(), reasoning.EffortMedium), true
		}
	}
	return Info{}, false
}

func reasoningInfo(efforts []reasoning.Effort, defaultEffort reasoning.Effort) Info {
	return Info{
		SupportsReasoning: true,
		SupportedEfforts:  normalizeEfforts(efforts),
		DefaultEffort:     defaultEffort,
	}
}

func standardEfforts() []reasoning.Effort {
	return reasoning.MustEfforts(reasoning.EffortMinimal, reasoning.EffortLow, reasoning.EffortMedium, reasoning.EffortHigh, reasoning.EffortXHigh)
}

func normalizeEfforts(efforts []reasoning.Effort) []reasoning.Effort {
	seen := map[reasoning.Effort]struct{}{}
	var out []reasoning.Effort
	for _, value := range efforts {
		effort, err := reasoning.NormalizeEffort(string(value))
		if err != nil || effort == reasoning.EffortNone {
			continue
		}
		if _, ok := seen[effort]; ok {
			continue
		}
		seen[effort] = struct{}{}
		out = append(out, effort)
	}
	return out
}

func firstPreferredDefault(efforts []reasoning.Effort) reasoning.Effort {
	for _, preferred := range []reasoning.Effort{reasoning.EffortMedium, reasoning.EffortLow, reasoning.EffortHigh, reasoning.EffortMinimal, reasoning.EffortXHigh} {
		if reasoning.Contains(efforts, preferred) {
			return preferred
		}
	}
	return reasoning.EffortNone
}

func modelKeys(model string) []string {
	model = strings.TrimSpace(model)
	if model == "" {
		return nil
	}
	keys := []string{model}
	if _, rest, ok := strings.Cut(model, "/"); ok {
		keys = append(keys, strings.TrimSpace(rest))
	}
	return keys
}

func canonicalModelID(model string) string {
	model = strings.ToLower(strings.TrimSpace(model))
	if _, rest, ok := strings.Cut(model, "/"); ok {
		model = strings.TrimSpace(rest)
	}
	return model
}

func hasAnyPrefix(value string, prefixes ...string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}
