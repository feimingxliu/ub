package task

import (
	"fmt"

	"github.com/feimingxliu/ub/internal/pkg/tool"
)

// Register adds the `task` tool. The tool relies on a SubagentRunner being
// installed on the ctx of every Execute call (the agent runtime is expected
// to do that); registration itself takes no extra arguments.
func Register(reg *tool.Registry) error {
	if reg == nil {
		return fmt.Errorf("task tool: nil registry")
	}
	return reg.Register(newTaskTool())
}
