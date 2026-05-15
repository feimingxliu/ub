package job

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"

	"github.com/invopop/jsonschema"

	"github.com/feimingxliu/ub/internal/tool"
)

type killArgs struct {
	JobID string `json:"job_id" jsonschema:"required,description=Job identifier returned by job_run."`
}

type killTool struct {
	mgr    *Manager
	schema *jsonschema.Schema
}

func newKillTool(mgr *Manager) *killTool {
	return &killTool{
		mgr:    mgr,
		schema: jsonschema.Reflect(&killArgs{}),
	}
}

func (t *killTool) Name() string { return "job_kill" }
func (t *killTool) Description() string {
	return "Terminate a background job. Sends SIGTERM to the process group, then SIGKILL after 2 seconds."
}
func (t *killTool) Schema() *jsonschema.Schema { return t.schema }
func (t *killTool) Risk() tool.Risk            { return tool.RiskExec }

func (t *killTool) Execute(_ context.Context, raw json.RawMessage) (tool.Result, error) {
	if runtime.GOOS == "windows" {
		return tool.Result{}, fmt.Errorf("job_kill: not supported on windows in V1")
	}
	var a killArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return tool.Result{}, fmt.Errorf("job_kill: invalid args: %w", err)
	}
	if a.JobID == "" {
		return tool.Result{}, fmt.Errorf("job_kill: job_id is required")
	}
	j, ok := t.mgr.Get(a.JobID)
	if !ok {
		return tool.Result{}, fmt.Errorf("job_kill: job not found: %s", a.JobID)
	}
	killed, err := t.mgr.Kill(j)
	if err != nil {
		return tool.Result{}, err
	}

	j.mu.Lock()
	exitCode := j.exitCode
	j.mu.Unlock()

	var b strings.Builder
	fmt.Fprintf(&b, "job_id=%s\n", j.id)
	fmt.Fprintf(&b, "state=exited\n")
	fmt.Fprintf(&b, "exit_code=%d\n", exitCode)
	fmt.Fprintf(&b, "killed=%t", killed)
	return tool.Result{Content: b.String()}, nil
}
