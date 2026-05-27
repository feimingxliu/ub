package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/feimingxliu/ub/internal/agent"
	"github.com/feimingxliu/ub/internal/config"
	"github.com/feimingxliu/ub/internal/execution"
	"github.com/feimingxliu/ub/internal/permission"
	"github.com/feimingxliu/ub/internal/provider"
	"github.com/feimingxliu/ub/internal/reasoning"
	"github.com/feimingxliu/ub/internal/tool"
)

// cliSubagentRunner dispatches one child agent run for the `task` tool. It
// captures the parent agent's configuration so the child reuses provider
// and tool registry verbatim (independent context is provided by giving
// the child a fresh Request.History).
//
// The child does NOT:
//
//   - share rollout (sub-agent runs are not persisted in this minimum
//     viable version; a future change may give them their own session
//     entry under the parent)
//   - share Hooks (user hooks fire only for the user's own turns)
//   - get its own SubagentRunner, so further nested `task` calls inside
//     the child trip the depth limit cleanly
//   - get an EventSink, so its activity does not noisily mix into the
//     parent TUI
type cliSubagentRunner struct {
	provider         provider.Provider
	tools            *tool.Registry
	permission       *permission.Manager
	model            string
	reasoningCfg     *reasoning.Config
	maxContextTokens int
	contextCfg       config.ContextConfig
	runtime          agent.RuntimeContext
	defaultMaxTurns  int
	workspaceRoot    string
	memoryMaxChars   int
}

func (r *cliSubagentRunner) RunSubagent(ctx context.Context, prompt string, maxTurns int) (string, error) {
	if strings.TrimSpace(prompt) == "" {
		return "", fmt.Errorf("subagent: prompt is required")
	}
	if maxTurns <= 0 {
		maxTurns = r.defaultMaxTurns
	}
	child, err := agent.New(agent.Options{
		Provider:         r.provider,
		Tools:            r.tools,
		Permission:       r.permission,
		Model:            r.model,
		Mode:             execution.ModeWork,
		MaxTurns:         maxTurns,
		Reasoning:        r.reasoningCfg,
		MaxContextTokens: r.maxContextTokens,
		Context:          r.contextCfg,
		Runtime:          r.runtime,
		WorkspaceRoot:    r.workspaceRoot,
		MemoryMaxChars:   r.memoryMaxChars,
	})
	if err != nil {
		return "", fmt.Errorf("subagent: build child: %w", err)
	}
	sessionID := fmt.Sprintf("subagent-%d", time.Now().UnixNano())
	res, err := child.Run(ctx, agent.Request{
		SessionID: sessionID,
		Turn:      1,
		Prompt:    prompt,
	})
	return res.Text, err
}
