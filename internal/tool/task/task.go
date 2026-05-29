// Package task implements the `task` tool: dispatch a sub-agent to do one
// focused sub-prompt (e.g. "explore module X and report") and return the
// child's final answer to the parent. The sub-agent runs in the same
// process and reuses the parent's provider + tool registry; depth is
// capped at 1 in this minimum-viable version to avoid recursive token
// blowups.
package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/invopop/jsonschema"

	"github.com/feimingxliu/ub/internal/tool"
)

// MaxSubagentDepth caps how many task calls can stack on one another.
// Beyond this, the tool refuses the call. The current value is 1 (root
// agent + one sub-agent); the limit will relax when the agent loop
// decoupling work (roadmap §4-01) lets us reason about token budgets per
// nested run.
const MaxSubagentDepth = 1

type taskArgs struct {
	Prompt   string      `json:"prompt"            jsonschema:"required,description=Self-contained instructions for the sub-agent. The sub-agent has the same tools as you but a fresh context."`
	MaxTurns tool.IntArg `json:"max_turns,omitempty" jsonschema:"description=Optional cap on the sub-agent's tool-call iterations. Defaults to the sub-agent's configured maxTurns."`
}

type taskTool struct {
	schema *jsonschema.Schema
}

func newTaskTool() *taskTool {
	return &taskTool{schema: jsonschema.Reflect(&taskArgs{})}
}

func (t *taskTool) Name() string { return "task" }
func (t *taskTool) Description() string {
	return "Dispatch a sub-agent to run one focused, self-contained research or exploration prompt and return its final answer. Use when a parallel investigation can stay independent from the main context, such as surveying a module or summarizing call sites. Do not use for small local reads the main agent can do directly. Include the exact scope, desired output, and constraints; recursive task calls are rejected."
}
func (t *taskTool) Schema() *jsonschema.Schema { return t.schema }
func (t *taskTool) Risk() tool.Risk            { return tool.RiskSafe }

func (t *taskTool) Execute(ctx context.Context, raw json.RawMessage) (tool.Result, error) {
	var a taskArgs
	if err := tool.UnmarshalArgs(raw, &a); err != nil {
		return tool.Result{}, fmt.Errorf("task: invalid args: %w", err)
	}
	if strings.TrimSpace(a.Prompt) == "" {
		return tool.Result{}, errors.New("task: prompt is required")
	}
	runner := tool.SubagentRunnerFromContext(ctx)
	if runner == nil {
		return tool.Result{}, errors.New("task: subagent runner not configured")
	}
	depth := tool.SubagentDepthFromContext(ctx)
	if depth >= MaxSubagentDepth {
		return tool.Result{}, fmt.Errorf("task: max subagent depth (%d) reached", MaxSubagentDepth)
	}
	childCtx := tool.WithSubagentDepth(ctx, depth+1)
	answer, err := runner.RunSubagent(childCtx, a.Prompt, int(a.MaxTurns))
	if err != nil {
		return tool.Result{
			Content: fmt.Sprintf("subagent failed: %v\n--- partial output ---\n%s", err, answer),
			IsError: true,
		}, nil
	}
	if answer == "" {
		answer = "(subagent returned no text)"
	}
	return tool.Result{Content: answer}, nil
}
