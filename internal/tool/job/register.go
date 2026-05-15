package job

import (
	"fmt"
	"path/filepath"

	"github.com/feimingxliu/ub/internal/tool"
)

// Register creates a fresh Manager bound to root and adds job_run /
// job_output / job_kill to reg, all sharing that Manager.
func Register(reg *tool.Registry, root string) error {
	if reg == nil {
		return fmt.Errorf("job: nil registry")
	}
	if root == "" {
		return fmt.Errorf("job: empty workspace root")
	}
	mgr := NewManager(filepath.Clean(root))
	for _, t := range []tool.Tool{
		newRunTool(mgr),
		newOutputTool(mgr),
		newKillTool(mgr),
	} {
		if err := reg.Register(t); err != nil {
			return err
		}
	}
	return nil
}
