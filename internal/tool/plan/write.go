package plan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/invopop/jsonschema"

	"github.com/feimingxliu/ub/internal/tool"
)

type writeArgs struct {
	Title string   `json:"title" jsonschema:"required,description=Short human-readable plan title (used to derive plan_id)."`
	Steps []string `json:"steps" jsonschema:"required,description=Ordered list of step descriptions; at least one entry."`
	Notes string   `json:"notes,omitempty" jsonschema:"description=Optional free-form context written under the Notes section."`
}

type writeTool struct {
	workspace string
	schema    *jsonschema.Schema
}

func newWriteTool(workspace string) *writeTool {
	return &writeTool{
		workspace: workspace,
		schema:    jsonschema.Reflect(&writeArgs{}),
	}
}

func (t *writeTool) Name() string { return "plan_write" }
func (t *writeTool) Description() string {
	return "Write a new plan markdown to <workspace>/.ub/plans/<id>.md with a title, ordered steps, and optional notes. Returns the plan_id used by plan_update_step."
}
func (t *writeTool) Schema() *jsonschema.Schema { return t.schema }
func (t *writeTool) Risk() tool.Risk            { return tool.RiskSafe }

func (t *writeTool) Execute(_ context.Context, raw json.RawMessage) (tool.Result, error) {
	var a writeArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return tool.Result{}, fmt.Errorf("plan_write: invalid args: %w", err)
	}
	if strings.TrimSpace(a.Title) == "" {
		return tool.Result{}, fmt.Errorf("plan_write: title is required")
	}
	if len(a.Steps) == 0 {
		return tool.Result{}, fmt.Errorf("plan_write: steps is required (at least one step)")
	}
	for i, s := range a.Steps {
		if strings.TrimSpace(s) == "" {
			return tool.Result{}, fmt.Errorf("plan_write: steps[%d] is empty", i)
		}
	}

	planID := newPlanID(a.Title)
	path := planPath(t.workspace, planID)
	if _, err := os.Stat(path); err == nil {
		return tool.Result{}, fmt.Errorf("plan_write: plan_id %s already exists", planID)
	} else if !errors.Is(err, os.ErrNotExist) {
		return tool.Result{}, fmt.Errorf("plan_write: stat target: %w", err)
	}

	doc := planDoc{
		title:   a.Title,
		created: nowFunc().Format("2006-01-02T15:04:05Z07:00"),
		status:  statusInProgress,
		notes:   a.Notes,
	}
	for i, s := range a.Steps {
		doc.steps = append(doc.steps, step{marker: " ", index: i + 1, text: strings.TrimSpace(s)})
	}
	rendered := renderPlan(doc)
	if err := os.MkdirAll(filepath.Dir(path), plansDirPerm); err != nil {
		return tool.Result{}, fmt.Errorf("plan_write: mkdir: %w", err)
	}
	if err := os.WriteFile(path, []byte(rendered), planFilePerm); err != nil {
		return tool.Result{}, fmt.Errorf("plan_write: write: %w", err)
	}

	rel, _ := filepath.Rel(t.workspace, path)
	if rel == "" {
		rel = path
	}
	return tool.Result{
		Content: fmt.Sprintf("plan_id=%s\npath=%s\n\n%s", planID, path, rendered),
		Files:   []tool.FileChange{{Path: rel, Kind: tool.KindCreate}},
	}, nil
}
