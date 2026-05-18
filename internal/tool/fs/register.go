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

// Register adds the five fs tools (read, ls, glob, write, edit) to reg.
// All tools share the same cleaned workspace root.
func Register(reg *tool.Registry, root string) error {
	return RegisterWithNotifier(reg, root, nil)
}

// RegisterWithNotifier adds fs tools and wires write/edit to notify after a
// successful disk mutation.
func RegisterWithNotifier(reg *tool.Registry, root string, notifier ChangeNotifier) error {
	if reg == nil {
		return fmt.Errorf("fs: nil registry")
	}
	if root == "" {
		return fmt.Errorf("fs: empty workspace root")
	}
	root = filepath.Clean(root)

	tools := []tool.Tool{
		newReadTool(root),
		newLsTool(root),
		newGlobTool(root),
		newWriteToolWithNotifier(root, notifier),
		newEditToolWithNotifier(root, notifier),
	}
	for _, t := range tools {
		if err := reg.Register(t); err != nil {
			return err
		}
	}
	return nil
}
