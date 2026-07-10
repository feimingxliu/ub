package permission

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
)

// RuleAction determines whether a matched rule allows, asks, or denies
// the tool call.
type RuleAction string

const (
	RuleAllow RuleAction = "allow"
	RuleAsk   RuleAction = "ask"
	RuleDeny  RuleAction = "deny"
)

// Rule is a Claude-style permission rule such as "Bash(go test:*)".
// Pattern is the command-matching portion inside the parentheses; an empty
// pattern means the rule matches the tool regardless of arguments.
type Rule struct {
	Raw     string
	Action  RuleAction
	Tool    string
	Pattern string
}

type ruleFile struct {
	Permissions RuleConfig `yaml:"permissions,omitempty" json:"permissions,omitempty"`
}

type RuleConfig struct {
	Allow []string `yaml:"allow,omitempty" json:"allow,omitempty"`
	Ask   []string `yaml:"ask,omitempty"   json:"ask,omitempty"`
	Deny  []string `yaml:"deny,omitempty"  json:"deny,omitempty"`
}

var blacklistPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\bmkfs\.`),
	regexp.MustCompile(`\bdd\s+.*of=/dev/`),
}

func commandFromRequest(req Request) string {
	if strings.TrimSpace(req.Command) != "" {
		return strings.TrimSpace(req.Command)
	}
	if len(req.Args) == 0 {
		return ""
	}
	switch strings.TrimSpace(req.Tool) {
	case "web_fetch":
		var body struct {
			URL string `json:"url"`
		}
		if err := json.Unmarshal(req.Args, &body); err != nil {
			return ""
		}
		return webFetchRuleTarget(body.URL)
	case "web_search":
		var body struct {
			Query   string   `json:"query"`
			Domains []string `json:"domains"`
		}
		if err := json.Unmarshal(req.Args, &body); err != nil {
			return ""
		}
		return webSearchRuleTarget(body.Query, body.Domains)
	}
	var body struct {
		Command string `json:"command"`
		Cwd     string `json:"cwd"`
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

func workspaceFromRequest(req Request) string {
	return strings.TrimSpace(req.Workspace)
}

// matches tests whether this rule matches the given tool and command.
// Tool name must match exactly; pattern uses bash-style glob matching
// (prefix:* or wildcards).
func (r Rule) matches(req Request, command string) bool {
	if r.Tool != "" && r.Tool != permissionToolName(req.Tool) {
		return false
	}
	if r.Pattern == "" {
		return r.Tool != ""
	}
	return bashPatternMatch(r.Pattern, command)
}

// matchAllowRule checks whether any allow rule covers the command. For
// compound commands (joined by &&, ;, |, etc.), every subcommand must match
// an allow rule for the whole command to be allowed. Returns the first
// matching rule.
func matchAllowRule(rules []Rule, req Request, command string) (Rule, bool) {
	command = strings.TrimSpace(command)
	if command == "" {
		return Rule{}, false
	}
	commands := splitShellCommands(command)
	if len(commands) <= 1 {
		return matchSingleRule(rules, req, command)
	}
	for _, rule := range rules {
		if rule.matchesWholeCommand(req, command) {
			return rule, true
		}
	}
	var first Rule
	for i, subcommand := range commands {
		rule, ok := matchSingleRule(rules, req, subcommand)
		if !ok {
			return Rule{}, false
		}
		if i == 0 {
			first = rule
		}
	}
	return first, true
}

// matchAnyRule checks whether any deny or ask rule matches the command.
// Unlike allow rules, matching any single subcommand is sufficient to
// trigger the rule (deny/ask are disjunctive across subcommands).
func matchAnyRule(rules []Rule, req Request, command string) (Rule, bool) {
	command = strings.TrimSpace(command)
	if command == "" {
		return Rule{}, false
	}
	commands := splitShellCommands(command)
	for _, rule := range rules {
		if rule.matches(req, command) {
			return rule, true
		}
		for _, subcommand := range commands {
			if rule.matches(req, subcommand) {
				return rule, true
			}
		}
	}
	return Rule{}, false
}

func matchSingleRule(rules []Rule, req Request, command string) (Rule, bool) {
	for _, rule := range rules {
		if rule.matches(req, command) {
			return rule, true
		}
	}
	return Rule{}, false
}

func (r Rule) matchesWholeCommand(req Request, command string) bool {
	if r.Tool != "" && r.Tool != permissionToolName(req.Tool) {
		return false
	}
	return r.Pattern == command || r.Pattern == ""
}

// bashPatternMatch implements the pattern matching for permission rules.
// It supports three forms:
//   - "prefix:*" — prefix match (command equals prefix or starts with "prefix ")
//   - "literal"  — exact match
//   - "glob*"    — wildcard match compiled to a regexp
func bashPatternMatch(pattern, command string) bool {
	pattern = strings.TrimSpace(pattern)
	command = strings.TrimSpace(command)
	if pattern == "" || command == "" {
		return false
	}
	if strings.HasSuffix(pattern, ":*") {
		prefix := strings.TrimSuffix(pattern, ":*")
		return command == prefix || strings.HasPrefix(command, prefix+" ") || strings.HasPrefix(command, prefix+":")
	}
	if !strings.Contains(pattern, "*") {
		return command == pattern
	}
	var b strings.Builder
	b.WriteByte('^')
	for _, part := range strings.Split(pattern, "*") {
		b.WriteString(regexp.QuoteMeta(part))
		b.WriteString(".*")
	}
	raw := b.String()
	if !strings.HasSuffix(pattern, "*") {
		raw = strings.TrimSuffix(raw, ".*")
	}
	raw += "$"
	re, err := regexp.Compile(raw)
	if err != nil {
		return false
	}
	return re.MatchString(command)
}

func parsePermissionRules(cfg RuleConfig) ([]Rule, []Rule, []Rule, error) {
	allow, err := parseRuleList(cfg.Allow, RuleAllow)
	if err != nil {
		return nil, nil, nil, err
	}
	ask, err := parseRuleList(cfg.Ask, RuleAsk)
	if err != nil {
		return nil, nil, nil, err
	}
	deny, err := parseRuleList(cfg.Deny, RuleDeny)
	if err != nil {
		return nil, nil, nil, err
	}
	return allow, ask, deny, nil
}

func parseRuleList(raw []string, action RuleAction) ([]Rule, error) {
	rules := make([]Rule, 0, len(raw))
	for _, item := range raw {
		rule, err := parsePermissionRule(item, action)
		if err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

func parsePermissionRule(raw string, action RuleAction) (Rule, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Rule{}, fmt.Errorf("permission rule is empty")
	}
	open := strings.IndexByte(raw, '(')
	if open < 0 {
		return Rule{Raw: raw, Action: action, Tool: permissionToolName(raw)}, nil
	}
	if !strings.HasSuffix(raw, ")") {
		return Rule{}, fmt.Errorf("permission rule %q missing closing parenthesis", raw)
	}
	toolName := strings.TrimSpace(raw[:open])
	pattern := strings.TrimSpace(raw[open+1 : len(raw)-1])
	if toolName == "" {
		return Rule{}, fmt.Errorf("permission rule %q missing tool name", raw)
	}
	return Rule{Raw: raw, Action: action, Tool: permissionToolName(toolName), Pattern: pattern}, nil
}

func permissionToolName(toolName string) string {
	switch strings.ToLower(strings.TrimSpace(toolName)) {
	case "bash", "job_run", "jobrun":
		return "Bash"
	case "job_kill", "jobkill":
		return "JobKill"
	case "web_fetch", "webfetch":
		return "WebFetch"
	case "web_search", "websearch":
		return "WebSearch"
	default:
		return strings.TrimSpace(toolName)
	}
}

func webFetchRuleTarget(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ""
	}
	if target := parsedWebFetchRuleTarget(rawURL); target != "" {
		return target
	}
	if !strings.Contains(rawURL, "://") {
		if target := parsedWebFetchRuleTarget("https://" + rawURL); target != "" {
			return target
		}
	}
	return ""
}

func parsedWebFetchRuleTarget(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || strings.TrimSpace(u.Host) == "" {
		return ""
	}
	host := strings.ToLower(strings.TrimSpace(u.Host))
	path := u.EscapedPath()
	if path == "" {
		path = "/"
	}
	return host + ":" + path
}

func webSearchRuleTarget(query string, domains []string) string {
	for _, domain := range domains {
		domain = strings.ToLower(strings.TrimSpace(domain))
		domain = strings.TrimPrefix(domain, "http://")
		domain = strings.TrimPrefix(domain, "https://")
		domain = strings.TrimPrefix(domain, "*.")
		domain = strings.Trim(domain, "/")
		if domain != "" {
			return "domain:" + domain
		}
	}
	return strings.TrimSpace(query)
}

func formatPermissionRule(toolName, pattern string) string {
	toolName = permissionToolName(toolName)
	if strings.TrimSpace(pattern) == "" {
		return toolName
	}
	return fmt.Sprintf("%s(%s)", toolName, strings.TrimSpace(pattern))
}

// splitShellCommands splits a compound shell command into individual
// subcommands at unquoted &&, ;, |, ||, and && boundaries. Quoted and
// escaped separators are preserved. This is used by the permission rule
// matcher to evaluate each subcommand independently.
func splitShellCommands(command string) []string {
	var commands []string
	var b strings.Builder
	var quote rune
	escaped := false
	flush := func() {
		part := strings.TrimSpace(b.String())
		if part != "" {
			commands = append(commands, part)
		}
		b.Reset()
	}
	for i, r := range command {
		if escaped {
			b.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			b.WriteRune(r)
			escaped = true
			continue
		}
		if quote != 0 {
			b.WriteRune(r)
			if r == quote {
				quote = 0
			}
			continue
		}
		switch r {
		case '\'', '"':
			quote = r
			b.WriteRune(r)
		case '\n', ';', '&', '|':
			flush()
			if (r == '&' || r == '|') && i+1 < len(command) {
				next := rune(command[i+1])
				if next == r || (r == '|' && next == '&') {
					// The next separator rune is skipped naturally by producing
					// an empty command on the next iteration.
				}
			}
		default:
			b.WriteRune(r)
		}
	}
	flush()
	return commands
}

// isBlacklisted is a defense-in-depth guard against obviously catastrophic
// commands ("rm -rf /", "mkfs.*", "dd of=/dev/*"). It is intended as a
// shallow safety net for fat-fingered input, not as a sandbox: it does not
// parse shell syntax, so it can be bypassed by indirection like
// `x=rm; $x -rf /`, busybox/exec wrappers, command substitution, or
// alternative deleters such as `find / -delete`. Treat the permission
// allowlist (and the host OS) as the real authority on what a tool is
// allowed to do; this check exists so that obvious mistakes do not turn
// into a recoverable outage.
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
