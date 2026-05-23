package job

import (
	"fmt"
	"path/filepath"

	"github.com/feimingxliu/ub/internal/tool"
)

// Register creates a fresh Manager bound to root and adds job_run /
// job_output / job_kill to reg, all sharing that Manager.
func Register(reg *tool.Registry, root string) error {
	_, err := RegisterWithOptions(reg, root, ManagerOptions{})
	return err
}

// RegisterWithOptions creates a Manager with lifecycle options and registers
// job_run / job_output / job_kill to reg.
func RegisterWithOptions(reg *tool.Registry, root string, opts ManagerOptions) (*Manager, error) {
	if reg == nil {
		return nil, fmt.Errorf("job: nil registry")
	}
	if root == "" {
		return nil, fmt.Errorf("job: empty workspace root")
	}
	mgr := NewManagerWithOptions(filepath.Clean(root), opts)
	for _, t := range []tool.Tool{
		newRunTool(mgr),
		newOutputTool(mgr),
		newKillTool(mgr),
	} {
		if err := reg.Register(t); err != nil {
			_ = mgr.Shutdown(nil)
			return nil, err
		}
	}
	return mgr, nil
}
