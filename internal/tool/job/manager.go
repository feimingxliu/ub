package job

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/feimingxliu/ub/internal/tool"
	"github.com/feimingxliu/ub/internal/tool/procgroup"
)

// jobState is the externally visible state of a job.
type jobState string

const (
	stateRunning jobState = "running"
	stateExited  jobState = "exited"
)

// Manager owns all jobs registered through Register. It is created once
// per Register call and shared by the three job tools via closure.
type Manager struct {
	root string

	mu   sync.Mutex
	jobs map[string]*job
}

// NewManager constructs a Manager bound to the given (already-cleaned)
// workspace root.
func NewManager(root string) *Manager {
	return &Manager{root: root, jobs: map[string]*job{}}
}

type job struct {
	id        string
	command   string
	startedAt time.Time

	cmd *exec.Cmd

	mu         sync.Mutex
	state      jobState
	exitCode   int
	finishedAt time.Time
	stdout     *ring
	stderr     *ring
	killOnce   sync.Once
	killed     bool
	killReason string
	done       chan struct{}
}

// ringWriter adapts a *ring into an io.Writer that takes the job lock
// on each Write. It is used as cmd.Stdout / cmd.Stderr.
type ringWriter struct {
	job *job
	r   *ring
}

func (w *ringWriter) Write(p []byte) (int, error) {
	w.job.mu.Lock()
	defer w.job.mu.Unlock()
	return w.r.Write(p)
}

// Start launches a new background process and returns the job. It
// resolves cwd through tool.Resolve so escapes are rejected before any
// process is spawned.
func (m *Manager) Start(cwd, command string) (*job, error) {
	if runtime.GOOS == "windows" {
		return nil, fmt.Errorf("job: not supported on windows in V1")
	}
	if command == "" {
		return nil, fmt.Errorf("job: command is required")
	}
	if cwd == "" {
		cwd = "."
	}
	absCwd, err := tool.Resolve(m.root, cwd)
	if err != nil {
		return nil, err
	}

	devNull, err := os.Open(os.DevNull)
	if err != nil {
		return nil, fmt.Errorf("job: open /dev/null: %w", err)
	}

	j := &job{
		id:        uuid.New().String(),
		command:   command,
		startedAt: time.Now(),
		state:     stateRunning,
		exitCode:  -1,
		stdout:    newRing(streamCap),
		stderr:    newRing(streamCap),
		done:      make(chan struct{}),
	}

	cmd := exec.Command("/bin/sh", "-c", command)
	cmd.Dir = absCwd
	cmd.Stdin = devNull
	cmd.Stdout = &ringWriter{job: j, r: j.stdout}
	cmd.Stderr = &ringWriter{job: j, r: j.stderr}
	procgroup.Set(cmd)

	if err := cmd.Start(); err != nil {
		devNull.Close()
		return nil, fmt.Errorf("job: start failed: %w", err)
	}
	j.cmd = cmd

	m.mu.Lock()
	m.jobs[j.id] = j
	m.mu.Unlock()

	go func() {
		defer devNull.Close()
		waitAndFinalize(j, cmd)
	}()

	return j, nil
}

func waitAndFinalize(j *job, cmd *exec.Cmd) {
	waitErr := cmd.Wait()
	j.mu.Lock()
	j.state = stateExited
	j.finishedAt = time.Now()
	j.exitCode = exitCodeFromErr(waitErr)
	j.mu.Unlock()
	close(j.done)
}

// Get returns the job by id.
func (m *Manager) Get(id string) (*job, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	return j, ok
}

// Kill signals the job's process group with SIGTERM, then SIGKILL after
// killGrace. It blocks until the child has exited and returns
// (killed=true) only when this call is the one that requested the
// signal; a job that was already exited returns (killed=false).
func (m *Manager) Kill(j *job) (bool, error) {
	if runtime.GOOS == "windows" {
		return false, fmt.Errorf("job: not supported on windows in V1")
	}
	j.mu.Lock()
	if j.state == stateExited {
		j.mu.Unlock()
		return false, nil
	}
	pid := j.cmd.Process.Pid
	j.killOnce.Do(func() {
		j.killed = true
		j.killReason = "killed by job_kill"
		_ = procgroup.Kill(pid, syscall.SIGTERM)
		go func() {
			time.Sleep(killGrace)
			_ = procgroup.Kill(pid, syscall.SIGKILL)
		}()
	})
	j.mu.Unlock()

	<-j.done
	return true, nil
}

func exitCodeFromErr(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}
