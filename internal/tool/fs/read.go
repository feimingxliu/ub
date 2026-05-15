package fs

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/invopop/jsonschema"

	"github.com/feimingxliu/ub/internal/tool"
)

// readMaxLines is the cap applied when the caller does not pass an
// explicit limit, to keep a single tool_result from blowing up the
// context window.
const readMaxLines = 2000

type readArgs struct {
	Path   string `json:"path"   jsonschema:"required,description=Path relative to workspace root (absolute paths must still be inside root)."`
	Offset int    `json:"offset,omitempty" jsonschema:"description=1-based line number to start at. Defaults to 1."`
	Limit  int    `json:"limit,omitempty"  jsonschema:"description=Maximum number of lines to return. Defaults to all lines (capped at 2000)."`
}

type readTool struct {
	root   string
	schema *jsonschema.Schema
}

func newReadTool(root string) *readTool {
	return &readTool{
		root:   root,
		schema: jsonschema.Reflect(&readArgs{}),
	}
}

func (t *readTool) Name() string { return "read" }
func (t *readTool) Description() string {
	return "Read a UTF-8 text file from the workspace and return its content with line numbers."
}
func (t *readTool) Schema() *jsonschema.Schema { return t.schema }
func (t *readTool) Risk() tool.Risk            { return tool.RiskSafe }

func (t *readTool) Execute(_ context.Context, raw json.RawMessage) (tool.Result, error) {
	var a readArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return tool.Result{}, fmt.Errorf("read: invalid args: %w", err)
	}
	if a.Path == "" {
		return tool.Result{}, fmt.Errorf("read: path is required")
	}
	abs, err := resolve(t.root, a.Path)
	if err != nil {
		return tool.Result{}, err
	}

	f, err := os.Open(abs)
	if err != nil {
		return tool.Result{}, fmt.Errorf("read: open %s: %w", a.Path, err)
	}
	defer f.Close()

	offset := max(a.Offset, 1)

	limit := a.Limit
	truncated := false
	if limit <= 0 {
		// no explicit limit: cap at readMaxLines and signal truncation
		limit = readMaxLines
		truncated = true
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	type numbered struct {
		n    int
		text string
	}
	var picked []numbered
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		if lineNo < offset {
			continue
		}
		if len(picked) >= limit {
			break
		}
		picked = append(picked, numbered{n: lineNo, text: scanner.Text()})
	}
	if err := scanner.Err(); err != nil {
		return tool.Result{}, fmt.Errorf("read: scan %s: %w", a.Path, err)
	}

	// truncated only matters if (a) we hit the implicit cap and (b) more
	// lines exist beyond what we returned.
	moreExists := false
	if truncated && len(picked) >= limit {
		// try to read one more line to confirm
		if scanner.Scan() {
			moreExists = true
		}
	}

	if len(picked) == 0 {
		return tool.Result{Content: ""}, nil
	}

	maxN := picked[len(picked)-1].n
	width := len(fmt.Sprintf("%d", maxN))

	var b strings.Builder
	for i, ln := range picked {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "%*d\t%s", width, ln.n, ln.text)
	}
	if truncated && moreExists {
		b.WriteString("\n... (truncated, use offset/limit)")
	}

	return tool.Result{Content: b.String()}, nil
}
