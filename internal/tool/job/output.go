package job

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/invopop/jsonschema"

	"github.com/feimingxliu/ub/internal/tool"
)

type outputArgs struct {
	JobID string `json:"job_id"        jsonschema:"required,description=Job identifier returned by job_run."`
	Tail  int    `json:"tail,omitempty" jsonschema:"description=Maximum bytes to return per stream. 0 or omitted returns the full 32KB ring buffer."`
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
	return "Read the most recent stdout/stderr from a background job along with its current state."
}
func (t *outputTool) Schema() *jsonschema.Schema { return t.schema }
func (t *outputTool) Risk() tool.Risk            { return tool.RiskSafe }

func (t *outputTool) Execute(_ context.Context, raw json.RawMessage) (tool.Result, error) {
	var a outputArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return tool.Result{}, fmt.Errorf("job_output: invalid args: %w", err)
	}
	if a.JobID == "" {
		return tool.Result{}, fmt.Errorf("job_output: job_id is required")
	}
	j, ok := t.mgr.Get(a.JobID)
	if !ok {
		return tool.Result{}, fmt.Errorf("job_output: job not found: %s", a.JobID)
	}

	j.mu.Lock()
	state := j.state
	exitCode := j.exitCode
	stdoutTotal := j.stdout.Total()
	stderrTotal := j.stderr.Total()
	stdoutSnap := j.stdout.Snapshot(a.Tail)
	stderrSnap := j.stderr.Snapshot(a.Tail)
	j.mu.Unlock()

	var b strings.Builder
	fmt.Fprintf(&b, "job_id=%s\n", j.id)
	fmt.Fprintf(&b, "state=%s\n", state)
	fmt.Fprintf(&b, "exit_code=%d\n", exitCode)
	fmt.Fprintf(&b, "stdout_total=%d\n", stdoutTotal)
	fmt.Fprintf(&b, "stderr_total=%d\n", stderrTotal)
	b.WriteString("--- stdout ---\n")
	b.Write(stdoutSnap)
	b.WriteString("\n--- stderr ---\n")
	b.Write(stderrSnap)
	return tool.Result{Content: b.String()}, nil
}
