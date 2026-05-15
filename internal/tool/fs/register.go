package fs

import (
	"fmt"
	"path/filepath"

	"github.com/feimingxliu/ub/internal/tool"
)

// Register adds the five fs tools (read, ls, glob, write, edit) to reg.
// All tools share the same cleaned workspace root.
func Register(reg *tool.Registry, root string) error {
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
		newWriteTool(root),
		newEditTool(root),
	}
	for _, t := range tools {
		if err := reg.Register(t); err != nil {
			return err
		}
	}
	return nil
}
