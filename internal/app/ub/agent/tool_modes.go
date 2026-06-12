package agent

import (
	"encoding/json"
	"fmt"

	"github.com/feimingxliu/ub/internal/pkg/core/execution"
	"github.com/feimingxliu/ub/internal/pkg/llm/provider"
	"github.com/feimingxliu/ub/internal/pkg/tool"
)

func (a *Agent) currentMode() execution.Mode {
	if a.modeFunc == nil {
		return a.mode
	}
	mode, err := execution.ParseMode(string(a.modeFunc()))
	if err != nil {
		return a.mode
	}
	return mode
}

func (a *Agent) toolDefinitions(mode execution.Mode) ([]provider.ToolDefinition, error) {
	if a == nil {
		return nil, nil
	}
	if a.toolDefinitionCache != nil {
		if defs, ok := a.toolDefinitionCache[mode]; ok {
			return cloneToolDefinitions(defs), nil
		}
	}
	defs, err := toolDefinitions(a.tools, mode)
	if err != nil {
		return nil, err
	}
	if a.toolDefinitionCache == nil {
		a.toolDefinitionCache = map[execution.Mode][]provider.ToolDefinition{}
	}
	a.toolDefinitionCache[mode] = cloneToolDefinitions(defs)
	return cloneToolDefinitions(defs), nil
}

func toolDefinitions(reg *tool.Registry, mode execution.Mode) ([]provider.ToolDefinition, error) {
	tools := reg.All()
	defs := make([]provider.ToolDefinition, 0, len(tools))
	for _, t := range tools {
		if !toolAdvertisedInMode(t, mode) {
			continue
		}
		raw, err := json.Marshal(t.Schema())
		if err != nil {
			return nil, fmt.Errorf("marshal schema for tool %q: %w", t.Name(), err)
		}
		defs = append(defs, provider.ToolDefinition{
			Name:        t.Name(),
			Description: t.Description(),
			Schema:      raw,
		})
	}
	return defs, nil
}

func cloneToolDefinitions(defs []provider.ToolDefinition) []provider.ToolDefinition {
	if defs == nil {
		return nil
	}
	out := make([]provider.ToolDefinition, len(defs))
	for i, def := range defs {
		out[i] = provider.ToolDefinition{
			Name:        def.Name,
			Description: def.Description,
			Schema:      cloneRaw(def.Schema),
		}
	}
	return out
}

func toolAdvertisedInMode(t tool.Tool, mode execution.Mode) bool {
	if t == nil || !toolAvailableInMode(t.Name(), mode) {
		return false
	}
	parsed, err := execution.ParseMode(string(mode))
	if err != nil {
		parsed = execution.ModeWork
	}
	return parsed != execution.ModePlan || t.Risk() != tool.RiskWrite
}

func toolAvailableInMode(name string, mode execution.Mode) bool {
	parsed, err := execution.ParseMode(string(mode))
	if err != nil {
		parsed = execution.ModeWork
	}
	if parsed == execution.ModePlan {
		return toolAllowedInPlanMode(name)
	}
	switch name {
	case "plan_write", "plan_update", "exit_plan_mode":
		return false
	case "enter_plan_mode":
		return parsed == execution.ModeWork
	default:
		return true
	}
}

func toolAllowedInPlanMode(name string) bool {
	switch name {
	case "read", "ls", "glob", "grep", "ask", "plan_write", "plan_update", "exit_plan_mode":
		return true
	default:
		return false
	}
}

func toolUnavailableInModeMessage(name string, mode execution.Mode) string {
	parsed, err := execution.ParseMode(string(mode))
	if err != nil {
		parsed = execution.ModeWork
	}
	if parsed == execution.ModePlan {
		return fmt.Sprintf("tool %q is not available in plan mode; use read, ls, glob, grep, ask, plan_write, plan_update, or exit_plan_mode", name)
	}
	if name == "plan_write" || name == "plan_update" {
		return fmt.Sprintf("tool %q is only available in plan mode", name)
	}
	if name == "exit_plan_mode" {
		return fmt.Sprintf("tool %q is only available in plan mode", name)
	}
	if name == "enter_plan_mode" {
		return fmt.Sprintf("tool %q is only available in work mode", name)
	}
	return fmt.Sprintf("tool %q is not available in %s mode", name, parsed)
}
