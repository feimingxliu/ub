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
	{Name: "model", Usage: "/model [model]", Description: "show models or switch to a supported model"},
	{Name: "mode", Usage: "/mode <work|plan|auto>", Description: "switch execution mode"},
	{Name: "clear", Usage: "/clear", Description: "clear the conversation view"},
	{Name: "sessions", Usage: "/sessions", Description: "show session command guidance"},
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
