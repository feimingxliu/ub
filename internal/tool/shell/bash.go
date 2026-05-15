package shell

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/invopop/jsonschema"

	"github.com/feimingxliu/ub/internal/tool"
)

type bashArgs struct {
	Command   string `json:"command"             jsonschema:"required,description=Shell command, executed via /bin/sh -c."`
	Cwd       string `json:"cwd,omitempty"       jsonschema:"description=Working directory, relative to workspace root. Defaults to '.'."`
	TimeoutMs int    `json:"timeout_ms,omitempty" jsonschema:"description=Timeout in milliseconds. Defaults to 120000. Must be non-negative."`
}

type bashTool struct {
	root   string
	schema *jsonschema.Schema
}

func newBashTool(root string) *bashTool {
	return &bashTool{
		root:   root,
		schema: jsonschema.Reflect(&bashArgs{}),
	}
}

func (t *bashTool) Name() string { return "bash" }
func (t *bashTool) Description() string {
	return "Run a shell command via /bin/sh -c. Returns exit code, duration and captured stdout/stderr (each capped at 32KB)."
}
func (t *bashTool) Schema() *jsonschema.Schema { return t.schema }
func (t *bashTool) Risk() tool.Risk            { return tool.RiskExec }

func (t *bashTool) Execute(ctx context.Context, raw json.RawMessage) (tool.Result, error) {
	if runtime.GOOS == "windows" {
		return tool.Result{}, fmt.Errorf("bash: not supported on windows in V1")
	}
	var a bashArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return tool.Result{}, fmt.Errorf("bash: invalid args: %w", err)
	}
	if a.Command == "" {
		return tool.Result{}, fmt.Errorf("bash: command is required")
	}
	if a.TimeoutMs < 0 {
		return tool.Result{}, fmt.Errorf("bash: timeout_ms must be non-negative")
	}

	cwd := a.Cwd
	if cwd == "" {
		cwd = "."
	}
	absCwd, err := tool.Resolve(t.root, cwd)
	if err != nil {
		return tool.Result{}, err
	}

	timeout := defaultTimeout
	if a.TimeoutMs > 0 {
		timeout = time.Duration(a.TimeoutMs) * time.Millisecond
	}

	devNull, err := os.Open(os.DevNull)
	if err != nil {
		return tool.Result{}, fmt.Errorf("bash: open /dev/null: %w", err)
	}
	defer devNull.Close()

	stdout := newCapWriter(streamCap)
	stderr := newCapWriter(streamCap)

	cmd := exec.Command("/bin/sh", "-c", a.Command)
	cmd.Dir = absCwd
	cmd.Stdin = devNull
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	setProcessGroup(cmd)

	start := time.Now()
	if err := cmd.Start(); err != nil {
		return tool.Result{
			Content: assembleContent(-1, time.Since(start), stdout, stderr, fmt.Sprintf("start failed: %v", err)),
			IsError: true,
		}, nil
	}

	pid := cmd.Process.Pid
	var killOnce sync.Once
	var killReason string
	killGroup := func(reason string) {
		killOnce.Do(func() {
			killReason = reason
			_ = killProcessGroup(pid, syscall.SIGTERM)
			go func() {
				time.Sleep(2 * time.Second)
				_ = killProcessGroup(pid, syscall.SIGKILL)
			}()
		})
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	var waitErr error
	select {
	case waitErr = <-done:
	case <-timer.C:
		killGroup(fmt.Sprintf("timeout after %s", timeout))
		waitErr = <-done
	case <-ctx.Done():
		killGroup(fmt.Sprintf("cancelled: %v", ctx.Err()))
		waitErr = <-done
	}
	duration := time.Since(start)

	exitCode := 0
	isError := false
	errorLine := ""

	if waitErr != nil {
		var ee *exec.ExitError
		switch {
		case errors.As(waitErr, &ee):
			exitCode = ee.ExitCode()
			isError = exitCode != 0
		default:
			exitCode = -1
			isError = true
			errorLine = waitErr.Error()
		}
	}
	if killReason != "" {
		isError = true
		errorLine = killReason
	}

	return tool.Result{
		Content: assembleContent(exitCode, duration, stdout, stderr, errorLine),
		IsError: isError,
	}, nil
}

// assembleContent formats Result.Content with the stable header/body
// layout defined in the bash-tool spec.
func assembleContent(exitCode int, duration time.Duration, stdout, stderr *capWriter, errorLine string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "exit_code=%d\n", exitCode)
	fmt.Fprintf(&b, "duration_ms=%d\n", duration.Milliseconds())
	if errorLine != "" {
		fmt.Fprintf(&b, "error=%s\n", errorLine)
	}
	b.WriteString("--- stdout ---\n")
	writeStream(&b, stdout)
	b.WriteString("\n--- stderr ---\n")
	writeStream(&b, stderr)
	return b.String()
}

func writeStream(b *strings.Builder, w *capWriter) {
	b.Write(w.Bytes())
	if w.Total() > len(w.Bytes()) {
		fmt.Fprintf(b, "\n... (truncated, total %d bytes)", w.Total())
	}
}
