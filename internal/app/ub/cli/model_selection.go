package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/feimingxliu/ub/internal/pkg/core/config"
)

func selectProviderModel(ctx context.Context, providerName string, providerCfg config.ProviderConfig, model string) (string, error) {
	model = strings.TrimSpace(model)
	if model != "" {
		return model, nil
	}
	if configured := firstConfiguredProviderModel(providerCfg); configured != "" {
		return configured, nil
	}
	check := checkProvider(ctx, providerName, providerCfg)
	if len(check.Models) > 0 {
		return check.Models[0], nil
	}
	if !providerRequiresModel(providerCfg.Type) {
		return "", nil
	}
	return "", missingModelError(providerName, check)
}

func selectConfiguredProviderModel(providerName string, providerCfg config.ProviderConfig, model string) (string, error) {
	model = strings.TrimSpace(model)
	if model != "" {
		return model, nil
	}
	if configured := firstConfiguredProviderModel(providerCfg); configured != "" {
		return configured, nil
	}
	if !providerRequiresModel(providerCfg.Type) {
		return "", nil
	}
	return "", fmt.Errorf("model required for provider %q: set model or configure providers.%s.models", providerName, providerName)
}

func firstConfiguredProviderModel(providerCfg config.ProviderConfig) string {
	if len(providerCfg.Models) == 0 {
		return ""
	}
	models := make([]string, 0, len(providerCfg.Models))
	for model := range providerCfg.Models {
		model = strings.TrimSpace(model)
		if model != "" {
			models = append(models, model)
		}
	}
	sort.Strings(models)
	if len(models) == 0 {
		return ""
	}
	return models[0]
}

func providerRequiresModel(providerType string) bool {
	switch strings.TrimSpace(providerType) {
	case "anthropic", "openai", "openai-compat":
		return true
	default:
		return false
	}
}

func missingModelError(providerName string, check providerCheck) error {
	status := strings.TrimSpace(check.Status)
	if status == "" {
		status = "unknown"
	}
	detail := status
	if strings.TrimSpace(check.Message) != "" {
		detail += ": " + strings.TrimSpace(check.Message)
	}
	return fmt.Errorf("model required for provider %q: set --model or default_model; provider did not report selectable models (%s)", providerName, detail)
}
