package memory

import (
	"fmt"
	"path/filepath"

	"github.com/feimingxliu/ub/internal/pkg/tool"
)

// Register adds the `remember` and `recall` tools to reg, scoped to
// workspaceRoot for the auto memory scope. workspaceRoot may be empty, in
// which case the tools still register and `remember(scope=global)` keeps
// working; only `scope=auto` calls will fail.
func Register(reg *tool.Registry, workspaceRoot string) error {
	if reg == nil {
		return fmt.Errorf("memory tool: nil registry")
	}
	root := workspaceRoot
	if root != "" {
		root = filepath.Clean(root)
	}
	for _, t := range []tool.Tool{
		newRememberTool(root),
		newRecallTool(root),
	} {
		if err := reg.Register(t); err != nil {
			return err
		}
	}
	return nil
}
