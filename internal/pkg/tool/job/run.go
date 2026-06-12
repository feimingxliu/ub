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

type runArgs struct {
	Command string `json:"command"      jsonschema:"required,description=Shell command, executed through the platform shell."`
	Cwd     string `json:"cwd,omitempty" jsonschema:"description=Working directory, relative to workspace root. Defaults to '.'."`
}

type runTool struct {
	mgr    *Manager
	schema *jsonschema.Schema
}

func newRunTool(mgr *Manager) *runTool {
	return &runTool{
		mgr:    mgr,
		schema: jsonschema.Reflect(&runArgs{}),
	}
}

func (t *runTool) Name() string { return "job_run" }
func (t *runTool) Description() string {
	return "Start a background shell job and return its job_id. Output is captured into a 32KB ring buffer per stream."
}
func (t *runTool) Schema() *jsonschema.Schema { return t.schema }
func (t *runTool) Risk() tool.Risk            { return tool.RiskExec }

func (t *runTool) Execute(_ context.Context, raw json.RawMessage) (tool.Result, error) {
	var a runArgs
	if err := tool.DecodeArgs("job_run", raw, &a); err != nil {
		return tool.Result{}, err
	}
	if a.Command == "" {
		return tool.Result{}, fmt.Errorf("job_run: command is required")
	}
	j, err := t.mgr.Start(a.Cwd, a.Command)
	if err != nil {
		return tool.Result{}, err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "job_id=%s\n", j.id)
	fmt.Fprintf(&b, "started_at=%s", j.startedAt.UTC().Format(time.RFC3339Nano))
	return tool.Result{Content: b.String()}, nil
}
