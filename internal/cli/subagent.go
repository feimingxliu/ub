package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/feimingxliu/ub/internal/agent"
	"github.com/feimingxliu/ub/internal/config"
	"github.com/feimingxliu/ub/internal/execution"
	"github.com/feimingxliu/ub/internal/hook"
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
//   - run user-turn hooks (tool hooks are inherited so blocking policies still
//     apply to child tool calls)
//   - get an EventSink, so its activity does not noisily mix into the
//     parent TUI
type cliSubagentRunner struct {
	provider         provider.Provider
	tools            *tool.Registry
	permission       *permission.Manager
	model            string
	mode             execution.Mode
	modeFunc         func() execution.Mode
	reasoningCfg     *reasoning.Config
	maxContextTokens int
	contextCfg       config.ContextConfig
	promptCfg        config.PromptConfig
	runtime          agent.RuntimeContext
	hooks            hook.Runner
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
		Mode:             r.currentMode(),
		ModeFunc:         r.modeFunc,
		MaxTurns:         maxTurns,
		Reasoning:        r.reasoningCfg,
		MaxContextTokens: r.maxContextTokens,
		Context:          r.contextCfg,
		Prompt:           r.promptCfg,
		Runtime:          r.runtime,
		Hooks:            subagentHookRunner{inner: r.hooks},
		WorkspaceRoot:    r.workspaceRoot,
		MemoryMaxChars:   r.memoryMaxChars,
		SubagentRunner:   r,
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

func (r *cliSubagentRunner) currentMode() execution.Mode {
	if r == nil {
		return execution.ModeWork
	}
	if r.modeFunc != nil {
		if mode, err := execution.ParseMode(string(r.modeFunc())); err == nil {
			return mode
		}
	}
	if mode, err := execution.ParseMode(string(r.mode)); err == nil {
		return mode
	}
	return execution.ModeWork
}

type subagentHookRunner struct {
	inner hook.Runner
}

func (r subagentHookRunner) Run(ctx context.Context, event hook.Event) hook.Decision {
	if r.inner == nil {
		return hook.Decision{}
	}
	switch event.Kind {
	case hook.KindPreToolCall, hook.KindPostToolCall:
		return r.inner.Run(ctx, event)
	default:
		return hook.Decision{}
	}
}
