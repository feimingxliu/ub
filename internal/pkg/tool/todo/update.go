package todo

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/invopop/jsonschema"

	"github.com/feimingxliu/ub/internal/pkg/tool"
)

type updateArgs struct {
	ID        string      `json:"id,omitempty" jsonschema:"description=Todo id returned by todo_write. Required unless item_index is provided."`
	ItemIndex tool.IntArg `json:"item_index,omitempty" jsonschema:"description=1-based todo position. Used when id is omitted."`
	Status    string      `json:"status" jsonschema:"required,enum=pending,enum=in_progress,enum=completed,enum=skipped,enum=failed,description=New task state."`
	Content   string      `json:"content,omitempty" jsonschema:"description=Optional replacement task text."`
	Note      string      `json:"note,omitempty" jsonschema:"description=Optional replacement note shown next to the task."`
}

type updateTool struct {
	schema *jsonschema.Schema
}

func newUpdateTool() *updateTool {
	return &updateTool{schema: jsonschema.Reflect(&updateArgs{})}
}

func (t *updateTool) Name() string { return "todo_update" }
func (t *updateTool) Description() string {
	return "Update one item in the current session todo list. Use after each meaningful execution step so the TUI stays current. Identify the item by id or 1-based item_index, set status to pending, in_progress, completed, skipped, or failed, and optionally replace content or note. Mark completed only after the work and any needed verification are actually done. The list may contain at most one in_progress item."
}
func (t *updateTool) Schema() *jsonschema.Schema { return t.schema }
func (t *updateTool) Risk() tool.Risk            { return tool.RiskSafe }

func (t *updateTool) Execute(ctx context.Context, raw json.RawMessage) (tool.Result, error) {
	var a updateArgs
	if err := tool.DecodeArgs("todo_update", raw, &a); err != nil {
		return tool.Result{}, err
	}
	sessionID := strings.TrimSpace(tool.SessionIDFromContext(ctx))
	if sessionID == "" {
		return tool.Result{}, fmt.Errorf("todo_update: session id is required")
	}
	if strings.TrimSpace(a.Status) == "" {
		return tool.Result{}, fmt.Errorf("todo_update: status is required")
	}
	status, err := normalizeStatus(a.Status)
	if err != nil {
		return tool.Result{}, fmt.Errorf("todo_update: %w", err)
	}
	l, err := load(sessionID)
	if err != nil {
		return tool.Result{}, fmt.Errorf("todo_update: %w", err)
	}
	idx, err := findItem(l, a.ID, int(a.ItemIndex))
	if err != nil {
		return tool.Result{}, fmt.Errorf("todo_update: %w", err)
	}
	if status == "in_progress" {
		for i, it := range l.Items {
			if i != idx && it.Status == "in_progress" {
				return tool.Result{}, fmt.Errorf("todo_update: item %s is already in_progress; complete, skip, fail, or reset it first", it.ID)
			}
		}
	}
	l.Items[idx].Status = status
	if content := strings.TrimSpace(a.Content); content != "" {
		l.Items[idx].Content = content
	}
	if strings.TrimSpace(a.Note) != "" {
		l.Items[idx].Note = strings.TrimSpace(a.Note)
	}
	if err := validate(l); err != nil {
		return tool.Result{}, fmt.Errorf("todo_update: %w", err)
	}
	if err := save(sessionID, l); err != nil {
		return tool.Result{}, fmt.Errorf("todo_update: %w", err)
	}
	return tool.Result{Content: render(sessionID, l)}, nil
}

func findItem(l list, id string, itemIndex int) (int, error) {
	id = strings.TrimSpace(id)
	if id != "" {
		for i, it := range l.Items {
			if it.ID == id {
				return i, nil
			}
		}
		return 0, fmt.Errorf("todo id %q not found", id)
	}
	if itemIndex < 1 {
		return 0, fmt.Errorf("id or item_index is required")
	}
	if itemIndex > len(l.Items) {
		return 0, fmt.Errorf("item_index %d out of range [1, %d]", itemIndex, len(l.Items))
	}
	return itemIndex - 1, nil
}
