package cli

import (
	"runtime"

	"github.com/feimingxliu/ub/internal/agent"
)

func agentRuntimeContext(workspace string) agent.RuntimeContext {
	return agent.RuntimeContext{
		Workspace: workspace,
		Shell:     "/bin/sh",
		OS:        runtime.GOOS,
	}
}
