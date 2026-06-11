package fs

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/feimingxliu/ub/internal/pkg/tool"
)

// ChangeNotifier receives successful filesystem write/edit notifications.
type ChangeNotifier interface {
	DidChangeFile(ctx context.Context, absPath string) error
}

// Options controls read-only state paths and model-facing read defaults.
//
// StateRoot historically holds the spillover output root (i.e.
// <state-root>/tool_outputs). OutputRoot is the preferred field with the
// same meaning; when empty, Register falls back to StateRoot.
type Options struct {
	StateRoot      string
	OutputRoot     string
	ReadMaxLines   int
	ChangeNotifier ChangeNotifier
}

// Register adds the six base fs tools (read, ls, glob, write, edit, multiedit)
// to reg. All tools share the same cleaned workspace root. When
// RegisterWithOptions receives a non-empty OutputRoot (or StateRoot fallback),
// a seventh tool, tool_result, is also registered so agents can replay
// spilled tool outputs by tool_use_id.
func Register(reg *tool.Registry, root string) error {
	return RegisterWithNotifier(reg, root, nil)
}

// RegisterWithNotifier adds fs tools and wires write/edit to notify after a
// successful disk mutation.
func RegisterWithNotifier(reg *tool.Registry, root string, notifier ChangeNotifier) error {
	return RegisterWithOptions(reg, root, Options{ChangeNotifier: notifier})
}

// RegisterWithOptions adds fs tools with optional read access to ub state.
func RegisterWithOptions(reg *tool.Registry, root string, opts Options) error {
	if reg == nil {
		return fmt.Errorf("fs: nil registry")
	}
	if root == "" {
		return fmt.Errorf("fs: empty workspace root")
	}
	root = filepath.Clean(root)

	outputRoot := strings.TrimSpace(opts.OutputRoot)
	if outputRoot == "" {
		outputRoot = strings.TrimSpace(opts.StateRoot)
	}
	tools := []tool.Tool{
		newReadToolWithOptions(root, outputRoot, opts.ReadMaxLines),
		newLsTool(root),
		newGlobTool(root),
		newWriteToolWithNotifier(root, opts.ChangeNotifier),
		newEditToolWithNotifier(root, opts.ChangeNotifier),
		newMultiEditToolWithNotifier(root, opts.ChangeNotifier),
	}
	if outputRoot != "" {
		tools = append(tools, newToolResultTool(outputRoot, opts.ReadMaxLines))
	}
	for _, t := range tools {
		if err := reg.Register(t); err != nil {
			return err
		}
	}
	return nil
}
