package shell

import (
	"fmt"
	"path/filepath"

	"github.com/feimingxliu/ub/internal/pkg/tool"
)

// Register adds the bash tool to reg. The workspace root is cleaned
// and used as the default cwd / sandbox boundary for every call.
func Register(reg *tool.Registry, root string) error {
	if reg == nil {
		return fmt.Errorf("shell: nil registry")
	}
	if root == "" {
		return fmt.Errorf("shell: empty workspace root")
	}
	return reg.Register(newBashTool(filepath.Clean(root)))
}
