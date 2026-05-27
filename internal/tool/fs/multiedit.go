package fs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	udiff "github.com/aymanbagabas/go-udiff"
	"github.com/invopop/jsonschema"

	"github.com/feimingxliu/ub/internal/tool"
)

type multiEditArgs struct {
	Edits []editArgs `json:"edits" jsonschema:"required,description=Edits applied atomically. Same-path edits are accumulated in array order: edit N sees the result of edits 1..N-1."`
}

func (a *multiEditArgs) UnmarshalJSON(raw []byte) error {
	type alias multiEditArgs
	var aux struct {
		Edits json.RawMessage `json:"edits"`
	}
	if err := json.Unmarshal(raw, &aux); err != nil {
		return err
	}
	edits, err := parseMultiEdits(aux.Edits)
	if err != nil {
		return err
	}
	*a = multiEditArgs(alias{Edits: edits})
	return nil
}

func parseMultiEdits(raw json.RawMessage) ([]editArgs, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var edits []editArgs
	if err := json.Unmarshal(raw, &edits); err == nil {
		return edits, nil
	}
	var encoded string
	if err := json.Unmarshal(raw, &encoded); err != nil {
		return nil, fmt.Errorf("edits must be an array of edit objects: %w", err)
	}
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return nil, nil
	}
	if !strings.HasPrefix(encoded, "[") {
		return nil, fmt.Errorf("edits must be an array of edit objects")
	}
	if err := json.Unmarshal([]byte(encoded), &edits); err != nil {
		return nil, fmt.Errorf("edits string must contain a JSON array of edit objects: %w", err)
	}
	return edits, nil
}

type multiEditTool struct {
	root     string
	notifier ChangeNotifier
	schema   *jsonschema.Schema
}

func newMultiEditTool(root string) *multiEditTool {
	return newMultiEditToolWithNotifier(root, nil)
}

func newMultiEditToolWithNotifier(root string, notifier ChangeNotifier) *multiEditTool {
	return &multiEditTool{
		root:     root,
		notifier: notifier,
		schema:   jsonschema.Reflect(&multiEditArgs{}),
	}
}

func (t *multiEditTool) Name() string { return "multiedit" }
func (t *multiEditTool) Description() string {
	return "Apply multiple edits across one or more workspace files atomically. Same-path edits accumulate in array order."
}
func (t *multiEditTool) Schema() *jsonschema.Schema { return t.schema }
func (t *multiEditTool) Risk() tool.Risk            { return tool.RiskWrite }

// meFile holds the per-file aggregate computed during the planning phase.
type meFile struct {
	abs    string
	rel    string
	before string
	after  string
	count  int
}

// plan parses the args, resolves each path, reads each file once, and
// applies the edits in array order accumulating per-file state. It does
// not touch disk beyond reads, so callers can use it for both Preview
// and Execute.
func (t *multiEditTool) plan(raw json.RawMessage) ([]*meFile, error) {
	var a multiEditArgs
	if err := tool.UnmarshalArgs(raw, &a); err != nil {
		return nil, fmt.Errorf("multiedit: invalid args: %w", err)
	}
	if len(a.Edits) == 0 {
		return nil, fmt.Errorf("multiedit: at least one edit is required")
	}

	files := map[string]*meFile{}
	for i, e := range a.Edits {
		if e.Path == "" {
			return nil, fmt.Errorf("multiedit: edits[%d].path is required", i)
		}
		if e.Old == "" {
			return nil, fmt.Errorf("multiedit: edits[%d].old is required", i)
		}
		abs, err := resolve(t.root, e.Path)
		if err != nil {
			return nil, fmt.Errorf("multiedit: edits[%d]: %w", i, err)
		}
		f, ok := files[abs]
		if !ok {
			rel, _ := relToRoot(t.root, abs)
			b, err := readFileFn(abs)
			if err != nil {
				return nil, fmt.Errorf("multiedit: edits[%d]: read %s: %w", i, rel, err)
			}
			f = &meFile{abs: abs, rel: rel, before: string(b), after: string(b)}
			files[abs] = f
		}
		next, count, err := applyEdit(f.after, e)
		if err != nil {
			return nil, fmt.Errorf("multiedit: edits[%d] (%s): %w", i, f.rel, err)
		}
		f.after = next
		f.count += count
	}

	out := make([]*meFile, 0, len(files))
	for _, f := range files {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].rel < out[j].rel })
	return out, nil
}

func (t *multiEditTool) Preview(_ context.Context, raw json.RawMessage) (tool.Preview, error) {
	files, err := t.plan(raw)
	if err != nil {
		return tool.Preview{}, err
	}
	diffs := make([]tool.FileDiff, len(files))
	for i, f := range files {
		diffs[i] = tool.FileDiff{
			Path:        f.rel,
			Kind:        tool.KindModify,
			UnifiedDiff: udiff.Unified(f.rel, f.rel, f.before, f.after),
		}
	}
	return tool.Preview{
		Summary: fmt.Sprintf("Edit %d file(s)", len(files)),
		Files:   diffs,
	}, nil
}

func (t *multiEditTool) Execute(ctx context.Context, raw json.RawMessage) (tool.Result, error) {
	files, err := t.plan(raw)
	if err != nil {
		return tool.Result{}, err
	}
	// TOCTOU: re-read every target file before any write so a concurrent
	// mutation aborts the whole batch with disk untouched.
	for _, f := range files {
		cur, err := readFileFn(f.abs)
		if err != nil {
			return tool.Result{}, fmt.Errorf("multiedit: re-read %s: %w", f.rel, err)
		}
		if string(cur) != f.before {
			return tool.Result{}, fmt.Errorf("multiedit: %s changed on disk since preview", f.rel)
		}
	}
	// All TOCTOU checks passed; write each file. On a write failure mid-batch,
	// restore the already-written files to their before snapshot before
	// returning the error, so callers see all-or-nothing semantics.
	written := make([]*meFile, 0, len(files))
	for _, f := range files {
		if err := os.WriteFile(f.abs, []byte(f.after), 0o644); err != nil {
			for _, w := range written {
				_ = os.WriteFile(w.abs, []byte(w.before), 0o644)
			}
			return tool.Result{}, fmt.Errorf("multiedit: write %s: %w", f.rel, err)
		}
		written = append(written, f)
	}

	changes := make([]tool.FileChange, len(files))
	var totalCount int
	var notifySuffix strings.Builder
	for i, f := range files {
		changes[i] = tool.FileChange{
			Path:        f.rel,
			Kind:        tool.KindModify,
			UnifiedDiff: udiff.Unified(f.rel, f.rel, f.before, f.after),
		}
		totalCount += f.count
		notifySuffix.WriteString(notifyChanged(ctx, t.notifier, f.abs))
	}
	return tool.Result{
		Content: fmt.Sprintf("multiedit: %d file(s), %d replacement(s)%s", len(files), totalCount, notifySuffix.String()),
		Files:   changes,
	}, nil
}
