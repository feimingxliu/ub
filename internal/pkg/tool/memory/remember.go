// Package memory implements the `remember` and `recall` tools that let an
// agent write and search durable facts. Reading happens at request time via
// the agent's withMemoryContext injection, not via a tool call — anything
// in the memory file is already on the prompt path.
package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/invopop/jsonschema"

	"github.com/feimingxliu/ub/internal/pkg/tool"
	"github.com/feimingxliu/ub/internal/pkg/workspace/memory"
)

type rememberArgs struct {
	Text     string `json:"text" jsonschema:"required,description=Fact to remember. Will be stored in memory with a timestamped entry."`
	Category string `json:"category,omitempty" jsonschema:"enum=preference,enum=project,enum=pattern,enum=decision,enum=debug,enum=general,description=Category for the entry. Defaults to general. For auto memory this controls injection priority; for global instructions it is recorded in the appended entry."`
	Scope    string `json:"scope,omitempty" jsonschema:"enum=auto,enum=global,description=Memory scope. auto stores in the user's state directory keyed by project. global stores in ~/.config/ub/instructions.md for hand-written cross-project preferences. Defaults to auto."`
}

type rememberTool struct {
	workspace string
	schema    *jsonschema.Schema
}

func newRememberTool(workspaceRoot string) *rememberTool {
	return &rememberTool{
		workspace: workspaceRoot,
		schema:    jsonschema.Reflect(&rememberArgs{}),
	}
}

func (t *rememberTool) Name() string { return "remember" }
func (t *rememberTool) Description() string {
	return "Append a durable fact to ub's memory. auto scope writes to the user's state directory (keyed by project, never in git) and merges duplicate entries in the same category; global scope appends to ~/.config/ub/instructions.md without rewriting hand-written cross-project preferences."
}
func (t *rememberTool) Schema() *jsonschema.Schema { return t.schema }
func (t *rememberTool) Risk() tool.Risk            { return tool.RiskSafe }

func (t *rememberTool) Execute(_ context.Context, raw json.RawMessage) (tool.Result, error) {
	var a rememberArgs
	if err := tool.DecodeArgs("remember", raw, &a); err != nil {
		return tool.Result{}, err
	}
	if strings.TrimSpace(a.Text) == "" {
		return tool.Result{}, errors.New("remember: text is required")
	}

	// Resolve category.
	cat := memory.DefaultCategory
	if a.Category != "" {
		if !memory.ValidCategory(a.Category) {
			return tool.Result{}, fmt.Errorf("remember: invalid category %q", a.Category)
		}
		cat = memory.Category(a.Category)
	}

	// Resolve scope.
	scope := strings.TrimSpace(a.Scope)
	if scope == "" {
		scope = string(memory.ScopeAuto)
	}
	// Backward compat: "workspace" → "auto".
	if scope == "workspace" {
		scope = string(memory.ScopeAuto)
	}
	if !memory.ValidScope(scope) {
		return tool.Result{}, fmt.Errorf("remember: invalid scope %q (expected auto or global)", scope)
	}

	out, err := memory.AppendWithOutcome(t.workspace, memory.Scope(scope), cat, a.Text)
	if err != nil {
		return tool.Result{}, fmt.Errorf("remember: %w", err)
	}

	return tool.Result{
		Content: fmt.Sprintf("remembered (%s, %s): %s\n%s", scope, cat, out.Path, out.Heading),
		Files:   []tool.FileChange{{Path: out.Path, Kind: tool.KindModify}},
		Metadata: map[string]string{
			"memory_scope":    string(out.Scope),
			"memory_category": string(out.Category),
			"memory_text":     out.Text,
			"memory_path":     out.Path,
			"memory_heading":  out.Heading,
			"memory_action":   string(out.Action),
		},
	}, nil
}
