// Package slash parses TUI slash commands.
package slash

import (
	"errors"
	"fmt"
	"strings"
)

// Command is one parsed slash command.
type Command struct {
	Name string
	Args []string
	Raw  string
}

// Spec describes a supported slash command for help and completions.
type Spec struct {
	Name        string
	Usage       string
	Description string
}

var specs = []Spec{
	{Name: "provider", Usage: "/provider [provider] [model]", Description: "show providers or switch provider and optional model"},
	{Name: "model", Usage: "/model [model]", Description: "show models or switch to a supported model"},
	{Name: "effort", Usage: "/effort [effort]", Description: "show or switch reasoning effort for the current model"},
	{Name: "approval-model", Usage: "/approval-model [model]", Description: "show or switch the auto approval model"},
	{Name: "small-model", Usage: "/small-model [model]", Description: "show or switch the auto memory model"},
	{Name: "mode", Usage: "/mode <work|plan|auto|full-access>", Description: "switch execution mode"},
	{Name: "compact", Usage: "/compact", Description: "compact earlier session context"},
	{Name: "goal", Usage: "/goal [objective|clear]", Description: "show current goal, set a new objective, or clear the goal"},
	{Name: "init", Usage: "/init [guidance]", Description: "run an agent pass to create or update AGENTS.md"},
	{Name: "plans", Usage: "/plans [plan-id]", Description: "show plan artifacts or open one in $EDITOR"},
	{Name: "plan-edit", Usage: "/plan-edit <plan-id>", Description: "open a plan artifact in $EDITOR"},
	{Name: "doctor", Usage: "/doctor", Description: "run health checks in the TUI"},
	{Name: "retry", Usage: "/retry", Description: "rerun the last user turn"},
	{Name: "rewind", Usage: "/rewind [turn]", Description: "choose a prior user turn, rewind to before it, and restore it in the input"},
	{Name: "btw", Usage: "/btw [question]", Description: "open the BTW side chat or ask without adding it to the main transcript"},
	{Name: "resume", Usage: "/resume [session-id]", Description: "resume a historical session"},
	{Name: "copy", Usage: "/copy [N]", Description: "copy last response to clipboard, or message N (shown as [N] in transcript)"},
	{Name: "clear", Usage: "/clear", Description: "clear the conversation view"},
	{Name: "new", Usage: "/new", Description: "start a new empty session"},
	{Name: "sessions", Usage: "/sessions [session-id|search <query>]", Description: "show session picker, switch session, or search rollout text across sessions"},
	{Name: "help", Usage: "/help", Description: "show slash command help"},
	{Name: "quit", Usage: "/quit", Description: "exit the TUI"},
	{Name: "exit", Usage: "/exit", Description: "exit the TUI"},
	{Name: "config", Usage: "/config", Description: "show current model, mode, and cwd"},
	{Name: "profile", Usage: "/profile <name>", Description: "show profile restart guidance"},
}

var supported = supportedSet(specs)

// Parse parses input that starts with '/'.
func Parse(input string) (Command, error) {
	raw := strings.TrimSpace(input)
	if !strings.HasPrefix(raw, "/") {
		return Command{}, errors.New("slash command must start with /")
	}
	trimmed := strings.TrimSpace(strings.TrimPrefix(raw, "/"))
	if trimmed == "" {
		return Command{}, errors.New("slash command required")
	}
	fields := strings.Fields(trimmed)
	name := strings.ToLower(fields[0])
	if _, ok := supported[name]; !ok {
		return Command{}, fmt.Errorf("unknown slash command %q", name)
	}
	return Command{Name: name, Args: append([]string(nil), fields[1:]...), Raw: raw}, nil
}

// Supported returns the supported command names in display order.
func Supported() []string {
	out := make([]string, 0, len(specs))
	for _, spec := range specs {
		out = append(out, spec.Name)
	}
	return out
}

// Specs returns command metadata in display order.
func Specs() []Spec {
	return append([]Spec(nil), specs...)
}

// Match returns command metadata whose name has the supplied prefix.
func Match(prefix string) []Spec {
	prefix = strings.TrimPrefix(strings.TrimSpace(prefix), "/")
	if strings.Contains(prefix, " ") {
		prefix = strings.Fields(prefix)[0]
	}
	prefix = strings.ToLower(prefix)
	var out []Spec
	for _, spec := range specs {
		if prefix == "" || strings.HasPrefix(spec.Name, prefix) {
			out = append(out, spec)
		}
	}
	return out
}

func supportedSet(specs []Spec) map[string]struct{} {
	out := make(map[string]struct{}, len(specs))
	for _, spec := range specs {
		out[spec.Name] = struct{}{}
	}
	return out
}
