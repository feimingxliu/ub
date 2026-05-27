package shell

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/feimingxliu/ub/internal/tool"
)

func TestBash_ImplementsStreamingTool(t *testing.T) {
	var _ tool.StreamingTool = newBashTool(t.TempDir())
}

func TestBash_ExecuteStream_EmitsChunks(t *testing.T) {
	skipOnWindows(t)
	b := newBashTool(t.TempDir())
	events := make(chan tool.StreamEvent, 64)

	raw, _ := json.Marshal(bashArgs{Command: `printf a; sleep 0.05; printf b; sleep 0.05; printf c`})
	resCh := make(chan tool.Result, 1)
	errCh := make(chan error, 1)
	go func() {
		res, err := b.ExecuteStream(context.Background(), raw, events)
		close(events)
		errCh <- err
		resCh <- res
	}()

	var chunks []string
	for ev := range events {
		if ev.Kind == tool.StreamStdout {
			chunks = append(chunks, ev.Data)
		}
	}
	if err := <-errCh; err != nil {
		t.Fatalf("ExecuteStream: %v", err)
	}
	res := <-resCh
	if len(chunks) < 2 {
		t.Fatalf("expected ≥2 stdout chunks, got %d: %v", len(chunks), chunks)
	}
	combined := strings.Join(chunks, "")
	if combined != "abc" {
		t.Fatalf("chunks concat = %q, want abc", combined)
	}
	if !strings.Contains(res.Content, "abc") {
		t.Fatalf("final Result.Content missing concatenated body:\n%s", res.Content)
	}
}

func TestBash_ExecuteStream_StderrKind(t *testing.T) {
	skipOnWindows(t)
	b := newBashTool(t.TempDir())
	events := make(chan tool.StreamEvent, 64)

	raw, _ := json.Marshal(bashArgs{Command: `printf hi 1>&2`})
	go func() {
		_, _ = b.ExecuteStream(context.Background(), raw, events)
		close(events)
	}()
	sawStderr := false
	for ev := range events {
		if ev.Kind == tool.StreamStderr && strings.Contains(ev.Data, "hi") {
			sawStderr = true
		}
	}
	if !sawStderr {
		t.Fatalf("expected stderr chunk for `printf hi 1>&2`")
	}
}
