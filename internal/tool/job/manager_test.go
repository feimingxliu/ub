package job

import (
	"context"
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
	if runtime.GOOS == "windows" {
		t.Skip("windows V1 job shell fixtures do not provide stable large stdout semantics")
	}
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

func TestManager_MaxConcurrentRejectsNewJob(t *testing.T) {
	mgr := NewManagerWithOptions(t.TempDir(), ManagerOptions{MaxConcurrent: 1})
	j, err := mgr.Start("", longRunningCommand())
	if err != nil {
		t.Fatalf("start first: %v", err)
	}
	t.Cleanup(func() {
		_ = mgr.Shutdown(context.Background())
	})

	if _, err := mgr.Start("", longRunningCommand()); err == nil || !strings.Contains(err.Error(), "maximum concurrent jobs") {
		t.Fatalf("expected max-concurrent error, got %v", err)
	}
	if _, ok := mgr.Get(j.id); !ok {
		t.Fatalf("first job disappeared after rejected start")
	}
}

func TestManager_PruneCompletedRemovesExpiredJobs(t *testing.T) {
	now := time.Date(2026, 5, 23, 10, 0, 0, 0, time.UTC)
	mgr := NewManagerWithOptions(t.TempDir(), ManagerOptions{Retention: time.Hour})
	expired := &job{id: "expired", state: stateExited, exitCode: 0, completedAt: now.Add(-2 * time.Hour), stdout: newRing(streamCap), stderr: newRing(streamCap), done: closedDone()}
	recent := &job{id: "recent", state: stateExited, exitCode: 0, completedAt: now.Add(-30 * time.Minute), stdout: newRing(streamCap), stderr: newRing(streamCap), done: closedDone()}
	mgr.jobs[expired.id] = expired
	mgr.jobs[recent.id] = recent

	if deleted := mgr.PruneCompleted(now); deleted != 1 {
		t.Fatalf("deleted = %d, want 1", deleted)
	}
	if _, ok := mgr.Get("expired"); ok {
		t.Fatalf("expired job was not pruned")
	}
	if _, ok := mgr.Get("recent"); !ok {
		t.Fatalf("recent job should remain")
	}
}

func TestManager_ShutdownTerminatesRunningJobs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows V1 job support has no process-group shutdown semantics")
	}
	mgr := NewManagerWithOptions(t.TempDir(), ManagerOptions{MaxConcurrent: 2})
	first, err := mgr.Start("", longRunningCommand())
	if err != nil {
		t.Fatalf("start first: %v", err)
	}
	second, err := mgr.Start("", longRunningCommand())
	if err != nil {
		t.Fatalf("start second: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := mgr.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	for _, j := range []*job{first, second} {
		j.mu.Lock()
		state := j.state
		killed := j.killed
		reason := j.killReason
		j.mu.Unlock()
		if state != stateExited || !killed || reason != "killed by manager shutdown" {
			t.Fatalf("job %s state=%s killed=%t reason=%q", j.id, state, killed, reason)
		}
	}
}

func closedDone() chan struct{} {
	done := make(chan struct{})
	close(done)
	return done
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
	return "awk 'BEGIN{for(i=0;i<40000;i++)printf \"x\"}'"
}
