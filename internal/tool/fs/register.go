package fs

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/feimingxliu/ub/internal/tool"
)

// ChangeNotifier receives successful filesystem write/edit notifications.
type ChangeNotifier interface {
	DidChangeFile(ctx context.Context, absPath string) error
}

// Options controls read-only state paths and model-facing read defaults.
type Options struct {
	StateRoot      string
	ReadMaxLines   int
	ChangeNotifier ChangeNotifier
}

// Register adds the five fs tools (read, ls, glob, write, edit) to reg.
// All tools share the same cleaned workspace root.
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

	tools := []tool.Tool{
		newReadToolWithOptions(root, opts.StateRoot, opts.ReadMaxLines),
		newLsTool(root),
		newGlobTool(root),
		newWriteToolWithNotifier(root, opts.ChangeNotifier),
		newEditToolWithNotifier(root, opts.ChangeNotifier),
	}
	for _, t := range tools {
		if err := reg.Register(t); err != nil {
			return err
		}
	}
	return nil
}
