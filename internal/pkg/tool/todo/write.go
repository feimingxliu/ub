package todo

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/invopop/jsonschema"

	"github.com/feimingxliu/ub/internal/pkg/tool"
)

type inputItem struct {
	ID      string `json:"id,omitempty" jsonschema:"description=Stable id used by todo_update. Omit to assign 1-based numeric ids."`
	Content string `json:"content" jsonschema:"required,description=Short execution task text."`
	Status  string `json:"status,omitempty" jsonschema:"enum=pending,enum=in_progress,enum=completed,enum=skipped,enum=failed,description=Task state. Defaults to pending."`
	Note    string `json:"note,omitempty" jsonschema:"description=Optional short note shown next to the task."`
}

type writeArgs struct {
	Items []inputItem `json:"items" jsonschema:"required,description=Current execution todo list. Replaces the previous session todo list."`
}

func (a *writeArgs) UnmarshalJSON(raw []byte) error {
	var body map[string]json.RawMessage
	if err := json.Unmarshal(raw, &body); err != nil {
		return err
	}
	for _, key := range []string{"items", "todos", "tasks"} {
		rawItems, ok := body[key]
		if !ok {
			continue
		}
		items, err := parseItems(rawItems)
		if err != nil {
			return err
		}
		a.Items = items
		return nil
	}
	return nil
}

func parseItems(raw json.RawMessage) ([]inputItem, error) {
	var items []inputItem
	if err := json.Unmarshal(raw, &items); err == nil {
		return items, nil
	}
	var stringsOnly []string
	if err := json.Unmarshal(raw, &stringsOnly); err != nil {
		return nil, fmt.Errorf("items must be an array of objects or strings: %w", err)
	}
	items = make([]inputItem, 0, len(stringsOnly))
	for _, text := range stringsOnly {
		items = append(items, inputItem{Content: text})
	}
	return items, nil
}

type writeTool struct {
	schema *jsonschema.Schema
}

func newWriteTool() *writeTool {
	return &writeTool{schema: jsonschema.Reflect(&writeArgs{})}
}

func (t *writeTool) Name() string { return "todo_write" }
func (t *writeTool) Description() string {
	return "Create or replace the current session execution todo list. Use this for ordinary multi-step work that needs a live progress view without creating a persistent plan artifact. Items may be strings or objects with content, optional id, status, and note. Status values are pending, in_progress, completed, skipped, and failed; at most one item may be in_progress."
}
func (t *writeTool) Schema() *jsonschema.Schema { return t.schema }
func (t *writeTool) Risk() tool.Risk            { return tool.RiskSafe }

func (t *writeTool) Execute(ctx context.Context, raw json.RawMessage) (tool.Result, error) {
	var a writeArgs
	if err := tool.DecodeArgs("todo_write", raw, &a); err != nil {
		return tool.Result{}, err
	}
	sessionID := strings.TrimSpace(tool.SessionIDFromContext(ctx))
	if sessionID == "" {
		return tool.Result{}, fmt.Errorf("todo_write: session id is required")
	}
	l := list{Items: make([]item, 0, len(a.Items))}
	for _, it := range a.Items {
		l.Items = append(l.Items, item{
			ID:      it.ID,
			Content: it.Content,
			Status:  it.Status,
			Note:    it.Note,
		})
	}
	if err := validate(l); err != nil {
		return tool.Result{}, fmt.Errorf("todo_write: %w", err)
	}
	if err := save(sessionID, l); err != nil {
		return tool.Result{}, fmt.Errorf("todo_write: %w", err)
	}
	return tool.Result{Content: render(sessionID, l)}, nil
}
