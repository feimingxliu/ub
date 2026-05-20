package fs

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/invopop/jsonschema"

	"github.com/feimingxliu/ub/internal/tool"
)

// defaultReadMaxLines is the cap applied when the caller does not pass an
// explicit limit. The agent may apply a stricter byte cap and spill the full
// output to state storage before returning the result to the model.
const defaultReadMaxLines = 400

type readArgs struct {
	Path   string      `json:"path"   jsonschema:"required,description=Path relative to workspace root. Absolute paths must be inside the workspace or ub tool-output state."`
	Offset tool.IntArg `json:"offset,omitempty" jsonschema:"description=1-based line number to start at. Defaults to 1."`
	Limit  tool.IntArg `json:"limit,omitempty"  jsonschema:"description=Maximum number of lines to return. Defaults to all lines (capped for model context)."`
}

type readTool struct {
	root      string
	stateRoot string
	maxLines  int
	schema    *jsonschema.Schema
}

type numbered struct {
	n    int
	text string
}

func newReadTool(root string) *readTool {
	return newReadToolWithOptions(root, "", defaultReadMaxLines)
}

func newReadToolWithOptions(root, stateRoot string, maxLines int) *readTool {
	if maxLines <= 0 {
		maxLines = defaultReadMaxLines
	}
	return &readTool{
		root:      root,
		stateRoot: stateRoot,
		maxLines:  maxLines,
		schema:    jsonschema.Reflect(&readArgs{}),
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
	abs, err := t.resolveReadPath(a.Path)
	if err != nil {
		return tool.Result{}, err
	}

	f, err := os.Open(abs)
	if err != nil {
		return tool.Result{}, fmt.Errorf("read: open %s: %w", a.Path, err)
	}
	defer f.Close()

	offset := max(int(a.Offset), 1)

	limit := int(a.Limit)
	implicitLimit := limit <= 0
	if implicitLimit {
		limit = t.maxLines
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	var picked []numbered
	var all []numbered
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		if lineNo < offset {
			continue
		}
		ln := numbered{n: lineNo, text: scanner.Text()}
		if implicitLimit {
			all = append(all, ln)
		}
		if len(picked) < limit {
			picked = append(picked, ln)
		} else if !implicitLimit {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return tool.Result{}, fmt.Errorf("read: scan %s: %w", a.Path, err)
	}

	if len(picked) == 0 {
		return tool.Result{Content: ""}, nil
	}

	content := formatNumberedLines(picked)
	var full string
	if implicitLimit && len(all) > len(picked) {
		full = formatNumberedLines(all)
		content += "\n... (truncated, use offset/limit)"
	}
	return tool.Result{Content: content, FullContent: full}, nil
}

func (t *readTool) resolveReadPath(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return resolve(t.root, path)
	}
	if abs, err := resolve(t.root, path); err == nil {
		return abs, nil
	}
	stateRoot := strings.TrimSpace(t.stateRoot)
	if stateRoot == "" {
		return resolve(t.root, path)
	}
	cleanRoot := filepath.Clean(stateRoot)
	abs := filepath.Clean(path)
	rel, err := filepath.Rel(cleanRoot, abs)
	if err != nil {
		return "", fmt.Errorf("tool: resolve %q: %w", path, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("tool: path %q is outside workspace root and tool-output state", path)
	}
	return abs, nil
}

func formatNumberedLines(lines []numbered) string {
	if len(lines) == 0 {
		return ""
	}
	maxN := lines[len(lines)-1].n
	width := len(fmt.Sprintf("%d", maxN))
	var b strings.Builder
	for i, ln := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "%*d\t%s", width, ln.n, ln.text)
	}
	return b.String()
}
