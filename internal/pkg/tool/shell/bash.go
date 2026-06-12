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

	"github.com/feimingxliu/ub/internal/pkg/tool"
	"github.com/feimingxliu/ub/internal/pkg/tool/procgroup"
)

type bashArgs struct {
	Command   string      `json:"command"              jsonschema:"required,description=Shell command, executed via the system shell (/bin/sh -c on Unix, cmd /C on Windows)."`
	Cwd       string      `json:"cwd,omitempty"        jsonschema:"description=Working directory, relative to workspace root. Defaults to '.'."`
	TimeoutMs tool.IntArg `json:"timeout_ms,omitempty" jsonschema:"description=Timeout in milliseconds. Defaults to 120000. Must be non-negative."`
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
	return "Run a shell command via the system shell (/bin/sh -c on Unix, cmd /C on Windows) from the workspace (or explicit cwd). Use for builds, tests, git/status inspection, and repo commands that need a real process. For file search/read/list/edit, prefer the dedicated grep/read/ls/glob/edit/multiedit tools over shell grep/cat/ls/find/sed/python/perl; if edit reports old string not found, re-read a narrow range and retry edit/multiedit with exact whitespace. Prefer cwd over `cd ... && ...`; set a timeout for long commands. Treat non-zero exit_code, timeout, aborted, stdout, and stderr as evidence, and never report a check as passed unless this tool actually ran and exit_code=0."
}
func (t *bashTool) Schema() *jsonschema.Schema { return t.schema }
func (t *bashTool) Risk() tool.Risk            { return tool.RiskExec }

// ExecuteStream runs the same command Execute would but pushes stdout /
// stderr chunks into events as they arrive, so the TUI can render running
// output. The final Result is identical to Execute's (same metadata block,
// same trailing stdout/stderr sections).
func (t *bashTool) ExecuteStream(ctx context.Context, raw json.RawMessage, events chan<- tool.StreamEvent) (tool.Result, error) {
	return t.run(ctx, raw, events)
}

func (t *bashTool) Execute(ctx context.Context, raw json.RawMessage) (tool.Result, error) {
	return t.run(ctx, raw, nil)
}

func (t *bashTool) run(ctx context.Context, raw json.RawMessage, events chan<- tool.StreamEvent) (tool.Result, error) {
	var a bashArgs
	if err := tool.DecodeArgs("bash", raw, &a); err != nil {
		return tool.Result{}, err
	}
	if a.Command == "" {
		return tool.Result{}, fmt.Errorf("bash: command is required")
	}
	if int(a.TimeoutMs) < 0 {
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
	if int(a.TimeoutMs) > 0 {
		timeout = time.Duration(int(a.TimeoutMs)) * time.Millisecond
	}

	devNull, err := os.Open(os.DevNull)
	if err != nil {
		return tool.Result{}, fmt.Errorf("bash: open /dev/null: %w", err)
	}
	defer devNull.Close()

	stdout := newCapWriter(streamCap)
	stderr := newCapWriter(streamCap)

	cmd := shellCommand(a.Command)
	cmd.Dir = absCwd
	cmd.Stdin = devNull
	if events != nil {
		cmd.Stdout = &streamingWriter{cap: stdout, events: events, kind: tool.StreamStdout}
		cmd.Stderr = &streamingWriter{cap: stderr, events: events, kind: tool.StreamStderr}
	} else {
		cmd.Stdout = stdout
		cmd.Stderr = stderr
	}
	procgroup.Set(cmd)

	start := time.Now()
	if err := cmd.Start(); err != nil {
		return tool.Result{
			Content: assembleContent(-1, time.Since(start), stdout, stderr, fmt.Sprintf("start failed: %v", err), false, false),
			IsError: true,
		}, nil
	}

	pid := cmd.Process.Pid
	var killOnce sync.Once
	var killReason string
	killGroup := func(reason string) {
		killOnce.Do(func() {
			killReason = reason
			if runtime.GOOS == "windows" {
				_ = exec.Command("taskkill", "/T", "/F", "/PID", fmt.Sprint(pid)).Run()
				_ = cmd.Process.Kill()
			} else {
				_ = procgroup.Kill(pid, syscall.SIGTERM)
				go func() {
					time.Sleep(2 * time.Second)
					_ = procgroup.Kill(pid, syscall.SIGKILL)
				}()
			}
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
	timedOut := false
	aborted := false
	if killReason != "" {
		isError = true
		switch {
		case strings.HasPrefix(killReason, "timeout"):
			timedOut = true
		case strings.HasPrefix(killReason, "cancelled"):
			aborted = true
		default:
			errorLine = killReason
		}
	}

	return tool.Result{
		Content: assembleContent(exitCode, duration, stdout, stderr, errorLine, timedOut, aborted),
		IsError: isError,
	}, nil
}

// streamingWriter fans bytes into both a cap-bound buffer (kept by the
// bash tool for its final Result.Content) and an events channel (one
// StreamEvent per Write call). The events chan is non-blocking via the
// agent's buffered chan; if the chan is closed or unbuffered, drops are
// silent — partial output is best-effort and never gates the underlying
// command's progress.
type streamingWriter struct {
	cap    *capWriter
	events chan<- tool.StreamEvent
	kind   tool.StreamEventKind
}

func (w *streamingWriter) Write(p []byte) (int, error) {
	n, err := w.cap.Write(p)
	// Always emit the chunk we received (not the truncated cap buffer) so
	// the TUI sees real output even if the cap is already exhausted.
	defer func() {
		// Channel send is wrapped in recover-on-closed via select-default;
		// a closed events chan would panic on send.
		select {
		case w.events <- tool.StreamEvent{Kind: w.kind, Data: string(p)}:
		default:
		}
	}()
	return n, err
}

// assembleContent formats Result.Content with the stable header/body layout
// defined in the bash-tool spec. metadata lives inside a <shell_metadata>
// block so model prompts can locate it deterministically; timeout / aborted
// are surfaced as explicit boolean flags instead of being inferred from the
// `error=` text.
func assembleContent(exitCode int, duration time.Duration, stdout, stderr *capWriter, errorLine string, timedOut, aborted bool) string {
	var b strings.Builder
	b.WriteString("<shell_metadata>\n")
	fmt.Fprintf(&b, "exit_code=%d\n", exitCode)
	fmt.Fprintf(&b, "duration_ms=%d\n", duration.Milliseconds())
	if timedOut {
		b.WriteString("timeout=true\n")
	}
	if aborted {
		b.WriteString("aborted=true\n")
	}
	if errorLine != "" {
		fmt.Fprintf(&b, "error=%s\n", errorLine)
	}
	b.WriteString("</shell_metadata>\n")
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

// shellCommand returns the platform-appropriate command to execute a shell
// one-liner: /bin/sh -c on Unix, cmd /C on Windows.
func shellCommand(command string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.Command("cmd", "/C", command)
	}
	return exec.Command("/bin/sh", "-c", command)
}
