package job

import (
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestManager_StartAndWaitForExit(t *testing.T) {
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
	mgr := NewManager(t.TempDir())
	j, err := mgr.Start("", longRunningCommand())
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
	mgr := NewManager(t.TempDir())
	j, err := mgr.Start("", successCommand())
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
	mgr := NewManager(t.TempDir())
	j, err := mgr.Start("", largeOutputCommand())
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
	mgr := NewManager(t.TempDir())
	if _, err := mgr.Start("../", "pwd"); err == nil {
		t.Fatalf("expected sandbox error")
	}
}

func TestManager_EmptyCommand(t *testing.T) {
	mgr := NewManager(t.TempDir())
	if _, err := mgr.Start("", ""); err == nil {
		t.Fatalf("expected empty-command error")
	}
}

func successCommand() string {
	if runtime.GOOS == "windows" {
		return "exit 0"
	}
	return "true"
}

func longRunningCommand() string {
	if runtime.GOOS == "windows" {
		return "ping -n 30 127.0.0.1 >NUL"
	}
	return "sleep 30"
}

func runningOutputCommand() string {
	if runtime.GOOS == "windows" {
		return "echo line1 & echo line2 & echo line3 & ping -n 30 127.0.0.1 >NUL"
	}
	return "for i in 1 2 3; do echo line$i; done; sleep 30"
}

func largeOutputCommand() string {
	if runtime.GOOS == "windows" {
		return `powershell -NoProfile -Command "$s = 'x' * 40000; [Console]::Out.Write($s)"`
	}
	return "awk 'BEGIN{for(i=0;i<40000;i++)printf \"x\"}'"
}
