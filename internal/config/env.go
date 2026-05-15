package config

import (
	"log/slog"
	"os"
	"regexp"
	"strings"
)

// envPattern matches one of:
//   - $$            - literal dollar sign escape
//   - ${VAR}        - variable lookup with empty fallback (and a WARN log)
//   - ${VAR:-default} - variable lookup with explicit fallback
//
// Variable names follow the convention used by most env-substitution
// implementations: [A-Z_][A-Z0-9_]*.
var envPattern = regexp.MustCompile(`\$\$|\$\{([A-Z_][A-Z0-9_]*)(:-([^}]*))?\}`)

// Expand performs environment variable substitution on the raw YAML byte
// stream BEFORE parsing. Substituting at the byte-stream level (rather
// than per-string-value after parse) lets placeholders appear anywhere -
// including in keys and structural positions - and keeps the rule simple.
func Expand(b []byte) []byte {
	return envPattern.ReplaceAllFunc(b, func(match []byte) []byte {
		s := string(match)
		if s == "$$" {
			return []byte("$")
		}
		// Re-run a captured-groups match to extract the parts.
		m := envPattern.FindStringSubmatch(s)
		// m[0] = full match, m[1] = VAR, m[2] = ":-default" or "", m[3] = default
		name := m[1]
		if v, ok := os.LookupEnv(name); ok && v != "" {
			return []byte(v)
		}
		if m[2] != "" {
			return []byte(m[3])
		}
		// Unset and no default - warn but don't fail. Tests can capture
		// the message via slog.SetDefault.
		slog.Warn("env variable used in config is unset", "name", name)
		return []byte("")
	})
}

// expandString is a convenience for callers (tests, REPLs) that have a
// string in hand rather than a byte slice.
func expandString(s string) string {
	return string(Expand([]byte(s)))
}

// envHasPlaceholder reports whether s contains a substitution placeholder.
// Used by error messages to hint at the cause when YAML parsing fails on
// what looks like a literal "${" inside a string.
func envHasPlaceholder(s string) bool {
	return strings.Contains(s, "${")
}
