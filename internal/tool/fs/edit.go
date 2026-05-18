package fs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	udiff "github.com/aymanbagabas/go-udiff"
	"github.com/invopop/jsonschema"

	"github.com/feimingxliu/ub/internal/tool"
)

// readFileFn is overridden in tests to exercise the TOCTOU guard in
// (*editTool).Execute. The first call returns the "before" snapshot,
// the second returns the (possibly mutated) current content.
var readFileFn = os.ReadFile

type editArgs struct {
	Path       string `json:"path"        jsonschema:"required,description=Path relative to workspace root."`
	Old        string `json:"old"         jsonschema:"required,description=Exact substring to replace."`
	New        string `json:"new"         jsonschema:"required,description=Replacement text."`
	ReplaceAll bool   `json:"replace_all,omitempty" jsonschema:"description=Replace all matches when true. Defaults to false."`
}

type editTool struct {
	root     string
	notifier ChangeNotifier
	schema   *jsonschema.Schema
}

func newEditTool(root string) *editTool {
	return newEditToolWithNotifier(root, nil)
}

func newEditToolWithNotifier(root string, notifier ChangeNotifier) *editTool {
	return &editTool{
		root:     root,
		notifier: notifier,
		schema:   jsonschema.Reflect(&editArgs{}),
	}
}

func (t *editTool) Name() string               { return "edit" }
func (t *editTool) Description() string        { return "Replace an exact substring inside a workspace file." }
func (t *editTool) Schema() *jsonschema.Schema { return t.schema }
func (t *editTool) Risk() tool.Risk            { return tool.RiskWrite }

func (t *editTool) parseAndResolve(raw json.RawMessage) (editArgs, string, error) {
	var a editArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return a, "", fmt.Errorf("edit: invalid args: %w", err)
	}
	if a.Path == "" {
		return a, "", fmt.Errorf("edit: path is required")
	}
	if a.Old == "" {
		return a, "", fmt.Errorf("edit: old is required")
	}
	abs, err := resolve(t.root, a.Path)
	if err != nil {
		return a, "", err
	}
	return a, abs, nil
}

// applyEdit returns the new file content and the number of replacements.
// It returns an error if old is missing or there are multiple matches
// without replace_all set.
func applyEdit(content string, a editArgs) (string, int, error) {
	count := strings.Count(content, a.Old)
	switch {
	case count == 0:
		return "", 0, fmt.Errorf("edit: old string not found")
	case count > 1 && !a.ReplaceAll:
		return "", 0, fmt.Errorf("edit: %d matches, set replace_all=true to replace all", count)
	}
	n := 1
	if a.ReplaceAll {
		n = -1
	}
	return strings.Replace(content, a.Old, a.New, n), count, nil
}

func (t *editTool) Preview(_ context.Context, raw json.RawMessage) (tool.Preview, error) {
	a, abs, err := t.parseAndResolve(raw)
	if err != nil {
		return tool.Preview{}, err
	}
	rel, _ := relToRoot(t.root, abs)

	before, err := readFileFn(abs)
	if err != nil {
		return tool.Preview{}, fmt.Errorf("edit: read %s: %w", rel, err)
	}
	after, _, err := applyEdit(string(before), a)
	if err != nil {
		return tool.Preview{}, err
	}
	diff := udiff.Unified(rel, rel, string(before), after)
	return tool.Preview{
		Summary: fmt.Sprintf("Edit %s", rel),
		Files: []tool.FileDiff{{
			Path:        rel,
			Kind:        tool.KindModify,
			UnifiedDiff: diff,
		}},
	}, nil
}

func (t *editTool) Execute(ctx context.Context, raw json.RawMessage) (tool.Result, error) {
	a, abs, err := t.parseAndResolve(raw)
	if err != nil {
		return tool.Result{}, err
	}
	rel, _ := relToRoot(t.root, abs)

	before, err := readFileFn(abs)
	if err != nil {
		return tool.Result{}, fmt.Errorf("edit: read %s: %w", rel, err)
	}
	after, count, err := applyEdit(string(before), a)
	if err != nil {
		return tool.Result{}, err
	}
	// re-check the file just before writing to detect concurrent changes
	// between Preview and Execute.
	current, err := readFileFn(abs)
	if err != nil {
		return tool.Result{}, fmt.Errorf("edit: re-read %s: %w", rel, err)
	}
	if string(current) != string(before) {
		return tool.Result{}, fmt.Errorf("edit: %s changed on disk since preview", rel)
	}
	if err := os.WriteFile(abs, []byte(after), 0o644); err != nil {
		return tool.Result{}, fmt.Errorf("edit: write %s: %w", rel, err)
	}
	notifySuffix := notifyChanged(ctx, t.notifier, abs)
	return tool.Result{
		Content: fmt.Sprintf("edited %s (%d replacement(s))%s", rel, count, notifySuffix),
		Files: []tool.FileChange{{
			Path: rel,
			Kind: tool.KindModify,
		}},
	}, nil
}
