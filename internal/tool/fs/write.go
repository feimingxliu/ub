package fs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	udiff "github.com/aymanbagabas/go-udiff"
	"github.com/invopop/jsonschema"

	"github.com/feimingxliu/ub/internal/tool"
)

type writeArgs struct {
	Path    string `json:"path"    jsonschema:"required,description=Path relative to workspace root (parent dirs are created)."`
	Content string `json:"content" jsonschema:"required,description=New file content. The file is overwritten."`
}

type writeTool struct {
	root     string
	notifier ChangeNotifier
	schema   *jsonschema.Schema
}

func newWriteTool(root string) *writeTool {
	return newWriteToolWithNotifier(root, nil)
}

func newWriteToolWithNotifier(root string, notifier ChangeNotifier) *writeTool {
	return &writeTool{
		root:     root,
		notifier: notifier,
		schema:   jsonschema.Reflect(&writeArgs{}),
	}
}

func (t *writeTool) Name() string { return "write" }
func (t *writeTool) Description() string {
	return "Write a file in the workspace, creating parent directories as needed."
}
func (t *writeTool) Schema() *jsonschema.Schema { return t.schema }
func (t *writeTool) Risk() tool.Risk            { return tool.RiskWrite }

func (t *writeTool) parseAndResolve(raw json.RawMessage) (writeArgs, string, error) {
	var a writeArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return a, "", fmt.Errorf("write: invalid args: %w", err)
	}
	if a.Path == "" {
		return a, "", fmt.Errorf("write: path is required")
	}
	abs, err := resolve(t.root, a.Path)
	if err != nil {
		return a, "", err
	}
	return a, abs, nil
}

func (t *writeTool) Preview(_ context.Context, raw json.RawMessage) (tool.Preview, error) {
	a, abs, err := t.parseAndResolve(raw)
	if err != nil {
		return tool.Preview{}, err
	}
	rel, _ := relToRoot(t.root, abs)

	old, kind, err := readForDiff(abs)
	if err != nil {
		return tool.Preview{}, err
	}

	diff := udiff.Unified(rel, rel, old, a.Content)
	summary := fmt.Sprintf("Write %s: %s", rel, kind)
	return tool.Preview{
		Summary: summary,
		Files: []tool.FileDiff{{
			Path:        rel,
			Kind:        kind,
			UnifiedDiff: diff,
		}},
	}, nil
}

func (t *writeTool) Execute(ctx context.Context, raw json.RawMessage) (tool.Result, error) {
	a, abs, err := t.parseAndResolve(raw)
	if err != nil {
		return tool.Result{}, err
	}
	rel, _ := relToRoot(t.root, abs)

	_, kind, err := readForDiff(abs)
	if err != nil {
		return tool.Result{}, err
	}

	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return tool.Result{}, fmt.Errorf("write: mkdir %s: %w", filepath.Dir(rel), err)
	}
	if err := os.WriteFile(abs, []byte(a.Content), 0o644); err != nil {
		return tool.Result{}, fmt.Errorf("write: %s: %w", rel, err)
	}
	notifySuffix := notifyChanged(ctx, t.notifier, abs)
	return tool.Result{
		Content: fmt.Sprintf("wrote %s (%d bytes)%s", rel, len(a.Content), notifySuffix),
		Files: []tool.FileChange{{
			Path: rel,
			Kind: kind,
		}},
	}, nil
}

// readForDiff returns the current on-disk content (empty string if the
// file does not yet exist) and the FileDiff Kind to use. An os error
// other than "not exist" is propagated.
func readForDiff(abs string) (string, string, error) {
	buf, err := os.ReadFile(abs)
	if err == nil {
		return string(buf), tool.KindModify, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return "", tool.KindCreate, nil
	}
	return "", "", fmt.Errorf("read existing: %w", err)
}
