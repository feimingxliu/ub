package plan

import (
	"fmt"
	"path/filepath"

	"github.com/feimingxliu/ub/internal/pkg/tool"
)

// Register adds the plan_write, plan_update, and plan_update_step tools to
// reg. Plan files are stored under $XDG_STATE_HOME/ub/plans/<project-key>/.
func Register(reg *tool.Registry, workspaceRoot string) error {
	if reg == nil {
		return fmt.Errorf("plan: nil registry")
	}
	if workspaceRoot == "" {
		return fmt.Errorf("plan: empty workspace root")
	}
	root := filepath.Clean(workspaceRoot)
	for _, t := range []tool.Tool{
		newWriteTool(root),
		newReviseTool(root),
		newUpdateTool(root),
	} {
		if err := reg.Register(t); err != nil {
			return err
		}
	}
	return nil
}
