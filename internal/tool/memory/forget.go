package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/invopop/jsonschema"

	"github.com/feimingxliu/ub/internal/tool"
	workspacememory "github.com/feimingxliu/ub/internal/workspace/memory"
)

type forgetArgs struct {
	Text     string `json:"text" jsonschema:"required,description=Exact text of the project auto-memory entry to remove. Use recall first when the stored wording or category is unclear."`
	Category string `json:"category" jsonschema:"required,enum=preference,enum=project,enum=pattern,enum=decision,enum=debug,enum=general,description=Category of the exact auto-memory entry to remove."`
}

type forgetTool struct {
	workspace string
	schema    *jsonschema.Schema
}

func newForgetTool(workspaceRoot string) *forgetTool {
	return &forgetTool{
		workspace: workspaceRoot,
		schema:    jsonschema.Reflect(&forgetArgs{}),
	}
}

func (t *forgetTool) Name() string { return "forget" }
func (t *forgetTool) Description() string {
	return "Remove one exact entry from project auto memory. Use recall first if the saved text or category is unclear. This never changes hand-written global instructions."
}
func (t *forgetTool) Schema() *jsonschema.Schema { return t.schema }
func (t *forgetTool) Risk() tool.Risk            { return tool.RiskSafe }

func (t *forgetTool) Execute(_ context.Context, raw json.RawMessage) (tool.Result, error) {
	var a forgetArgs
	if err := tool.DecodeArgs("forget", raw, &a); err != nil {
		return tool.Result{}, err
	}
	if strings.TrimSpace(a.Text) == "" {
		return tool.Result{}, errors.New("forget: text is required")
	}
	if !workspacememory.ValidCategory(a.Category) {
		return tool.Result{}, fmt.Errorf("forget: invalid category %q", a.Category)
	}

	out, err := workspacememory.ForgetWithOutcome(t.workspace, workspacememory.Category(a.Category), a.Text)
	if err != nil {
		return tool.Result{}, fmt.Errorf("forget: %w", err)
	}
	return tool.Result{
		Content: fmt.Sprintf("forgot (%s, %s): %s", out.Scope, out.Category, out.Text),
		Files:   []tool.FileChange{{Path: out.Path, Kind: tool.KindModify}},
		Metadata: map[string]string{
			"memory_scope":    string(out.Scope),
			"memory_category": string(out.Category),
			"memory_text":     out.Text,
			"memory_path":     out.Path,
			"memory_action":   string(out.Action),
		},
	}, nil
}
