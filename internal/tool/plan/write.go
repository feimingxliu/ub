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

func (a *writeArgs) UnmarshalJSON(raw []byte) error {
	type alias writeArgs
	var aux struct {
		Title string          `json:"title"`
		Steps json.RawMessage `json:"steps"`
		Notes string          `json:"notes,omitempty"`
	}
	if err := json.Unmarshal(raw, &aux); err != nil {
		return err
	}
	steps, err := parseWriteSteps(aux.Steps)
	if err != nil {
		return err
	}
	*a = writeArgs(alias{
		Title: aux.Title,
		Steps: steps,
		Notes: aux.Notes,
	})
	return nil
}

func parseWriteSteps(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var steps []string
	if err := json.Unmarshal(raw, &steps); err == nil {
		return steps, nil
	}
	var encoded string
	if err := json.Unmarshal(raw, &encoded); err != nil {
		return nil, fmt.Errorf("steps must be an array of strings: %w", err)
	}
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return nil, nil
	}
	if !strings.HasPrefix(encoded, "[") {
		return nil, fmt.Errorf("steps must be an array of strings")
	}
	if err := json.Unmarshal([]byte(encoded), &steps); err != nil {
		return nil, fmt.Errorf("steps string must contain a JSON array of strings: %w", err)
	}
	return steps, nil
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
	return "Write a new plan markdown to <workspace>/.ub/plans/<id>.md with a title, ordered steps, and optional notes. In plan mode, use this before implementation work. Returns the plan_id used by plan_update_step."
}
func (t *writeTool) Schema() *jsonschema.Schema { return t.schema }
func (t *writeTool) Risk() tool.Risk            { return tool.RiskSafe }

func (t *writeTool) Execute(_ context.Context, raw json.RawMessage) (tool.Result, error) {
	var a writeArgs
	if err := tool.UnmarshalArgs(raw, &a); err != nil {
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
