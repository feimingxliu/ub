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

var supported = map[string]struct{}{
	"model":    {},
	"mode":     {},
	"clear":    {},
	"sessions": {},
	"help":     {},
	"quit":     {},
	"config":   {},
	"profile":  {},
}

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
	return []string{"model", "mode", "clear", "sessions", "help", "quit", "config", "profile"}
}
