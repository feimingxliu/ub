package agent

import (
	"fmt"
	"strings"

	"github.com/feimingxliu/ub/internal/pkg/core/config"
	"github.com/feimingxliu/ub/internal/pkg/core/execution"
	"github.com/feimingxliu/ub/internal/pkg/core/message"
	"github.com/feimingxliu/ub/internal/pkg/workspace/memory"
)

// RuntimeContext describes the local execution environment for one agent run.
// It is sent to providers on every request but is not persisted in rollout
// history.
type RuntimeContext struct {
	Workspace string
	Shell     string
	OS        string
}

func (c RuntimeContext) normalized() RuntimeContext {
	return RuntimeContext{
		Workspace: strings.TrimSpace(c.Workspace),
		Shell:     strings.TrimSpace(c.Shell),
		OS:        strings.TrimSpace(c.OS),
	}
}

func (a *Agent) withRuntimeContext(messages []message.Message) []message.Message {
	// Callers pass an owned message slice when building provider requests.
	// Keep the returned request slice separate, but avoid deep-cloning the
	// entire tail here; the provider boundary still clones before Chat.
	prepend := promptMessages(a.promptRegistry.mainSections(a.currentMode()))
	if len(prepend) == 0 {
		return messages
	}
	out := make([]message.Message, 0, len(prepend)+len(messages))
	out = append(out, prepend...)
	return append(out, messages...)
}

// RuntimeContextMessages builds the same non-persisted request context prefix
// used by the agent loop. Hosts can use it for read-only provider requests that
// should share the main conversation's stable prompt prefix without recording a
// new user turn.
func RuntimeContextMessages(runtime RuntimeContext, workspaceRoot string, promptCfg config.PromptConfig, mode execution.Mode, memoryMaxChars int) []message.Message {
	registry := newPromptRegistry(runtime, workspaceRoot, promptCfg, memoryMaxChars)
	return promptMessages(registry.mainSections(mode))
}

// NoToolRuntimeContextMessages builds a small non-persisted context prefix for
// provider requests that intentionally cannot call tools. It keeps environment
// and memory context, but omits coding-agent, workspace, git, and mode prompts
// that mention tool use.
func NoToolRuntimeContextMessages(runtime RuntimeContext, workspaceRoot string, memoryMaxChars int) []message.Message {
	registry := newNoToolPromptRegistry(runtime, workspaceRoot, memoryMaxChars)
	return promptMessages(registry.noToolSections())
}

func memoryMessageForWorkspace(workspaceRoot string, maxChars int) (message.Message, bool) {
	if strings.TrimSpace(workspaceRoot) == "" {
		return message.Message{}, false
	}
	budget := maxChars
	if budget <= 0 {
		budget = memory.DefaultReadMaxChars
	}
	body := memory.Read(workspaceRoot, budget)
	if strings.TrimSpace(body) == "" {
		return message.Message{}, false
	}
	return message.Text(message.RoleSystem, "<memory>\n"+body+"\n</memory>"), true
}

func executionModeMessageForMode(value execution.Mode) (message.Message, bool) {
	mode, err := execution.ParseMode(string(value))
	if err != nil || mode != execution.ModePlan {
		return message.Message{}, false
	}
	const body = `<execution_mode>
mode=plan
</execution_mode>
Plan mode instructions:
- This is read-only planning mode. Inspect the workspace only with read, ls, glob, and grep when needed.
- For implementation requests such as add, fix, refactor, configure, test, build, or CI setup, create a plan with the plan_write tool before starting implementation.
- If a plan already exists and the user corrects or changes it, update that same plan with plan_update instead of creating another plan.
- Do not create, edit, delete, move, format, install, execute commands, launch sub-agents, or otherwise change project files in plan mode.
- After writing or updating the plan, call exit_plan_mode with the plan_id and a concise summary to ask the user to approve the plan. If denied, revise the same plan with plan_update.
- If the user only asks a question, answer normally; use plan_write or plan_update only when a persistent execution plan is useful.`
	return message.Text(message.RoleSystem, body), true
}

func (c RuntimeContext) message() (message.Message, bool) {
	if c.Workspace == "" {
		return message.Message{}, false
	}
	var b strings.Builder
	b.WriteString("<environment_context>\n")
	b.WriteString(fmt.Sprintf("  <cwd>%s</cwd>\n", xmlEscape(c.Workspace)))
	if c.Shell != "" {
		b.WriteString(fmt.Sprintf("  <shell>%s</shell>\n", xmlEscape(c.Shell)))
	}
	if c.OS != "" {
		b.WriteString(fmt.Sprintf("  <os>%s</os>\n", xmlEscape(c.OS)))
	}
	b.WriteString("</environment_context>\n")
	b.WriteString("Path rules:\n")
	b.WriteString("- File and search tool paths are relative to the current workspace unless an absolute path is explicitly inside it.\n")
	b.WriteString("- Use read only for regular files. Use ls or glob for directories, and use ls first when the path type is unknown.\n")
	b.WriteString("- Shell commands run from the current workspace by default; use the cwd parameter for subdirectories instead of `cd ... && ...`.\n")
	b.WriteString("- Do not invent alternate project paths such as /home/user. Use pwd or ls if the workspace appears inconsistent.")
	return message.Text(message.RoleSystem, b.String()), true
}

func (c RuntimeContext) noToolMessage() (message.Message, bool) {
	c = c.normalized()
	if c.Workspace == "" && c.Shell == "" && c.OS == "" {
		return message.Message{}, false
	}
	var b strings.Builder
	b.WriteString("<environment_context>\n")
	if c.Workspace != "" {
		b.WriteString(fmt.Sprintf("  <cwd>%s</cwd>\n", xmlEscape(c.Workspace)))
	}
	if c.Shell != "" {
		b.WriteString(fmt.Sprintf("  <shell>%s</shell>\n", xmlEscape(c.Shell)))
	}
	if c.OS != "" {
		b.WriteString(fmt.Sprintf("  <os>%s</os>\n", xmlEscape(c.OS)))
	}
	b.WriteString("</environment_context>\n")
	b.WriteString("No-tool context rules:\n")
	b.WriteString("- This context is informational only. No tools are available in this request.\n")
	b.WriteString("- Do not emit tool calls, tool-call JSON, XML tool tags, command blocks, or requests for tool results.")
	return message.Text(message.RoleSystem, b.String()), true
}

func xmlEscape(s string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&apos;",
	)
	return replacer.Replace(s)
}
