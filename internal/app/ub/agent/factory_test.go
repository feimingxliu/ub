package agent

import (
	"testing"

	"github.com/feimingxliu/ub/internal/pkg/core/execution"
	"github.com/feimingxliu/ub/internal/pkg/llm/provider/fake"
	"github.com/feimingxliu/ub/internal/pkg/tool"
)

func TestFactoryCreatesFreshAgentsFromTemplate(t *testing.T) {
	reg := tool.New()
	factory := NewFactory(Options{
		Provider: fake.New(nil),
		Tools:    reg,
		Model:    "base/model",
		Mode:     execution.ModeWork,
	})

	first, err := factory.New(nil)
	if err != nil {
		t.Fatalf("New first: %v", err)
	}
	second, err := factory.New(nil)
	if err != nil {
		t.Fatalf("New second: %v", err)
	}
	if first == second {
		t.Fatalf("factory returned the same Agent instance")
	}
	if first.model != "base/model" || second.model != "base/model" {
		t.Fatalf("models = %q/%q, want base/model", first.model, second.model)
	}

	overridden, err := factory.New(func(opts *Options) {
		opts.Model = "override/model"
	})
	if err != nil {
		t.Fatalf("New override: %v", err)
	}
	if overridden.model != "override/model" {
		t.Fatalf("override model = %q, want override/model", overridden.model)
	}
	if factory.Options().Model != "base/model" {
		t.Fatalf("factory template mutated to %q", factory.Options().Model)
	}
}
