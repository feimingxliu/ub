package job

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/feimingxliu/ub/internal/tool"
)

func TestOutputTool_ImplementsStreamingTool(t *testing.T) {
	var _ tool.StreamingTool = newOutputTool(NewManager(t.TempDir()))
}

func TestOutputTool_RunningJob(t *testing.T) {
	mgr := NewManager(t.TempDir())
	run := newRunTool(mgr)
	out := newOutputTool(mgr)
	res, err := execTool(t, run, runArgs{Command: runningOutputCommand()})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	id := extractField(t, res.Content, "job_id=")

	// poll briefly until output appears, up to 2s.
	deadline := time.Now().Add(2 * time.Second)
	var output string
	for time.Now().Before(deadline) {
		res, err := execTool(t, out, outputArgs{JobID: id})
		if err != nil {
			t.Fatalf("output: %v", err)
		}
		output = res.Content
		if strings.Contains(output, "line3") {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !strings.Contains(output, "state=running") {
		t.Errorf("expected state=running in:\n%s", output)
	}
	if !strings.Contains(output, "exit_code=-1") {
		t.Errorf("expected exit_code=-1 in:\n%s", output)
	}
	if !strings.Contains(output, "line1") || !strings.Contains(output, "line3") {
		t.Errorf("missing line1/line3 in stdout:\n%s", output)
	}
	for _, divider := range []string{"--- stdout ---", "--- stderr ---", "stdout_total=", "stderr_total="} {
		if !strings.Contains(output, divider) {
			t.Errorf("missing %s in:\n%s", divider, output)
		}
	}

	// clean up the sleeping job
	if _, err := mgr.Kill(mustGet(t, mgr, id)); err != nil {
		t.Fatalf("cleanup kill: %v", err)
	}
}

func TestOutputTool_FollowStreamsInitialAndNewOutput(t *testing.T) {
	mgr := NewManager(t.TempDir())
	j := &job{
		id:       "job-1",
		state:    stateRunning,
		exitCode: -1,
		stdout:   newRing(streamCap),
		stderr:   newRing(streamCap),
		done:     make(chan struct{}),
	}
	_, _ = j.stdout.Write([]byte("old\n"))
	mgr.jobs[j.id] = j

	out := newOutputTool(mgr)
	events := make(chan tool.StreamEvent, 8)
	raw := json.RawMessage(`{"job_id":"job-1","follow":true,"timeout_ms":1000}`)
	resultCh := make(chan struct {
		res tool.Result
		err error
	}, 1)
	go func() {
		res, err := out.ExecuteStream(context.Background(), raw, events)
		resultCh <- struct {
			res tool.Result
			err error
		}{res: res, err: err}
	}()

	waitStreamEvent(t, events, tool.StreamStdout, "old\n")

	j.mu.Lock()
	_, _ = j.stdout.Write([]byte("new\n"))
	_, _ = j.stderr.Write([]byte("warn\n"))
	j.state = stateExited
	j.exitCode = 0
	j.mu.Unlock()
	close(j.done)

	waitStreamEvent(t, events, tool.StreamStdout, "new\n")
	waitStreamEvent(t, events, tool.StreamStderr, "warn\n")

	select {
	case got := <-resultCh:
		if got.err != nil {
			t.Fatalf("ExecuteStream: %v", got.err)
		}
		for _, want := range []string{"state=exited", "exit_code=0", "old\nnew\n", "--- stderr ---\nwarn\n"} {
			if !strings.Contains(got.res.Content, want) {
				t.Fatalf("missing %q in:\n%s", want, got.res.Content)
			}
		}
		if got.res.IsError {
			t.Fatalf("follow result should not be an error:\n%s", got.res.Content)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ExecuteStream did not finish after job exit")
	}
}

func TestOutputTool_FollowTimeoutReturnsRunningSnapshot(t *testing.T) {
	mgr := NewManager(t.TempDir())
	j := &job{
		id:       "job-1",
		state:    stateRunning,
		exitCode: -1,
		stdout:   newRing(streamCap),
		stderr:   newRing(streamCap),
		done:     make(chan struct{}),
	}
	_, _ = j.stdout.Write([]byte("still running\n"))
	mgr.jobs[j.id] = j

	out := newOutputTool(mgr)
	res, err := out.ExecuteStream(context.Background(), json.RawMessage(`{"job_id":"job-1","follow":true,"timeout_ms":20}`), make(chan tool.StreamEvent, 8))
	if err != nil {
		t.Fatalf("ExecuteStream: %v", err)
	}
	if res.IsError {
		t.Fatalf("follow timeout should return a usable snapshot, not an error:\n%s", res.Content)
	}
	for _, want := range []string{"state=running", "follow_timeout=true", "still running\n"} {
		if !strings.Contains(res.Content, want) {
			t.Fatalf("missing %q in:\n%s", want, res.Content)
		}
	}
}

func TestOutputTool_NotFound(t *testing.T) {
	mgr := NewManager(t.TempDir())
	out := newOutputTool(mgr)
	_, err := execTool(t, out, outputArgs{JobID: "nope"})
	if err == nil || !strings.Contains(err.Error(), "job not found") {
		t.Fatalf("expected job-not-found error, got: %v", err)
	}
}

func TestOutputTool_MissingID(t *testing.T) {
	mgr := NewManager(t.TempDir())
	out := newOutputTool(mgr)
	if _, err := execTool(t, out, outputArgs{}); err == nil {
		t.Fatalf("expected required-id error")
	}
}

func TestOutputTool_NegativeFollowTimeout(t *testing.T) {
	mgr := NewManager(t.TempDir())
	out := newOutputTool(mgr)
	_, err := out.Execute(context.Background(), json.RawMessage(`{"job_id":"job-1","follow":true,"timeout_ms":-1}`))
	if err == nil || !strings.Contains(err.Error(), "timeout_ms must be non-negative") {
		t.Fatalf("expected timeout validation error, got: %v", err)
	}
}

func TestOutputTool_TailAcceptsNumericString(t *testing.T) {
	mgr := NewManager(t.TempDir())
	j := &job{
		id:       "job-1",
		state:    stateExited,
		exitCode: 0,
		stdout:   newRing(streamCap),
		stderr:   newRing(streamCap),
		done:     make(chan struct{}),
	}
	close(j.done)
	_, _ = j.stdout.Write([]byte("abcdef"))
	mgr.jobs[j.id] = j

	out := newOutputTool(mgr)
	res, err := out.Execute(context.Background(), json.RawMessage(`{"job_id":"job-1","tail":"3"}`))
	if err != nil {
		t.Fatalf("output: %v", err)
	}
	if !strings.Contains(res.Content, "--- stdout ---\ndef") {
		t.Fatalf("tail did not apply:\n%s", res.Content)
	}
	if strings.Contains(res.Content, "--- stdout ---\nabcdef") {
		t.Fatalf("tail returned full stdout:\n%s", res.Content)
	}
}

func waitStreamEvent(t *testing.T, events <-chan tool.StreamEvent, kind tool.StreamEventKind, contains string) tool.StreamEvent {
	t.Helper()
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	for {
		select {
		case ev := <-events:
			if ev.Kind == kind && strings.Contains(ev.Data, contains) {
				return ev
			}
		case <-timer.C:
			t.Fatalf("timed out waiting for %s event containing %q", kind, contains)
		}
	}
}

func mustGet(t *testing.T, mgr *Manager, id string) *job {
	t.Helper()
	j, ok := mgr.Get(id)
	if !ok {
		t.Fatalf("job %s not found", id)
	}
	return j
}
