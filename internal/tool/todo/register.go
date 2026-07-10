package todo

import (
	"fmt"

	"github.com/feimingxliu/ub/internal/tool"
)

// Register adds the todo_write and todo_update tools to reg. Todo lists are
// session-scoped execution state, stored separately from plan artifacts.
func Register(reg *tool.Registry) error {
	if reg == nil {
		return fmt.Errorf("todo: nil registry")
	}
	for _, t := range []tool.Tool{
		newWriteTool(),
		newUpdateTool(),
	} {
		if err := reg.Register(t); err != nil {
			return err
		}
	}
	return nil
}
