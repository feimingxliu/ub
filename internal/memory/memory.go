// Package memory persists durable, agent-visible facts about the user's
// workspace ("build command is X", "issue #42 root cause is Y") and the
// user's broader environment ("prefer pnpm over npm", "VPN URL is Z").
//
// Two scopes:
//
//   - workspace: <workspace>/.ub/memory.md — committed (or .gitignored) per
//     project, owned by the team
//   - global:    <XDG_CONFIG_HOME or ~/.config>/ub/memory.md — owned by the
//     individual user across all workspaces
//
// Entries append; readers concatenate global-then-workspace. There is no
// structured schema beyond "## <timestamp>" headings so users can hand-edit
// memory files without breaking the runtime.
package memory

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Scope selects which memory file an operation targets.
type Scope string

const (
	ScopeWorkspace Scope = "workspace"
	ScopeGlobal    Scope = "global"
)

// nowFunc is overridden in tests for deterministic entry headers.
var nowFunc = func() time.Time { return time.Now() }

// DefaultReadMaxChars is the default budget used when agent.Options does
// not specify one.
const DefaultReadMaxChars = 4000

// ValidScope reports whether s is one of the two supported values.
func ValidScope(s string) bool {
	switch Scope(s) {
	case ScopeWorkspace, ScopeGlobal:
		return true
	}
	return false
}

// Path returns the absolute path of the memory file for one scope. For
// the workspace scope, workspaceRoot MUST be non-empty.
func Path(workspaceRoot string, scope Scope) (string, error) {
	switch scope {
	case ScopeWorkspace:
		if strings.TrimSpace(workspaceRoot) == "" {
			return "", errors.New("memory: workspace root required for workspace scope")
		}
		return filepath.Join(workspaceRoot, ".ub", "memory.md"), nil
	case ScopeGlobal:
		root, err := configHome()
		if err != nil {
			return "", err
		}
		return filepath.Join(root, "ub", "memory.md"), nil
	default:
		return "", fmt.Errorf("memory: unknown scope %q", scope)
	}
}

func configHome() (string, error) {
	if v := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); v != "" {
		return v, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config"), nil
}

// Append writes a new entry to the scope's memory file. Returns the
// absolute path of the file and the heading string of the new entry.
func Append(workspaceRoot string, scope Scope, text string) (string, string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", "", errors.New("memory: text is required")
	}
	path, err := Path(workspaceRoot, scope)
	if err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", "", fmt.Errorf("memory: mkdir: %w", err)
	}
	heading := "## " + nowFunc().Format(time.RFC3339)
	entry := "\n" + heading + "\n\n" + text + "\n"
	// Open with append so concurrent ub invocations on the same memory file
	// don't clobber each other's writes mid-flight.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return "", "", fmt.Errorf("memory: open: %w", err)
	}
	defer f.Close()
	if _, err := f.WriteString(entry); err != nil {
		return "", "", fmt.Errorf("memory: write: %w", err)
	}
	return path, heading, nil
}

// Read returns the concatenated memory the agent should see this turn:
// global entries first, then workspace entries, with HTML comment markers
// so the model can tell them apart. Each file's missing or empty content
// is silently dropped — there's no "memory file not found" error path
// surfaced upward.
//
// When maxChars > 0 and the combined text exceeds the budget, the head is
// truncated (keeping the tail = the newest entries) and a "... [memory
// truncated]" marker is inserted at the new start.
func Read(workspaceRoot string, maxChars int) string {
	var parts []string
	if gp, err := Path("", ScopeGlobal); err == nil {
		if body := readFile(gp); body != "" {
			parts = append(parts, "<!-- global memory -->\n"+body)
		}
	}
	if strings.TrimSpace(workspaceRoot) != "" {
		if wp, err := Path(workspaceRoot, ScopeWorkspace); err == nil {
			if body := readFile(wp); body != "" {
				parts = append(parts, "<!-- workspace memory -->\n"+body)
			}
		}
	}
	if len(parts) == 0 {
		return ""
	}
	joined := strings.Join(parts, "\n---\n")
	if maxChars > 0 && len(joined) > maxChars {
		// Drop the head, keep the tail.
		drop := len(joined) - maxChars
		joined = "... [memory truncated]\n" + joined[drop:]
	}
	return joined
}

func readFile(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
