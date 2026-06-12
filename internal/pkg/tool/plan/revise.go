package plan

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/invopop/jsonschema"

	"github.com/feimingxliu/ub/internal/pkg/tool"
)

type reviseArgs struct {
	PlanID string   `json:"plan_id" jsonschema:"required,description=ID returned by plan_write (the basename of the plan file without .md)."`
	Title  string   `json:"title,omitempty" jsonschema:"description=Optional replacement plan title. Omit to keep the existing title."`
	Steps  []string `json:"steps,omitempty" jsonschema:"description=Optional replacement ordered step list. Omit to keep existing steps. Provide at least one non-empty step when updating steps."`
	Notes  string   `json:"notes,omitempty" jsonschema:"description=Optional replacement notes text. Use an empty string to clear notes."`
	Reason string   `json:"reason,omitempty" jsonschema:"description=Short reason appended to the plan log, e.g. user correction or refined scope."`

	titleSet bool
	stepsSet bool
	notesSet bool
}

func (a *reviseArgs) UnmarshalJSON(raw []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	if v, ok := fields["plan_id"]; ok {
		if err := json.Unmarshal(v, &a.PlanID); err != nil {
			return fmt.Errorf("plan_id must be a string: %w", err)
		}
	}
	if v, ok := fields["title"]; ok {
		a.titleSet = true
		if string(v) != "null" {
			if err := json.Unmarshal(v, &a.Title); err != nil {
				return fmt.Errorf("title must be a string: %w", err)
			}
		}
	}
	if v, ok := fields["steps"]; ok {
		a.stepsSet = true
		if string(v) != "null" {
			steps, err := parseWriteSteps(v)
			if err != nil {
				return err
			}
			a.Steps = steps
		}
	}
	if v, ok := fields["notes"]; ok {
		a.notesSet = true
		if string(v) != "null" {
			if err := json.Unmarshal(v, &a.Notes); err != nil {
				return fmt.Errorf("notes must be a string: %w", err)
			}
		}
	}
	if v, ok := fields["reason"]; ok {
		if err := json.Unmarshal(v, &a.Reason); err != nil {
			return fmt.Errorf("reason must be a string: %w", err)
		}
	}
	return nil
}

type reviseTool struct {
	workspace string
	schema    *jsonschema.Schema
}

func newReviseTool(workspace string) *reviseTool {
	return &reviseTool{
		workspace: workspace,
		schema:    jsonschema.Reflect(&reviseArgs{}),
	}
}

func (t *reviseTool) Name() string { return "plan_update" }
func (t *reviseTool) Description() string {
	return "Revise an existing plan in place. Available only in plan mode. Use this instead of plan_write when the user corrects, narrows, expands, or asks to change a plan that already has a plan_id. Can replace title, steps, and/or notes while preserving the original plan file path and appending a log entry."
}
func (t *reviseTool) Schema() *jsonschema.Schema { return t.schema }
func (t *reviseTool) Risk() tool.Risk            { return tool.RiskSafe }

func (t *reviseTool) Execute(_ context.Context, raw json.RawMessage) (tool.Result, error) {
	var a reviseArgs
	if err := tool.DecodeArgs("plan_update", raw, &a); err != nil {
		return tool.Result{}, err
	}
	if strings.TrimSpace(a.PlanID) == "" {
		return tool.Result{}, fmt.Errorf("plan_update: plan_id is required")
	}
	if !a.titleSet && !a.stepsSet && !a.notesSet {
		return tool.Result{}, fmt.Errorf("plan_update: at least one of title, steps, or notes is required")
	}
	if a.titleSet && strings.TrimSpace(a.Title) == "" {
		return tool.Result{}, fmt.Errorf("plan_update: title must be non-empty when provided")
	}
	if a.stepsSet {
		if len(a.Steps) == 0 {
			return tool.Result{}, fmt.Errorf("plan_update: steps must contain at least one entry when provided")
		}
		for i, s := range a.Steps {
			if strings.TrimSpace(s) == "" {
				return tool.Result{}, fmt.Errorf("plan_update: steps[%d] is empty", i)
			}
		}
	}

	path, err := planPath(t.workspace, a.PlanID)
	if err != nil {
		return tool.Result{}, fmt.Errorf("plan_update: %w", err)
	}
	doc, err := loadPlan(path)
	if err != nil {
		return tool.Result{}, fmt.Errorf("plan_update: %w", err)
	}
	if a.titleSet {
		doc.title = strings.TrimSpace(a.Title)
	}
	if a.stepsSet {
		doc.steps = nil
		for i, s := range a.Steps {
			doc.steps = append(doc.steps, step{marker: stepMarkerPending, index: i + 1, text: strings.TrimSpace(s)})
		}
		doc.status = statusInProgress
	}
	if a.notesSet {
		doc.notes = strings.TrimSpace(a.Notes)
	}

	logLine := fmt.Sprintf("- %s plan updated", nowFunc().Format("2006-01-02T15:04:05Z07:00"))
	if reason := strings.TrimSpace(a.Reason); reason != "" {
		logLine += ": " + reason
	}
	doc.log = append(doc.log, logLine)

	if err := savePlan(path, doc); err != nil {
		return tool.Result{}, fmt.Errorf("plan_update: %w", err)
	}
	rendered := renderPlan(doc)
	return tool.Result{
		Content: fmt.Sprintf("plan_id=%s\npath=%s\n\n%s", a.PlanID, path, rendered),
		Files:   []tool.FileChange{{Path: path, Kind: tool.KindModify}},
	}, nil
}
