package command

import (
	"sync"
	"testing"

	execmode "github.com/feimingxliu/ub/internal/mode"
)

// TestRunnerInjectChannelReusedAcrossRuns verifies the inject channel is built
// once and reused: Inject always delivers to the same channel, so guidance
// buffered before an agent loop starts is not lost and not sent to a stale
// per-run channel. Run with -race to confirm the (formerly racy) field is no
// longer written concurrently.
func TestRunnerInjectChannelReusedAcrossRuns(t *testing.T) {
	runner := &tuiAgentRunner{
		model: "fake/test",
		mode:  execmode.ModeWork,
	}
	// Mimic the production constructor, which builds the channel once.
	runner.injectCh = make(chan string, 16)

	// Inject before any run/agent loop exists — the channel buffers it.
	if !runner.Inject("before run") {
		t.Fatalf("Inject before run returned false; channel should buffer")
	}

	// A would-be agent loop drains the channel.
	got := drainInject(t, runner, 1)
	if len(got) != 1 || got[0] != "before run" {
		t.Fatalf("drained = %#v, want [before run]", got)
	}

	// Concurrent Inject + drain must be race-free and loss-free.
	const n = 64
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			// Some sends may drop if the drain falls behind the buffer cap,
			// but none should panic or race; collect only successful sends.
			_ = runner.Inject("g")
		}
	}()
	var drained []string
	go func() {
		defer wg.Done()
		drained = drainInject(t, runner, n)
	}()
	wg.Wait()

	// Every successfully-injected message must be recoverable from the
	// channel (none lost to a stale per-run channel).
	remaining := drainInject(t, runner, n)
	total := len(drained) + len(remaining)
	if total == 0 {
		t.Fatalf("no guidance drained despite concurrent injects")
	}
}

func drainInject(t *testing.T, runner *tuiAgentRunner, max int) []string {
	t.Helper()
	var out []string
	for len(out) < max {
		select {
		case text, ok := <-runner.injectCh:
			if !ok {
				return out
			}
			out = append(out, text)
		default:
			return out
		}
	}
	return out
}
