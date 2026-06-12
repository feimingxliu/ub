package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/invopop/jsonschema"

	"github.com/feimingxliu/ub/internal/pkg/core/execution"
	"github.com/feimingxliu/ub/internal/pkg/llm/provider"
	"github.com/feimingxliu/ub/internal/pkg/tool"
)

type benchmarkSchemaTool struct {
	name   string
	risk   tool.Risk
	schema *jsonschema.Schema
}

func (t benchmarkSchemaTool) Name() string        { return t.name }
func (t benchmarkSchemaTool) Description() string { return "benchmark schema tool" }
func (t benchmarkSchemaTool) Schema() *jsonschema.Schema {
	if t.schema != nil {
		return t.schema
	}
	return jsonschema.Reflect(&struct {
		Path      string   `json:"path" jsonschema:"required"`
		Pattern   string   `json:"pattern,omitempty"`
		Limit     int      `json:"limit,omitempty"`
		Recursive bool     `json:"recursive,omitempty"`
		Tags      []string `json:"tags,omitempty"`
	}{})
}
func (t benchmarkSchemaTool) Risk() tool.Risk { return t.risk }
func (t benchmarkSchemaTool) Execute(context.Context, json.RawMessage) (tool.Result, error) {
	return tool.Result{Content: "ok"}, nil
}

func BenchmarkToolDefinitionsWorkMode(b *testing.B) {
	reg := benchmarkToolRegistry(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		defs, err := toolDefinitions(reg, execution.ModeWork)
		if err != nil {
			b.Fatal(err)
		}
		if len(defs) == 0 {
			b.Fatal("no tool definitions")
		}
	}
}

func BenchmarkAgentToolDefinitionsCachedWorkMode(b *testing.B) {
	reg := benchmarkToolRegistry(b)
	a := &Agent{tools: reg, toolDefinitionCache: map[execution.Mode][]provider.ToolDefinition{}}
	if _, err := a.toolDefinitions(execution.ModeWork); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		defs, err := a.toolDefinitions(execution.ModeWork)
		if err != nil {
			b.Fatal(err)
		}
		if len(defs) == 0 {
			b.Fatal("no tool definitions")
		}
	}
}

func benchmarkToolRegistry(b *testing.B) *tool.Registry {
	b.Helper()
	reg := tool.New()
	for i := 0; i < 80; i++ {
		risk := tool.RiskSafe
		if i%5 == 0 {
			risk = tool.RiskWrite
		}
		if i%7 == 0 {
			risk = tool.RiskExec
		}
		if err := reg.Register(benchmarkSchemaTool{name: fmt.Sprintf("bench_tool_%03d", i), risk: risk}); err != nil {
			b.Fatal(err)
		}
	}
	return reg
}
