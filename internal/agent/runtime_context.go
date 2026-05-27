package agent

import (
	"fmt"
	"strings"

	"github.com/feimingxliu/ub/internal/execution"
	"github.com/feimingxliu/ub/internal/memory"
	"github.com/feimingxliu/ub/internal/message"
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
	var prepend []message.Message
	if runtimeMsg, ok := a.runtime.message(); ok {
		prepend = append(prepend, runtimeMsg)
	}
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

// memoryMessage returns a role=system message containing the
// <workspace_memory>...</workspace_memory> envelope of the current memory
// files. Returns ok=false when the agent has no workspace configured or
// when neither memory file has content.
func (a *Agent) memoryMessage() (message.Message, bool) {
	if a.workspaceRoot == "" {
		return message.Message{}, false
	}
	budget := a.memoryMaxChars
	if budget <= 0 {
		budget = memory.DefaultReadMaxChars
	}
	body := memory.Read(a.workspaceRoot, budget)
	if strings.TrimSpace(body) == "" {
		return message.Message{}, false
	}
	return message.Text(message.RoleSystem, "<workspace_memory>\n"+body+"\n</workspace_memory>"), true
}

func (a *Agent) executionModeMessage() (message.Message, bool) {
	mode, err := execution.ParseMode(string(a.currentMode()))
	if err != nil || mode != execution.ModePlan {
		return message.Message{}, false
	}
	const body = `<execution_mode>
mode=plan
</execution_mode>
Plan mode instructions:
- This is read-only planning mode. Inspect the workspace with safe read-only tools when needed.
- For implementation requests such as add, fix, refactor, configure, test, build, or CI setup, create a plan with the plan_write tool before starting implementation.
- Do not create, edit, delete, move, format, install, or otherwise change project files in plan mode.
- After writing the plan, report the plan_id and wait for the user to switch to work or auto mode before executing it.
- If the user only asks a question, answer normally; use plan_write only when an execution plan is useful.`
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
