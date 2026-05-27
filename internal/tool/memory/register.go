package memory

import (
	"fmt"
	"path/filepath"

	"github.com/feimingxliu/ub/internal/tool"
)

// Register adds the `remember` tool to reg, scoped to workspaceRoot for the
// workspace memory scope. workspaceRoot may be empty, in which case the
// tool still registers and `remember(scope=global)` keeps working; only
// `scope=workspace` calls will fail.
func Register(reg *tool.Registry, workspaceRoot string) error {
	if reg == nil {
		return fmt.Errorf("memory tool: nil registry")
	}
	root := workspaceRoot
	if root != "" {
		root = filepath.Clean(root)
	}
	return reg.Register(newRememberTool(root))
}
