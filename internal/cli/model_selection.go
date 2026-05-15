package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/feimingxliu/ub/internal/config"
)

func selectProviderModel(ctx context.Context, providerName string, providerCfg config.ProviderConfig, model string) (string, error) {
	model = strings.TrimSpace(model)
	if model != "" {
		return model, nil
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

func providerRequiresModel(providerType string) bool {
	switch strings.TrimSpace(providerType) {
	case "anthropic", "openai", "openai-compat", "ollama":
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
