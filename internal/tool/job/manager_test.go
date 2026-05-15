package job

import (
	"runtime"
	"strings"
	"testing"
	"time"
)

func skipOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("job tools not supported on windows in V1")
	}
}

func TestManager_StartAndWaitForExit(t *testing.T) {
	skipOnWindows(t)
	mgr := NewManager(t.TempDir())
	j, err := mgr.Start("", "echo hi")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	select {
	case <-j.done:
	case <-time.After(5 * time.Second):
		t.Fatalf("job did not finish in 5s")
	}
	j.mu.Lock()
	state := j.state
	code := j.exitCode
	stdout := string(j.stdout.Snapshot(0))
	j.mu.Unlock()
	if state != stateExited || code != 0 {
		t.Fatalf("state=%s exit=%d", state, code)
	}
	if !strings.Contains(stdout, "hi") {
		t.Fatalf("stdout=%q want contain hi", stdout)
	}
}

func TestManager_KillSleep(t *testing.T) {
	skipOnWindows(t)
	mgr := NewManager(t.TempDir())
	j, err := mgr.Start("", "sleep 30")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	start := time.Now()
	killed, err := mgr.Kill(j)
	if err != nil {
		t.Fatalf("kill: %v", err)
	}
	if !killed {
		t.Fatalf("expected killed=true on first kill")
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("kill took too long: %s", time.Since(start))
	}
	j.mu.Lock()
	state := j.state
	j.mu.Unlock()
	if state != stateExited {
		t.Fatalf("state=%s want exited", state)
	}
}

func TestManager_KillIdempotent(t *testing.T) {
	skipOnWindows(t)
	mgr := NewManager(t.TempDir())
	j, err := mgr.Start("", "true")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	<-j.done // wait for natural exit
	killed, err := mgr.Kill(j)
	if err != nil {
		t.Fatalf("kill exited: %v", err)
	}
	if killed {
		t.Fatalf("expected killed=false on already-exited job")
	}
}

func TestManager_RingTotalAndTail(t *testing.T) {
	skipOnWindows(t)
	mgr := NewManager(t.TempDir())
	// awk one-liner produces deterministic 40000 bytes of 'x'.
	j, err := mgr.Start("", "awk 'BEGIN{for(i=0;i<40000;i++)printf \"x\"}'")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	select {
	case <-j.done:
	case <-time.After(10 * time.Second):
		t.Fatalf("job did not finish in 10s")
	}
	j.mu.Lock()
	total := j.stdout.Total()
	snap := j.stdout.Snapshot(streamCap)
	j.mu.Unlock()
	if total != 40000 {
		t.Fatalf("stdout total=%d want 40000", total)
	}
	if len(snap) != streamCap {
		t.Fatalf("snapshot len=%d want %d", len(snap), streamCap)
	}
	for _, b := range snap {
		if b != 'x' {
			t.Fatalf("snapshot contains non-x byte: %v", b)
		}
	}
}

func TestManager_CwdOutsideRoot(t *testing.T) {
	skipOnWindows(t)
	mgr := NewManager(t.TempDir())
	if _, err := mgr.Start("../", "pwd"); err == nil {
		t.Fatalf("expected sandbox error")
	}
}

func TestManager_EmptyCommand(t *testing.T) {
	skipOnWindows(t)
	mgr := NewManager(t.TempDir())
	if _, err := mgr.Start("", ""); err == nil {
		t.Fatalf("expected empty-command error")
	}
}
