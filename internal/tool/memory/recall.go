package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/invopop/jsonschema"

	"github.com/feimingxliu/ub/internal/tool"
	"github.com/feimingxliu/ub/internal/workspace/memory"
)

type recallArgs struct {
	Query    string `json:"query,omitempty" jsonschema:"description=Search query to match against memory entries (case-insensitive substring match). Omit to list all entries matching the category filter."`
	Category string `json:"category,omitempty" jsonschema:"enum=preference,enum=project,enum=pattern,enum=decision,enum=debug,enum=general,description=Filter by category. Omit to search all categories."`
}

type recallTool struct {
	workspace string
	schema    *jsonschema.Schema
}

func newRecallTool(workspaceRoot string) *recallTool {
	return &recallTool{
		workspace: workspaceRoot,
		schema:    jsonschema.Reflect(&recallArgs{}),
	}
}

func (t *recallTool) Name() string { return "recall" }
func (t *recallTool) Description() string {
	return "Search auto memory entries by keyword and optional category. Returns matching entries from the project's machine-appended memory. Use to look up specific facts without reading the full memory."
}
func (t *recallTool) Schema() *jsonschema.Schema { return t.schema }
func (t *recallTool) Risk() tool.Risk            { return tool.RiskSafe }

func (t *recallTool) Execute(_ context.Context, raw json.RawMessage) (tool.Result, error) {
	var a recallArgs
	if err := tool.DecodeArgs("recall", raw, &a); err != nil {
		return tool.Result{}, err
	}

	var cat memory.Category
	if a.Category != "" {
		if !memory.ValidCategory(a.Category) {
			return tool.Result{}, fmt.Errorf("recall: invalid category %q", a.Category)
		}
		cat = memory.Category(a.Category)
	}

	entries, err := memory.Recall(t.workspace, a.Query, cat)
	if err != nil {
		return tool.Result{}, fmt.Errorf("recall: %w", err)
	}
	if len(entries) == 0 {
		return tool.Result{Content: "no matching memory entries found"}, nil
	}

	var b strings.Builder
	for _, e := range entries {
		fmt.Fprintf(&b, "[%s] %s *(%s)*\n", e.Category, e.Text, e.Timestamp.Format("2006-01-02T15:04:05Z07:00"))
	}
	return tool.Result{Content: b.String()}, nil
}
