package tool

import (
	"context"
	"testing"
)

func TestWithSessionID_RoundTrip(t *testing.T) {
	ctx := WithSessionID(context.Background(), "sess-1")
	if got := SessionIDFromContext(ctx); got != "sess-1" {
		t.Fatalf("SessionIDFromContext = %q, want sess-1", got)
	}
}

func TestSessionIDFromContext_Empty(t *testing.T) {
	if got := SessionIDFromContext(context.Background()); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestWithSessionID_EmptySessionIsNoop(t *testing.T) {
	ctx := WithSessionID(context.Background(), "")
	if got := SessionIDFromContext(ctx); got != "" {
		t.Fatalf("empty sid leaked into ctx: %q", got)
	}
}

type fakeSubagentRunner struct{ called int }

func (f *fakeSubagentRunner) RunSubagent(_ context.Context, _ string, _ int) (string, error) {
	f.called++
	return "ok", nil
}

func TestSubagentRunner_RoundTrip(t *testing.T) {
	r := &fakeSubagentRunner{}
	ctx := WithSubagentRunner(context.Background(), r)
	got := SubagentRunnerFromContext(ctx)
	if got != r {
		t.Fatalf("runner round-trip broken: %#v vs %#v", got, r)
	}
}

func TestSubagentRunner_NilDropped(t *testing.T) {
	ctx := WithSubagentRunner(context.Background(), nil)
	if got := SubagentRunnerFromContext(ctx); got != nil {
		t.Fatalf("nil runner leaked: %#v", got)
	}
}

func TestSubagentDepth_DefaultZero(t *testing.T) {
	if got := SubagentDepthFromContext(context.Background()); got != 0 {
		t.Fatalf("default depth = %d, want 0", got)
	}
}

func TestSubagentDepth_RoundTrip(t *testing.T) {
	ctx := WithSubagentDepth(context.Background(), 1)
	if got := SubagentDepthFromContext(ctx); got != 1 {
		t.Fatalf("depth = %d, want 1", got)
	}
}
