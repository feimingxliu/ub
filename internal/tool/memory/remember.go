// Package memory implements the `remember` tool that lets an agent append
// durable facts to the workspace or global memory file. Reading happens at
// request time via the agent's withMemoryContext injection, not via a tool
// call — anything in the memory file is already on the prompt path.
package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/invopop/jsonschema"

	"github.com/feimingxliu/ub/internal/memory"
	"github.com/feimingxliu/ub/internal/tool"
)

type rememberArgs struct {
	Text  string `json:"text" jsonschema:"required,description=Fact to remember. Will be appended to memory.md with a timestamped heading."`
	Scope string `json:"scope,omitempty" jsonschema:"enum=workspace,enum=global,description=Memory scope. Defaults to workspace."`
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
	return "Append a durable fact to ub's memory file. workspace scope writes to <workspace>/.ub/memory.md and travels with the project; global scope writes to ~/.config/ub/memory.md and travels with the user."
}
func (t *rememberTool) Schema() *jsonschema.Schema { return t.schema }
func (t *rememberTool) Risk() tool.Risk            { return tool.RiskSafe }

func (t *rememberTool) Execute(_ context.Context, raw json.RawMessage) (tool.Result, error) {
	var a rememberArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return tool.Result{}, fmt.Errorf("remember: invalid args: %w", err)
	}
	if strings.TrimSpace(a.Text) == "" {
		return tool.Result{}, errors.New("remember: text is required")
	}
	scope := strings.TrimSpace(a.Scope)
	if scope == "" {
		scope = string(memory.ScopeWorkspace)
	}
	if !memory.ValidScope(scope) {
		return tool.Result{}, fmt.Errorf("remember: invalid scope %q (expected workspace or global)", scope)
	}
	path, heading, err := memory.Append(t.workspace, memory.Scope(scope), a.Text)
	if err != nil {
		return tool.Result{}, fmt.Errorf("remember: %w", err)
	}
	rel := path
	if scope == string(memory.ScopeWorkspace) && t.workspace != "" {
		if r, err := tool.RelToRoot(t.workspace, path); err == nil && !strings.HasPrefix(r, "..") {
			rel = r
		}
	}
	return tool.Result{
		Content: fmt.Sprintf("remembered (%s): %s\n%s", scope, path, heading),
		Files:   []tool.FileChange{{Path: rel, Kind: tool.KindModify}},
	}, nil
}
