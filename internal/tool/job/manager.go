package job

import (
	"context"
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

	mu       sync.Mutex
	jobs     map[string]*job
	starting int

	maxConcurrent   int
	retention       time.Duration
	cleanupInterval time.Duration
	now             func() time.Time

	stopCleanup chan struct{}
	cleanupDone chan struct{}
}

// ManagerOptions controls background job lifecycle behavior.
type ManagerOptions struct {
	MaxConcurrent   int
	Retention       time.Duration
	CleanupInterval time.Duration
	Now             func() time.Time
}

const (
	defaultMaxConcurrent   = 50
	defaultRetention       = 8 * time.Hour
	defaultCleanupInterval = 5 * time.Minute
)

// NewManager constructs a Manager bound to the given (already-cleaned)
// workspace root.
func NewManager(root string) *Manager {
	return NewManagerWithOptions(root, ManagerOptions{})
}

// NewManagerWithOptions constructs a Manager with lifecycle limits.
func NewManagerWithOptions(root string, opts ManagerOptions) *Manager {
	if opts.MaxConcurrent <= 0 {
		opts.MaxConcurrent = defaultMaxConcurrent
	}
	if opts.Retention <= 0 {
		opts.Retention = defaultRetention
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	m := &Manager{
		root:            root,
		jobs:            map[string]*job{},
		maxConcurrent:   opts.MaxConcurrent,
		retention:       opts.Retention,
		cleanupInterval: opts.CleanupInterval,
		now:             now,
	}
	if opts.CleanupInterval > 0 {
		m.stopCleanup = make(chan struct{})
		m.cleanupDone = make(chan struct{})
		go m.cleanupLoop(opts.CleanupInterval)
	}
	return m
}

type job struct {
	id        string
	command   string
	startedAt time.Time

	cmd *exec.Cmd

	mu          sync.Mutex
	state       jobState
	exitCode    int
	completedAt time.Time
	stdout      *ring
	stderr      *ring
	killOnce    sync.Once
	killed      bool
	killReason  string
	done        chan struct{}
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
	if err := m.reserveSlot(); err != nil {
		return nil, err
	}
	releaseSlot := true
	defer func() {
		if releaseSlot {
			m.releaseReservedSlot()
		}
	}()

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

	cmd := shellCommand(command)
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
	m.starting--
	m.jobs[j.id] = j
	m.mu.Unlock()
	releaseSlot = false

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
	j.completedAt = time.Now()
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
	return m.kill(j, "killed by job_kill")
}

func (m *Manager) kill(j *job, reason string) (bool, error) {
	j.mu.Lock()
	if j.state == stateExited {
		j.mu.Unlock()
		return false, nil
	}
	process := j.cmd.Process
	pid := process.Pid
	j.killOnce.Do(func() {
		j.killed = true
		j.killReason = reason
		if runtime.GOOS == "windows" {
			_ = process.Kill()
		} else {
			_ = procgroup.Kill(pid, syscall.SIGTERM)
			go func() {
				time.Sleep(killGrace)
				_ = procgroup.Kill(pid, syscall.SIGKILL)
			}()
		}
	})
	j.mu.Unlock()

	<-j.done
	return true, nil
}

// Shutdown terminates all running jobs and stops background cleanup.
func (m *Manager) Shutdown(ctx context.Context) error {
	if m == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	m.stopCleanupLoop()

	m.mu.Lock()
	jobs := make([]*job, 0, len(m.jobs))
	for _, j := range m.jobs {
		j.mu.Lock()
		running := j.state == stateRunning
		j.mu.Unlock()
		if running {
			jobs = append(jobs, j)
		}
	}
	m.mu.Unlock()

	var firstErr error
	for _, j := range jobs {
		done := make(chan error, 1)
		go func(job *job) {
			_, err := m.kill(job, "killed by manager shutdown")
			done <- err
		}(j)
		select {
		case err := <-done:
			if err != nil && firstErr == nil {
				firstErr = err
			}
		case <-ctx.Done():
			if firstErr == nil {
				firstErr = ctx.Err()
			}
		}
	}
	return firstErr
}

// PruneCompleted removes completed jobs older than the configured retention.
func (m *Manager) PruneCompleted(now time.Time) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pruneCompletedLocked(now)
}

func (m *Manager) reserveSlot() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneCompletedLocked(m.now())
	if len(m.jobs)+m.starting >= m.maxConcurrent {
		return fmt.Errorf("job: maximum concurrent jobs reached (%d)", m.maxConcurrent)
	}
	m.starting++
	return nil
}

func (m *Manager) releaseReservedSlot() {
	m.mu.Lock()
	m.starting--
	m.mu.Unlock()
}

func (m *Manager) pruneCompletedLocked(now time.Time) int {
	if m.retention <= 0 {
		return 0
	}
	deleted := 0
	cutoff := now.Add(-m.retention)
	for id, j := range m.jobs {
		j.mu.Lock()
		expired := j.state == stateExited && !j.completedAt.IsZero() && j.completedAt.Before(cutoff)
		j.mu.Unlock()
		if expired {
			delete(m.jobs, id)
			deleted++
		}
	}
	return deleted
}

func (m *Manager) cleanupLoop(interval time.Duration) {
	defer close(m.cleanupDone)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			m.PruneCompleted(m.now())
		case <-m.stopCleanup:
			return
		}
	}
}

func (m *Manager) stopCleanupLoop() {
	if m.stopCleanup == nil {
		return
	}
	select {
	case <-m.cleanupDone:
		return
	default:
	}
	close(m.stopCleanup)
	<-m.cleanupDone
	m.stopCleanup = nil
	m.cleanupDone = nil
}

func shellCommand(command string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.Command("cmd", "/C", command)
	}
	return exec.Command("/bin/sh", "-c", command)
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
