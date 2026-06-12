package fs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/invopop/jsonschema"

	"github.com/feimingxliu/ub/internal/pkg/tool"
)

type lsArgs struct {
	Path string `json:"path" jsonschema:"required,description=Directory to list, relative to workspace root."`
}

type lsTool struct {
	root   string
	schema *jsonschema.Schema
}

func newLsTool(root string) *lsTool {
	return &lsTool{
		root:   root,
		schema: jsonschema.Reflect(&lsArgs{}),
	}
}

func (t *lsTool) Name() string               { return "ls" }
func (t *lsTool) Description() string        { return "List files and directories under a workspace path." }
func (t *lsTool) Schema() *jsonschema.Schema { return t.schema }
func (t *lsTool) Risk() tool.Risk            { return tool.RiskSafe }

func (t *lsTool) Execute(_ context.Context, raw json.RawMessage) (tool.Result, error) {
	var a lsArgs
	if err := tool.DecodeArgs("ls", raw, &a); err != nil {
		return tool.Result{}, err
	}
	if a.Path == "" {
		return tool.Result{}, fmt.Errorf("ls: path is required")
	}
	abs, err := resolve(t.root, a.Path)
	if err != nil {
		return tool.Result{}, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return tool.Result{}, fmt.Errorf("ls: stat %s: %w", a.Path, err)
	}
	if !info.IsDir() {
		return tool.Result{}, fmt.Errorf("ls: %s is not a directory", a.Path)
	}

	entries, err := os.ReadDir(abs)
	if err != nil {
		return tool.Result{}, fmt.Errorf("ls: read %s: %w", a.Path, err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	var b strings.Builder
	for i, e := range entries {
		if i > 0 {
			b.WriteByte('\n')
		}
		kind := entryKind(e)
		fmt.Fprintf(&b, "%s\t%s", kind, e.Name())
	}
	return tool.Result{Content: b.String()}, nil
}

func entryKind(e os.DirEntry) string {
	switch {
	case e.IsDir():
		return "dir"
	case e.Type()&os.ModeSymlink != 0:
		return "symlink"
	case e.Type().IsRegular():
		return "file"
	default:
		return "other"
	}
}
