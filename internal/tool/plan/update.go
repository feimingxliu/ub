package plan

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/invopop/jsonschema"

	"github.com/feimingxliu/ub/internal/tool"
)

type updateArgs struct {
	PlanID    string      `json:"plan_id"   jsonschema:"required,description=ID returned by plan_write (the basename of the plan file without .md)."`
	StepIndex tool.IntArg `json:"step_index" jsonschema:"required,description=1-based step number to update."`
	Status    string      `json:"status"    jsonschema:"required,enum=in_progress,enum=done,enum=skipped,enum=failed,enum=pending,description=New step status."`
	Note      string      `json:"note,omitempty" jsonschema:"description=Optional message appended to the Log entry for this update."`
}

type updateTool struct {
	workspace string
	schema    *jsonschema.Schema
}

func newUpdateTool(workspace string) *updateTool {
	return &updateTool{
		workspace: workspace,
		schema:    jsonschema.Reflect(&updateArgs{}),
	}
}

func (t *updateTool) Name() string { return "plan_update_step" }
func (t *updateTool) Description() string {
	return "Update one step in a plan: mark it in_progress / done / skipped / failed / pending, append a log entry, and auto-transition the plan to complete when every step has a terminal status."
}
func (t *updateTool) Schema() *jsonschema.Schema { return t.schema }
func (t *updateTool) Risk() tool.Risk            { return tool.RiskSafe }

func (t *updateTool) Execute(_ context.Context, raw json.RawMessage) (tool.Result, error) {
	var a updateArgs
	if err := tool.UnmarshalArgs(raw, &a); err != nil {
		return tool.Result{}, fmt.Errorf("plan_update_step: invalid args: %w", err)
	}
	if strings.TrimSpace(a.PlanID) == "" {
		return tool.Result{}, fmt.Errorf("plan_update_step: plan_id is required")
	}
	marker, err := statusMarker(a.Status)
	if err != nil {
		return tool.Result{}, fmt.Errorf("plan_update_step: %w", err)
	}

	path := planPath(t.workspace, a.PlanID)
	doc, err := loadPlan(path)
	if err != nil {
		return tool.Result{}, fmt.Errorf("plan_update_step: %w", err)
	}
	stepIndex := int(a.StepIndex)
	if stepIndex < 1 || stepIndex > len(doc.steps) {
		return tool.Result{}, fmt.Errorf("plan_update_step: step_index %d out of range [1, %d]", stepIndex, len(doc.steps))
	}
	doc.steps[stepIndex-1].marker = marker

	logLine := fmt.Sprintf("- %s step %d → %s", nowFunc().Format("2006-01-02T15:04:05Z07:00"), stepIndex, strings.ToLower(strings.TrimSpace(a.Status)))
	if note := strings.TrimSpace(a.Note); note != "" {
		logLine += ": " + note
	}
	doc.log = append(doc.log, logLine)

	if allStepsFinished(doc.steps) {
		doc.status = statusComplete
	} else {
		doc.status = statusInProgress
	}

	if err := savePlan(path, doc); err != nil {
		return tool.Result{}, fmt.Errorf("plan_update_step: %w", err)
	}

	stepsRendered := renderStepsBlock(doc.steps)
	rel, _ := filepath.Rel(t.workspace, path)
	if rel == "" {
		rel = path
	}
	return tool.Result{
		Content: fmt.Sprintf("plan_id=%s\nstatus=%s\n\n%s", a.PlanID, doc.status, stepsRendered),
		Files:   []tool.FileChange{{Path: rel, Kind: tool.KindModify}},
	}, nil
}

func renderStepsBlock(steps []step) string {
	var b strings.Builder
	b.WriteString("## Steps\n\n")
	for _, s := range steps {
		fmt.Fprintf(&b, "- [%s] %d. %s\n", s.marker, s.index, s.text)
	}
	return b.String()
}
