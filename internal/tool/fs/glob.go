package fs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/invopop/jsonschema"

	"github.com/feimingxliu/ub/internal/tool"
)

type globArgs struct {
	Pattern string `json:"pattern" jsonschema:"required,description=Doublestar glob pattern relative to workspace root (e.g. **/*.go)."`
}

type globTool struct {
	root   string
	schema *jsonschema.Schema
}

func newGlobTool(root string) *globTool {
	return &globTool{
		root:   root,
		schema: jsonschema.Reflect(&globArgs{}),
	}
}

func (t *globTool) Name() string { return "glob" }
func (t *globTool) Description() string {
	return "Match workspace paths against a doublestar glob pattern and return matches sorted lexicographically."
}
func (t *globTool) Schema() *jsonschema.Schema { return t.schema }
func (t *globTool) Risk() tool.Risk            { return tool.RiskSafe }

func (t *globTool) Execute(_ context.Context, raw json.RawMessage) (tool.Result, error) {
	var a globArgs
	if err := tool.UnmarshalArgs(raw, &a); err != nil {
		return tool.Result{}, fmt.Errorf("glob: invalid args: %w", err)
	}
	if a.Pattern == "" {
		return tool.Result{}, fmt.Errorf("glob: pattern is required")
	}
	matches, err := doublestar.Glob(os.DirFS(t.root), a.Pattern)
	if err != nil {
		return tool.Result{}, fmt.Errorf("glob: %w", err)
	}
	// matches are already relative to root because we used a rooted FS.
	sort.Strings(matches)
	return tool.Result{Content: strings.Join(matches, "\n")}, nil
}
