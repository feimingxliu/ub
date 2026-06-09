package cli

import (
	"context"
	"testing"

	"github.com/feimingxliu/ub/internal/config"
)

func TestNewSummarySetupUsesSmallModelWhenAvailableForProvider(t *testing.T) {
	cfg := &config.Config{SmallModel: "small/model"}
	providerCfg := config.ProviderConfig{
		Type: "fake",
		Models: map[string]config.ModelConfig{
			"small/model": {},
			"main/model":  {},
		},
	}

	setup, err := newSummarySetup(context.Background(), cfg, "manual", providerCfg, "main/model")
	if err != nil {
		t.Fatalf("newSummarySetup: %v", err)
	}
	if setup.Model != "small/model" || setup.UsesCurrentModel {
		t.Fatalf("setup = %#v, want small/model without current-model fallback", setup)
	}
}

func TestNewSummarySetupFallsBackWhenSmallModelUnavailableForProvider(t *testing.T) {
	cfg := &config.Config{SmallModel: "small/model"}
	providerCfg := config.ProviderConfig{
		Type: "fake",
		Models: map[string]config.ModelConfig{
			"main/model": {},
		},
	}

	setup, err := newSummarySetup(context.Background(), cfg, "manual", providerCfg, "main/model")
	if err != nil {
		t.Fatalf("newSummarySetup: %v", err)
	}
	if setup.Model != "main/model" || !setup.UsesCurrentModel {
		t.Fatalf("setup = %#v, want main/model current-model fallback", setup)
	}
}

func TestNewSummarySetupKeepsSmallModelWhenProviderModelsUnknown(t *testing.T) {
	cfg := &config.Config{SmallModel: "custom-small"}
	providerCfg := config.ProviderConfig{Type: "fake"}

	setup, err := newSummarySetup(context.Background(), cfg, "manual", providerCfg, "main/model")
	if err != nil {
		t.Fatalf("newSummarySetup: %v", err)
	}
	if setup.Model != "custom-small" || setup.UsesCurrentModel {
		t.Fatalf("setup = %#v, want custom-small preserved when candidates are unknown", setup)
	}
}
