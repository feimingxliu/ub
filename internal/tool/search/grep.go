package search

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/invopop/jsonschema"

	"github.com/feimingxliu/ub/internal/tool"
)

type grepArgs struct {
	Pattern string `json:"pattern" jsonschema:"required,description=Regular expression (Go RE2 syntax)."`
	Path    string `json:"path,omitempty" jsonschema:"description=Subdirectory to search, relative to workspace root. Defaults to '.'."`
	Include string `json:"include,omitempty" jsonschema:"description=Optional doublestar glob filter applied to matched paths (e.g. '**/*.go')."`
}

type grepTool struct {
	root   string
	schema *jsonschema.Schema
}

func newGrepTool(root string) *grepTool {
	return &grepTool{
		root:   root,
		schema: jsonschema.Reflect(&grepArgs{}),
	}
}

func (t *grepTool) Name() string { return "grep" }
func (t *grepTool) Description() string {
	return "Search for a regular expression across workspace files. Returns 'path:line:match' lines."
}
func (t *grepTool) Schema() *jsonschema.Schema { return t.schema }
func (t *grepTool) Risk() tool.Risk            { return tool.RiskSafe }

func (t *grepTool) Execute(ctx context.Context, raw json.RawMessage) (tool.Result, error) {
	var a grepArgs
	if err := tool.UnmarshalArgs(raw, &a); err != nil {
		return tool.Result{}, fmt.Errorf("grep: invalid args: %w", err)
	}
	if a.Pattern == "" {
		return tool.Result{}, fmt.Errorf("grep: pattern is required")
	}
	re, err := regexp.Compile(a.Pattern)
	if err != nil {
		return tool.Result{}, fmt.Errorf("grep: invalid regex: %w", err)
	}

	path := a.Path
	if path == "" {
		path = "."
	}
	abs, err := tool.Resolve(t.root, path)
	if err != nil {
		return tool.Result{}, err
	}

	opts := grepOpts{
		pattern:    re,
		rawPattern: a.Pattern,
		root:       t.root,
		searchPath: abs,
		include:    a.Include,
	}
	hits, err := newBackend().run(ctx, opts)
	if err != nil {
		return tool.Result{}, fmt.Errorf("grep: %w", err)
	}

	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Path != hits[j].Path {
			return hits[i].Path < hits[j].Path
		}
		return hits[i].Line < hits[j].Line
	})

	if len(hits) == 0 {
		return tool.Result{Content: ""}, nil
	}

	var b strings.Builder
	for i, h := range hits {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "%s:%d:%s", h.Path, h.Line, h.Text)
	}
	return tool.Result{Content: b.String()}, nil
}
