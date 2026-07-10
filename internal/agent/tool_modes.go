package agent

import (
	"encoding/json"
	"fmt"

	execmode "github.com/feimingxliu/ub/internal/mode"
	"github.com/feimingxliu/ub/internal/provider"
	"github.com/feimingxliu/ub/internal/tool"
)

// currentMode returns the effective execution execmode. If modeFunc is set
// (used by the TUI for live mode switching), it takes precedence over the
// static mode field. Parsing failures fall back to the stored execmode.
func (a *Agent) currentMode() execmode.Mode {
	if a.modeFunc == nil {
		return a.mode
	}
	mode, err := execmode.ParseMode(string(a.modeFunc()))
	if err != nil {
		return a.mode
	}
	return mode
}

// toolDefinitions returns the provider-facing tool definitions for the
// given mode, filtered by mode availability. Results are cached per mode
// and cloned on return so callers cannot mutate the cache.
func (a *Agent) toolDefinitions(mode execmode.Mode) ([]provider.ToolDefinition, error) {
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
		a.toolDefinitionCache = map[execmode.Mode][]provider.ToolDefinition{}
	}
	a.toolDefinitionCache[mode] = cloneToolDefinitions(defs)
	return cloneToolDefinitions(defs), nil
}

// toolDefinitions builds the tool definition list from the registry,
// filtering out tools not advertised in the given mode (e.g. write tools
// in plan mode). Each tool's JSON schema is marshalled once.
func toolDefinitions(reg *tool.Registry, mode execmode.Mode) ([]provider.ToolDefinition, error) {
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

// toolAdvertisedInMode reports whether a tool should be shown to the model
// in the given execmode. In plan mode, write-risk tools are hidden from the
// advertised list so the model does not attempt to use them.
func toolAdvertisedInMode(t tool.Tool, mode execmode.Mode) bool {
	if t == nil || !toolAvailableInMode(t.Name(), mode) {
		return false
	}
	parsed, err := execmode.ParseMode(string(mode))
	if err != nil {
		parsed = execmode.ModeWork
	}
	return parsed != execmode.ModePlan || t.Risk() != tool.RiskWrite
}

// toolAvailableInMode reports whether a tool may be executed in the given
// execmode. Unlike toolAdvertisedInMode, this is checked at execution time to
// guard against the model calling a tool that was hidden from advertising.
func toolAvailableInMode(name string, mode execmode.Mode) bool {
	parsed, err := execmode.ParseMode(string(mode))
	if err != nil {
		parsed = execmode.ModeWork
	}
	if parsed == execmode.ModePlan {
		return toolAllowedInPlanMode(name)
	}
	switch name {
	case "plan_write", "plan_update", "exit_plan_mode":
		return false
	case "enter_plan_mode":
		return parsed == execmode.ModeWork
	default:
		return true
	}
}

func toolAllowedInPlanMode(name string) bool {
	switch name {
	case "read", "ls", "glob", "grep", "ask", "plan_write", "plan_update", "exit_plan_mode", "create_goal", "update_goal", "get_goal":
		return true
	default:
		return false
	}
}

func toolUnavailableInModeMessage(name string, mode execmode.Mode) string {
	parsed, err := execmode.ParseMode(string(mode))
	if err != nil {
		parsed = execmode.ModeWork
	}
	if parsed == execmode.ModePlan {
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
