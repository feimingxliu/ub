package eval

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const defaultTimeout = 10 * time.Minute

type RunOptions struct {
	Provider      string
	Model         string
	Timeout       time.Duration
	KeepWorkspace bool
	Executable    string
	Executor      ProcessExecutor
}

type ProcessRequest struct {
	Executable string
	Args       []string
	Dir        string
	Env        []string
}

type ProcessResult struct {
	Stdout string
	Stderr string
}

type ProcessExecutor interface {
	Run(context.Context, ProcessRequest) (ProcessResult, error)
}

type OSProcessExecutor struct{}

func (OSProcessExecutor) Run(ctx context.Context, request ProcessRequest) (ProcessResult, error) {
	cmd := exec.CommandContext(ctx, request.Executable, request.Args...)
	cmd.Dir = request.Dir
	cmd.Env = request.Env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return ProcessResult{Stdout: stdout.String(), Stderr: stderr.String()}, err
}

func Run(ctx context.Context, taskFile TaskFile, opts RunOptions) (Report, error) {
	report := Report{
		Task:     taskFile.Task.Name,
		Provider: opts.Provider,
		Model:    opts.Model,
		Metrics: Metrics{
			ToolCalls:        []string{},
			ContextDecisions: []ContextDecision{},
		},
		Assertions: []AssertionResult{},
	}
	root, err := os.MkdirTemp("", "ub-eval-")
	if err != nil {
		return report, err
	}
	workspace := filepath.Join(root, "workspace")
	stateHome := filepath.Join(root, "state")
	dataHome := filepath.Join(root, "data")
	for _, dir := range []string{workspace, filepath.Join(stateHome, "ub", "tool_outputs"), dataHome} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			_ = os.RemoveAll(root)
			return report, err
		}
	}
	keep := opts.KeepWorkspace
	if keep {
		report.Workspace = workspace
	}
	defer func() {
		if !keep {
			_ = os.RemoveAll(root)
		}
	}()
	if err := PrepareFixture(taskFile, workspace); err != nil {
		report.FailureCategory = FailureTask
		report.Failure = err.Error()
		return report, err
	}

	timeout, err := runTimeout(taskFile.Task, opts.Timeout)
	if err != nil {
		report.FailureCategory = FailureTask
		report.Failure = err.Error()
		return report, err
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	executable := opts.Executable
	if executable == "" {
		executable, err = os.Executable()
		if err != nil {
			return report, err
		}
	}
	executor := opts.Executor
	if executor == nil {
		executor = OSProcessExecutor{}
	}
	env := withEnv(os.Environ(), "XDG_STATE_HOME", stateHome)
	env = withEnv(env, "XDG_DATA_HOME", dataHome)
	env = withEnv(env, "UB_EVAL", "1")
	started := time.Now()
	prompts := append([]string{taskFile.Task.Prompt}, taskFile.Task.Followups...)
	var processResult ProcessResult
	var processErr error
	var observationErr error
	var sessionID string
	for _, prompt := range prompts {
		args := []string{"--mode", "full-access", "run", "--prompt", prompt}
		if opts.Provider != "" {
			args = append(args, "--provider", opts.Provider)
		}
		if opts.Model != "" {
			args = append(args, "--model", opts.Model)
		}
		if sessionID != "" {
			args = append(args, "--session", sessionID)
		}
		current, currentErr := executor.Run(runCtx, ProcessRequest{
			Executable: executable,
			Args:       args,
			Dir:        workspace,
			Env:        env,
		})
		processResult.Stdout += current.Stdout
		processResult.Stderr += current.Stderr
		if currentErr != nil {
			processErr = currentErr
			break
		}
		observation, observeErr := Observe(runCtx, dataHome, workspace)
		if observeErr != nil {
			observationErr = observeErr
			break
		}
		sessionID = observation.SessionID
	}
	report.Metrics.Duration = time.Since(started)
	report.Metrics.DurationMillis = report.Metrics.Duration.Milliseconds()
	if observationErr != nil {
		report.AgentStderr = summarize(processResult.Stderr)
		report.FailureCategory = FailureInternal
		report.Failure = observationErr.Error()
		return report, observationErr
	}
	if processErr != nil {
		report.AgentStderr = summarize(processResult.Stderr)
		report.FailureCategory = FailureAgent
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			report.Failure = fmt.Sprintf("agent timed out after %s", timeout)
		} else {
			report.Failure = fmt.Sprintf("agent failed: %v", processErr)
		}
		return report, fmt.Errorf("%s", report.Failure)
	}

	observation, err := Observe(ctx, dataHome, workspace)
	if err != nil {
		report.FailureCategory = FailureInternal
		report.Failure = err.Error()
		return report, err
	}
	report.SessionID = observation.SessionID
	if report.Provider == "" {
		report.Provider = observation.Provider
	}
	if report.Model == "" {
		report.Model = observation.Model
	}
	report.Metrics = observation.Metrics
	report.Metrics.Duration = time.Since(started)
	report.Metrics.DurationMillis = report.Metrics.Duration.Milliseconds()
	results, err := Evaluate(ctx, taskFile.Task, workspace, env, observation)
	if err != nil {
		report.FailureCategory = FailureInternal
		report.Failure = err.Error()
		return report, err
	}
	report.Assertions = results
	report.Passed = true
	for _, result := range results {
		report.Passed = report.Passed && result.Passed
	}
	if !report.Passed {
		report.FailureCategory = FailureAssertion
		report.Failure = "one or more assertions failed"
		return report, errors.New(report.Failure)
	}
	return report, nil
}

func runTimeout(task Task, override time.Duration) (time.Duration, error) {
	if override < 0 {
		return 0, fmt.Errorf("timeout override must be positive")
	}
	if override > 0 {
		return override, nil
	}
	if task.Timeout == "" {
		return defaultTimeout, nil
	}
	duration, err := time.ParseDuration(task.Timeout)
	if err != nil || duration <= 0 {
		return 0, fmt.Errorf("invalid timeout %q", task.Timeout)
	}
	return duration, nil
}

func withEnv(env []string, key, value string) []string {
	prefix := key + "="
	filtered := slices.DeleteFunc(slices.Clone(env), func(entry string) bool {
		return strings.HasPrefix(entry, prefix)
	})
	return append(filtered, prefix+value)
}
