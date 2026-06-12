package job

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/invopop/jsonschema"

	"github.com/feimingxliu/ub/internal/pkg/tool"
)

type outputArgs struct {
	JobID     string       `json:"job_id"              jsonschema:"required,description=Job identifier returned by job_run."`
	Tail      tool.IntArg  `json:"tail,omitempty"      jsonschema:"description=Maximum bytes to return per stream. 0 or omitted returns the full 32KB ring buffer."`
	Follow    tool.BoolArg `json:"follow,omitempty"    jsonschema:"description=When true, stream current and new job output until the job exits, timeout_ms expires, or the request is cancelled."`
	TimeoutMs tool.IntArg  `json:"timeout_ms,omitempty" jsonschema:"description=Maximum milliseconds to follow a running job. Defaults to 120000 when follow is true. Must be non-negative."`
}

type outputTool struct {
	mgr    *Manager
	schema *jsonschema.Schema
}

func newOutputTool(mgr *Manager) *outputTool {
	return &outputTool{
		mgr:    mgr,
		schema: jsonschema.Reflect(&outputArgs{}),
	}
}

func (t *outputTool) Name() string { return "job_output" }
func (t *outputTool) Description() string {
	return "Read the most recent stdout/stderr from a background job along with its current state. Set follow=true to stream output until the job exits, timeout_ms expires, or the request is cancelled."
}
func (t *outputTool) Schema() *jsonschema.Schema { return t.schema }
func (t *outputTool) Risk() tool.Risk            { return tool.RiskSafe }

func (t *outputTool) Execute(ctx context.Context, raw json.RawMessage) (tool.Result, error) {
	return t.run(ctx, raw, nil)
}

func (t *outputTool) ExecuteStream(ctx context.Context, raw json.RawMessage, events chan<- tool.StreamEvent) (tool.Result, error) {
	return t.run(ctx, raw, events)
}

func (t *outputTool) run(ctx context.Context, raw json.RawMessage, events chan<- tool.StreamEvent) (tool.Result, error) {
	var a outputArgs
	if err := tool.DecodeArgs("job_output", raw, &a); err != nil {
		return tool.Result{}, err
	}
	if a.JobID == "" {
		return tool.Result{}, fmt.Errorf("job_output: job_id is required")
	}
	if int(a.TimeoutMs) < 0 {
		return tool.Result{}, fmt.Errorf("job_output: timeout_ms must be non-negative")
	}
	j, ok := t.mgr.Get(a.JobID)
	if !ok {
		return tool.Result{}, fmt.Errorf("job_output: job not found: %s", a.JobID)
	}

	if bool(a.Follow) {
		return followJobOutput(ctx, j, int(a.Tail), followTimeout(a.TimeoutMs), events), nil
	}
	return snapshotJobOutput(j, int(a.Tail), followOutcome{}), nil
}

const (
	defaultFollowTimeout = 120 * time.Second
	followPollInterval   = 50 * time.Millisecond
)

type followOutcome struct {
	TimedOut bool
	Aborted  bool
	Error    string
}

func followTimeout(timeoutMs tool.IntArg) time.Duration {
	if int(timeoutMs) > 0 {
		return time.Duration(int(timeoutMs)) * time.Millisecond
	}
	return defaultFollowTimeout
}

func followJobOutput(ctx context.Context, j *job, tail int, timeout time.Duration, events chan<- tool.StreamEvent) tool.Result {
	stdoutSeen, stderrSeen := emitInitialJobOutput(j, tail, events)

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	ticker := time.NewTicker(followPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return snapshotJobOutput(j, tail, followOutcome{Aborted: true, Error: ctx.Err().Error()})
		case <-timer.C:
			emitJobOutputDeltas(j, &stdoutSeen, &stderrSeen, events)
			return snapshotJobOutput(j, tail, followOutcome{TimedOut: true})
		case <-j.done:
			emitJobOutputDeltas(j, &stdoutSeen, &stderrSeen, events)
			return snapshotJobOutput(j, tail, followOutcome{})
		case <-ticker.C:
			emitJobOutputDeltas(j, &stdoutSeen, &stderrSeen, events)
		}
	}
}

func emitInitialJobOutput(j *job, tail int, events chan<- tool.StreamEvent) (int, int) {
	j.mu.Lock()
	stdoutTotal := j.stdout.Total()
	stderrTotal := j.stderr.Total()
	stdoutSnap := j.stdout.Snapshot(tail)
	stderrSnap := j.stderr.Snapshot(tail)
	j.mu.Unlock()

	emitJobOutput(events, tool.StreamStdout, string(stdoutSnap))
	emitJobOutput(events, tool.StreamStderr, string(stderrSnap))
	return stdoutTotal, stderrTotal
}

func emitJobOutputDeltas(j *job, stdoutSeen, stderrSeen *int, events chan<- tool.StreamEvent) {
	j.mu.Lock()
	stdoutTotal := j.stdout.Total()
	stderrTotal := j.stderr.Total()
	stdoutDelta := jobOutputDelta(j.stdout, stdoutTotal-*stdoutSeen)
	stderrDelta := jobOutputDelta(j.stderr, stderrTotal-*stderrSeen)
	j.mu.Unlock()

	emitJobOutput(events, tool.StreamStdout, stdoutDelta)
	emitJobOutput(events, tool.StreamStderr, stderrDelta)
	*stdoutSeen = stdoutTotal
	*stderrSeen = stderrTotal
}

func jobOutputDelta(r *ring, delta int) string {
	if delta <= 0 {
		return ""
	}
	snap := r.Snapshot(delta)
	if len(snap) == 0 {
		return ""
	}
	if delta > len(snap) {
		return "[earlier job output truncated]\n" + string(snap)
	}
	return string(snap)
}

func emitJobOutput(events chan<- tool.StreamEvent, kind tool.StreamEventKind, data string) {
	if events == nil || data == "" {
		return
	}
	select {
	case events <- tool.StreamEvent{Kind: kind, Data: data}:
	default:
	}
}

func snapshotJobOutput(j *job, tail int, outcome followOutcome) tool.Result {
	j.mu.Lock()
	state := j.state
	exitCode := j.exitCode
	stdoutTotal := j.stdout.Total()
	stderrTotal := j.stderr.Total()
	stdoutSnap := j.stdout.Snapshot(tail)
	stderrSnap := j.stderr.Snapshot(tail)
	j.mu.Unlock()

	var b strings.Builder
	fmt.Fprintf(&b, "job_id=%s\n", j.id)
	fmt.Fprintf(&b, "state=%s\n", state)
	fmt.Fprintf(&b, "exit_code=%d\n", exitCode)
	if outcome.TimedOut {
		b.WriteString("follow_timeout=true\n")
	}
	if outcome.Aborted {
		b.WriteString("follow_aborted=true\n")
	}
	if outcome.Error != "" {
		fmt.Fprintf(&b, "error=%s\n", outcome.Error)
	}
	fmt.Fprintf(&b, "stdout_total=%d\n", stdoutTotal)
	fmt.Fprintf(&b, "stderr_total=%d\n", stderrTotal)
	b.WriteString("--- stdout ---\n")
	b.Write(stdoutSnap)
	b.WriteString("\n--- stderr ---\n")
	b.Write(stderrSnap)
	return tool.Result{Content: b.String(), IsError: outcome.Aborted}
}
