package command

import (
	"testing"

	"github.com/feimingxliu/ub/internal/config"
	"github.com/feimingxliu/ub/internal/reasoning"
)

func TestModelRoleKeepsExplicitNone(t *testing.T) {
	cfg := config.ProviderConfig{Type: "openai-compat", Models: map[string]config.ModelConfig{
		"qwen": {SupportsReasoning: true, SupportedEfforts: []reasoning.Effort{reasoning.EffortNone, reasoning.EffortMax}, DefaultEffort: reasoning.EffortMax},
	}}
	for _, effort := range []reasoning.Effort{reasoning.EffortMax, reasoning.EffortNone} {
		role := resolveModelRole(modelRoleMain, "local", cfg, "qwen", reasoning.Config{Effort: effort})
		got := role.cloneReasoning()
		if got == nil || got.Effort != effort {
			t.Fatalf("role reasoning=%#v, want %s", got, effort)
		}
		if len(role.Efforts) != 2 || role.Efforts[1] != "max" {
			t.Fatalf("role efforts=%v", role.Efforts)
		}
	}
}
