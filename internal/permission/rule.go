package permission

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
)

// Rule is an allow-rule used either for session memory or global YAML.
type Rule struct {
	Tool          string `yaml:"tool,omitempty"           json:"tool,omitempty"`
	Command       string `yaml:"command,omitempty"        json:"command,omitempty"`
	CommandPrefix string `yaml:"command_prefix,omitempty" json:"command_prefix,omitempty"`
}

type ruleFile struct {
	Global []Rule `yaml:"global" json:"global"`
}

var blacklistPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\bmkfs\.`),
	regexp.MustCompile(`\bdd\s+.*of=/dev/`),
}

func commandFromRequest(req Request) string {
	if strings.TrimSpace(req.Command) != "" {
		return strings.TrimSpace(req.Command)
	}
	var body struct {
		Command string `json:"command"`
		Cwd     string `json:"cwd"`
	}
	if len(req.Args) == 0 {
		return ""
	}
	if err := json.Unmarshal(req.Args, &body); err != nil {
		return ""
	}
	return strings.TrimSpace(body.Command)
}

func cwdFromRequest(req Request) string {
	if strings.TrimSpace(req.Cwd) != "" {
		return strings.TrimSpace(req.Cwd)
	}
	var body struct {
		Cwd string `json:"cwd"`
	}
	if len(req.Args) == 0 {
		return ""
	}
	if err := json.Unmarshal(req.Args, &body); err != nil {
		return ""
	}
	return strings.TrimSpace(body.Cwd)
}

func (r Rule) matches(req Request, command string) bool {
	if r.Tool != "" && r.Tool != req.Tool {
		return false
	}
	switch {
	case r.Command != "":
		return command == r.Command
	case r.CommandPrefix != "":
		return strings.HasPrefix(command, r.CommandPrefix)
	default:
		return r.Tool != ""
	}
}

func matchRule(rules []Rule, req Request, command string) (Rule, bool) {
	for _, rule := range rules {
		if rule.matches(req, command) {
			return rule, true
		}
	}
	return Rule{}, false
}

func isBlacklisted(command string) bool {
	command = normalizeCommandForBlacklist(command)
	if isDangerousRecursiveRemove(command) {
		return true
	}
	for _, pattern := range blacklistPatterns {
		if pattern.MatchString(command) {
			return true
		}
	}
	return false
}

func normalizeCommandForBlacklist(command string) string {
	command = os.ExpandEnv(command)
	var b strings.Builder
	escaped := false
	for _, r := range command {
		if escaped {
			b.WriteRune(r)
			escaped = false
			continue
		}
		switch r {
		case '\\':
			escaped = true
		case '\'', '"':
			continue
		default:
			b.WriteRune(r)
		}
	}
	if escaped {
		b.WriteRune('\\')
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func isDangerousRecursiveRemove(command string) bool {
	fields := strings.Fields(command)
	for i, field := range fields {
		if !isRMCommand(field) {
			continue
		}
		recursive := false
		force := false
		for _, arg := range fields[i+1:] {
			if arg == "--" {
				continue
			}
			if strings.HasPrefix(arg, "-") && arg != "-" {
				if arg == "--recursive" || strings.ContainsAny(arg, "rR") {
					recursive = true
				}
				if arg == "--force" || strings.Contains(arg, "f") {
					force = true
				}
				continue
			}
			if recursive && force && isDangerousRMTarget(arg) {
				return true
			}
		}
	}
	return false
}

func isRMCommand(field string) bool {
	if field == "rm" {
		return true
	}
	return strings.HasSuffix(field, "/rm")
}

func isDangerousRMTarget(target string) bool {
	switch {
	case target == "~", strings.HasPrefix(target, "~/"):
		return true
	case strings.HasPrefix(target, "/"):
		return true
	default:
		return false
	}
}
