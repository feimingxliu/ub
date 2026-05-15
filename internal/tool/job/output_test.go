package job

import (
	"strings"
	"testing"
	"time"
)

func TestOutputTool_RunningJob(t *testing.T) {
	skipOnWindows(t)
	mgr := NewManager(t.TempDir())
	run := newRunTool(mgr)
	out := newOutputTool(mgr)
	// command produces a few lines fast then sleeps to stay running.
	res, err := execTool(t, run, runArgs{Command: "for i in 1 2 3; do echo line$i; done; sleep 30"})
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

func TestOutputTool_NotFound(t *testing.T) {
	skipOnWindows(t)
	mgr := NewManager(t.TempDir())
	out := newOutputTool(mgr)
	_, err := execTool(t, out, outputArgs{JobID: "nope"})
	if err == nil || !strings.Contains(err.Error(), "job not found") {
		t.Fatalf("expected job-not-found error, got: %v", err)
	}
}

func TestOutputTool_MissingID(t *testing.T) {
	skipOnWindows(t)
	mgr := NewManager(t.TempDir())
	out := newOutputTool(mgr)
	if _, err := execTool(t, out, outputArgs{}); err == nil {
		t.Fatalf("expected required-id error")
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
