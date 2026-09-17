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
				MaxContextTokens:  200000,
			},
		},
	}, "custom")

	if !info.SupportsReasoning || info.DefaultEffort != reasoning.EffortHigh || info.MaxContextTokens != 200000 {
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

func TestConfiguredMaxAndExplicitNone(t *testing.T) {
	for _, tc := range []struct {
		name          string
		preferred     reasoning.Effort
		defaultEffort reasoning.Effort
		want          reasoning.Effort
	}{
		{"unset uses max default", "", reasoning.EffortMax, reasoning.EffortMax},
		{"unsupported preference uses max default", reasoning.EffortHigh, reasoning.EffortMax, reasoning.EffortMax},
		{"explicit max", reasoning.EffortMax, reasoning.EffortNone, reasoning.EffortMax},
		{"explicit none overrides max", reasoning.EffortNone, reasoning.EffortMax, reasoning.EffortNone},
		{"explicit none default", "", reasoning.EffortNone, reasoning.EffortNone},
		{"unset default selects declared max", "", "", reasoning.EffortMax},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := Resolve("local", config.ProviderConfig{Type: "openai-compat", Models: map[string]config.ModelConfig{
				"qwen": {SupportsReasoning: true, SupportedEfforts: []reasoning.Effort{reasoning.EffortNone, reasoning.EffortMax}, DefaultEffort: tc.defaultEffort},
			}}, "qwen")
			opts := EffortOptions(info)
			if len(opts) != 2 || opts[0] != "none" || opts[1] != "max" {
				t.Fatalf("options = %v", opts)
			}
			got := RequestConfig(reasoning.Config{Effort: tc.preferred, Summary: "auto"}, info)
			if got == nil || got.Effort != tc.want || got.Summary != "auto" {
				t.Fatalf("request = %#v, want %s", got, tc.want)
			}
		})
	}
}

func TestMaxIsNotAdvertisedByDefault(t *testing.T) {
	for _, cfg := range []config.ProviderConfig{
		{Type: "openai"},
		{Type: "openai-compat", Models: map[string]config.ModelConfig{"gpt-5": {SupportsReasoning: true}}},
	} {
		info := Resolve("test", cfg, "gpt-5")
		if reasoning.Contains(info.SupportedEfforts, reasoning.EffortMax) {
			t.Fatal("max must be explicitly declared")
		}
		if _, err := ValidateEffort(info, "max"); err == nil {
			t.Fatal("undeclared max accepted")
		}
	}
	info := Resolve("test", config.ProviderConfig{Type: "openai-compat"}, "unknown")
	for _, effort := range []reasoning.Effort{"", reasoning.EffortNone, reasoning.EffortMax} {
		if got := RequestConfig(reasoning.Config{Effort: effort}, info); got != nil {
			t.Fatalf("unknown model request = %#v", got)
		}
	}
}
