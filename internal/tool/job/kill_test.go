package job

import (
	"strings"
	"testing"
	"time"
)

func TestKillTool_HappyPath(t *testing.T) {
	mgr := NewManager(t.TempDir())
	run := newRunTool(mgr)
	kill := newKillTool(mgr)

	runRes, err := execTool(t, run, runArgs{Command: longRunningCommand()})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	id := extractField(t, runRes.Content, "job_id=")

	start := time.Now()
	res, err := execTool(t, kill, killArgs{JobID: id})
	if err != nil {
		t.Fatalf("kill: %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("kill took too long: %s", time.Since(start))
	}
	for _, want := range []string{"state=exited", "killed=true", "exit_code="} {
		if !strings.Contains(res.Content, want) {
			t.Errorf("missing %s in:\n%s", want, res.Content)
		}
	}
}

func TestKillTool_Idempotent(t *testing.T) {
	mgr := NewManager(t.TempDir())
	run := newRunTool(mgr)
	kill := newKillTool(mgr)

	runRes, err := execTool(t, run, runArgs{Command: successCommand()})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	id := extractField(t, runRes.Content, "job_id=")
	<-mustGet(t, mgr, id).done

	res, err := execTool(t, kill, killArgs{JobID: id})
	if err != nil {
		t.Fatalf("kill exited job: %v", err)
	}
	if !strings.Contains(res.Content, "killed=false") {
		t.Errorf("expected killed=false:\n%s", res.Content)
	}
	if !strings.Contains(res.Content, "exit_code=0") {
		t.Errorf("expected exit_code=0:\n%s", res.Content)
	}
}

func TestKillTool_NotFound(t *testing.T) {
	mgr := NewManager(t.TempDir())
	kill := newKillTool(mgr)
	_, err := execTool(t, kill, killArgs{JobID: "nope"})
	if err == nil || !strings.Contains(err.Error(), "job not found") {
		t.Fatalf("expected job-not-found, got: %v", err)
	}
}

func TestKillTool_MissingID(t *testing.T) {
	mgr := NewManager(t.TempDir())
	kill := newKillTool(mgr)
	if _, err := execTool(t, kill, killArgs{}); err == nil {
		t.Fatalf("expected required-id error")
	}
}
