package shell

import (
	"context"
	"encoding/json"
	"runtime"
	"strings"
	"testing"

	"github.com/feimingxliu/ub/internal/tool"
)

func TestBash_ImplementsStreamingTool(t *testing.T) {
	var _ tool.StreamingTool = newBashTool(t.TempDir())
}

func shellStreamingChunksCommand() string {
	if runtime.GOOS == "windows" {
		return `echo a & ping 127.0.0.1 -n 2 >NUL & echo b & ping 127.0.0.1 -n 2 >NUL & echo c`
	}
	return `printf a; sleep 0.05; printf b; sleep 0.05; printf c`
}

func TestBash_ExecuteStream_EmitsChunks(t *testing.T) {
	b := newBashTool(t.TempDir())
	events := make(chan tool.StreamEvent, 64)

	raw, _ := json.Marshal(bashArgs{Command: shellStreamingChunksCommand()})
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
	normalized := strings.NewReplacer("\r", "", "\n", "", " ", "").Replace(combined)
	if normalized != "abc" {
		t.Fatalf("chunks concat = %q, want abc", combined)
	}
	finalOutput := strings.NewReplacer("\r", "", "\n", "", " ", "").Replace(res.Content)
	if !strings.Contains(finalOutput, "abc") {
		t.Fatalf("final Result.Content missing concatenated body:\n%s", res.Content)
	}
}

func TestBash_ExecuteStream_StderrKind(t *testing.T) {
	b := newBashTool(t.TempDir())
	events := make(chan tool.StreamEvent, 64)

	raw, _ := json.Marshal(bashArgs{Command: `echo hi 1>&2`})
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
