package modelinfo

import (
	"testing"

	"github.com/feimingxliu/ub/internal/config"
	"github.com/feimingxliu/ub/internal/reasoning"
)

func TestResolveUsesUserModelConfig(t *testing.T) {
	info := Resolve("compat", config.ProviderConfig{
		Type: "openai-compat",
		Models: map[string]config.ModelConfig{
			"custom": {
				SupportsReasoning: true,
				SupportedEfforts:  []reasoning.Effort{reasoning.EffortLow, reasoning.EffortHigh},
				DefaultEffort:     reasoning.EffortHigh,
			},
		},
	}, "custom")

	if !info.SupportsReasoning || info.DefaultEffort != reasoning.EffortHigh {
		t.Fatalf("info = %#v", info)
	}
	if got := EffortOptions(info); len(got) != 3 || got[0] != "none" || got[2] != "high" {
		t.Fatalf("effort options = %#v", got)
	}
}

func TestResolveBuiltInOpenAIReasoningModel(t *testing.T) {
	info := Resolve("openai", config.ProviderConfig{Type: "openai"}, "openai/gpt-5")
	if !info.SupportsReasoning || !reasoning.Contains(info.SupportedEfforts, reasoning.EffortMedium) {
		t.Fatalf("info = %#v", info)
	}
}

func TestResolveUnknownModelConservative(t *testing.T) {
	info := Resolve("compat", config.ProviderConfig{Type: "openai-compat"}, "unknown-model")
	if info.SupportsReasoning {
		t.Fatalf("unknown model should not support reasoning: %#v", info)
	}
	if got := RequestConfig(reasoning.Config{Effort: reasoning.EffortHigh}, info); got != nil {
		t.Fatalf("request config = %#v, want nil", got)
	}
}

func TestValidateEffortRejectsUnsupported(t *testing.T) {
	info := Info{
		ID:                "test",
		SupportsReasoning: true,
		SupportedEfforts:  []reasoning.Effort{reasoning.EffortLow},
		DefaultEffort:     reasoning.EffortLow,
	}
	if _, err := ValidateEffort(info, "high"); err == nil {
		t.Fatal("expected unsupported effort error")
	}
	if effort, err := ValidateEffort(info, "none"); err != nil || effort != reasoning.EffortNone {
		t.Fatalf("none effort = %q err=%v", effort, err)
	}
}
