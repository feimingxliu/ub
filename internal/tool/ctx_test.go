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
