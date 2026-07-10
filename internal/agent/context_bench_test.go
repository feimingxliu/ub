package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/feimingxliu/ub/internal/config"
	"github.com/feimingxliu/ub/internal/message"
	execmode "github.com/feimingxliu/ub/internal/mode"
	"github.com/feimingxliu/ub/internal/provider"
	"github.com/feimingxliu/ub/internal/provider/fake"
	contextmgr "github.com/feimingxliu/ub/internal/tokenizer"
)

func BenchmarkPrepareMessagesHistory(b *testing.B) {
	a, tools := benchmarkPrepareMessagesAgent(b, 1_000_000)
	for _, turns := range []int{20, 200, 1000} {
		b.Run(fmt.Sprintf("turns=%d", turns), func(b *testing.B) {
			history := hugeTurnHistory(turns, 120)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				prepared, err := a.prepareMessages(context.Background(), "", 1, history, tools)
				if err != nil {
					b.Fatal(err)
				}
				if len(prepared.requestMessages) <= len(history) || prepared.estimatedTokens <= 0 {
					b.Fatalf("unexpected prepared messages: len=%d estimate=%d", len(prepared.requestMessages), prepared.estimatedTokens)
				}
			}
		})
	}
}

func BenchmarkPrepareMessagesWithSummary(b *testing.B) {
	a, tools := benchmarkPrepareMessagesAgent(b, 30_000)
	history := hugeTurnHistory(80, 300)
	if !a.shouldSummarize(contextEstimateForBenchmark(a, history, tools)) {
		b.Fatal("benchmark history did not trigger summary")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		prepared, err := a.prepareMessages(context.Background(), "", 1, history, tools)
		if err != nil {
			b.Fatal(err)
		}
		if len(prepared.messages) >= len(history) || prepared.estimatedTokens <= 0 {
			b.Fatalf("unexpected compacted messages: len=%d history=%d estimate=%d", len(prepared.messages), len(history), prepared.estimatedTokens)
		}
	}
}

func benchmarkPrepareMessagesAgent(b *testing.B, maxContext int) (*Agent, []provider.ToolDefinition) {
	b.Helper()
	reg := benchmarkToolRegistry(b)
	p := benchmarkSummaryProvider{maxContext: maxContext}
	workspace := b.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "AGENTS.md"), []byte("Use make test for validation.\nKeep changes scoped.\n"), 0o600); err != nil {
		b.Fatal(err)
	}
	a, err := New(Options{
		Provider:         p,
		Tools:            reg,
		Model:            "fake/model",
		Mode:             execmode.ModeWork,
		MaxContextTokens: maxContext,
		SummaryProvider:  p,
		SummaryModel:     "fake/model",
		Context: config.ContextConfig{
			TriggerRatio:        0.8,
			KeepRecentTurns:     3,
			ReserveOutputTokens: 12_000,
		},
		Runtime: RuntimeContext{
			Workspace: workspace,
			Shell:     "/bin/sh",
			OS:        "linux",
		},
		WorkspaceRoot: workspace,
	})
	if err != nil {
		b.Fatal(err)
	}
	tools, err := a.toolDefinitions(execmode.ModeWork)
	if err != nil {
		b.Fatal(err)
	}
	return a, tools
}

func contextEstimateForBenchmark(a *Agent, messages []message.Message, tools []provider.ToolDefinition) int {
	prepared := a.withRuntimeContext(messages)
	return contextmgr.EstimateRequest(prepared, tools, a.model)
}

type benchmarkSummaryProvider struct {
	maxContext int
}

func (p benchmarkSummaryProvider) Name() string { return "benchmark-summary" }

func (p benchmarkSummaryProvider) Caps() provider.Caps {
	return provider.Caps{
		SupportsTools:     true,
		SupportsStreaming: true,
		MaxContextTokens:  p.maxContext,
	}
}

func (p benchmarkSummaryProvider) Chat(ctx context.Context, req provider.Request) (provider.Stream, error) {
	return fake.New(fake.Script{fake.TextDelta("benchmark summary"), fake.Done()}).Chat(ctx, req)
}
