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
	out := cloneMessages(messages)
	prepend := cloneMessages(a.startupPrompt)
	if modeMsg, ok := a.executionModeMessage(); ok {
		prepend = append(prepend, modeMsg)
	}
	if memMsg, ok := a.memoryMessage(); ok {
		prepend = append(prepend, memMsg)
	}
	if len(prepend) == 0 {
		return out
	}
	return append(prepend, out...)
}

// RuntimeContextMessages builds the same non-persisted request context prefix
// used by the agent loop. Hosts can use it for read-only provider requests that
// should share the main conversation's stable prompt prefix without recording a
// new user turn.
func RuntimeContextMessages(runtime RuntimeContext, workspaceRoot string, promptCfg config.PromptConfig, mode execution.Mode, memoryMaxChars int) []message.Message {
	out := buildStartupPromptMessages(runtime, workspaceRoot, promptCfg)
	if modeMsg, ok := executionModeMessageForMode(mode); ok {
		out = append(out, modeMsg)
	}
	if memMsg, ok := memoryMessageForWorkspace(workspaceRoot, memoryMaxChars); ok {
		out = append(out, memMsg)
	}
	return out
}

// NoToolRuntimeContextMessages builds a small non-persisted context prefix for
// provider requests that intentionally cannot call tools. It keeps environment
// and memory context, but omits coding-agent, workspace, git, and mode prompts
// that mention tool use.
func NoToolRuntimeContextMessages(runtime RuntimeContext, workspaceRoot string, memoryMaxChars int) []message.Message {
	var out []message.Message
	if runtimeMsg, ok := runtime.noToolMessage(); ok {
		out = append(out, runtimeMsg)
	}
	if memMsg, ok := memoryMessageForWorkspace(workspaceRoot, memoryMaxChars); ok {
		out = append(out, memMsg)
	}
	return out
}

// memoryMessage returns a role=system message containing the
// <memory>...</memory> envelope of the current memory sources.
// Returns ok=false when the agent has no workspace configured or
// when no memory content exists.
func (a *Agent) memoryMessage() (message.Message, bool) {
	return memoryMessageForWorkspace(a.workspaceRoot, a.memoryMaxChars)
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

func (a *Agent) executionModeMessage() (message.Message, bool) {
	return executionModeMessageForMode(a.currentMode())
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
- After writing the plan, report the plan_id and wait for the user to switch to work or auto mode before executing it.
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
