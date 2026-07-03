package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/feimingxliu/ub/internal/pkg/core/config"
	"github.com/feimingxliu/ub/internal/pkg/core/message"
)

// gitSnapshotTimeout caps how long the prompt harness waits for git
// commands when building the startup snapshot. Git operations are best-effort:
// a timeout or error produces an empty snapshot rather than blocking startup.
const gitSnapshotTimeout = 500 * time.Millisecond

// workspaceInstructionFiles lists filenames that are loaded as workspace
// instructions and injected into the system prompt prefix.
var workspaceInstructionFiles = []string{"AGENTS.md"}

// gitCommandRunner is the function signature for running git commands,
// abstracted for testability.
type gitCommandRunner func(ctx context.Context, dir string, args ...string) (string, error)

// effectivePromptConfig fills in defaults for any unset prompt configuration
// fields, returning a fully-populated config safe for the prompt builder.
func effectivePromptConfig(cfg config.PromptConfig) config.PromptConfig {
	if cfg.WorkspaceInstructions.Enabled == nil {
		enabled := true
		cfg.WorkspaceInstructions.Enabled = &enabled
	}
	if cfg.WorkspaceInstructions.MaxChars <= 0 {
		cfg.WorkspaceInstructions.MaxChars = config.DefaultPromptWorkspaceInstructionsMaxChars
	}
	if cfg.GitSnapshot.Enabled == nil {
		enabled := true
		cfg.GitSnapshot.Enabled = &enabled
	}
	if cfg.GitSnapshot.MaxChars <= 0 {
		cfg.GitSnapshot.MaxChars = config.DefaultPromptGitSnapshotMaxChars
	}
	if strings.TrimSpace(cfg.CompactStyle) == "" {
		cfg.CompactStyle = config.CompactStyleStructured
	}
	return cfg
}

func buildStartupPromptMessages(runtime RuntimeContext, workspaceRoot string, cfg config.PromptConfig) []message.Message {
	cfg = effectivePromptConfig(cfg)
	var out []message.Message
	out = append(out, codingAgentInstructionsMessage())
	if runtimeMsg, ok := runtime.message(); ok {
		out = append(out, runtimeMsg)
	}
	if instructionMsg, ok := workspaceInstructionsMessage(workspaceRoot, cfg.WorkspaceInstructions); ok {
		out = append(out, instructionMsg)
	}
	if gitMsg, ok := gitSnapshotMessage(workspaceRoot, cfg.GitSnapshot, realGitCommand); ok {
		out = append(out, gitMsg)
	}
	return out
}

func codingAgentInstructionsMessage() message.Message {
	const body = `<coding_agent_instructions>
- Work from the current repository state. Read the relevant files before proposing or applying edits.
- Prefer purpose-built tools: read for files, ls/glob for directories, grep for text search, ask for concrete user choices, enter_plan_mode for complex implementation planning, web_search/web_fetch for current external information, task for isolated research, plan_write/plan_update for persistent planning, exit_plan_mode for plan approval, plan_update_step for plan progress, and todo_write/todo_update for the current execution checklist.
- In work mode, request plan mode before complex new features, multi-file behavior changes, architecture choices, risky migrations, or ambiguous requirements. Do not request plan mode for small typo fixes, simple known bug fixes, already-specified implementation steps, or pure read-only questions. The only way to enter plan mode is to call the enter_plan_mode tool; saying "entering plan mode" or "let me plan" in text does not switch modes, so never state you are entering plan mode and then start editing. Do not substitute todo_write for plan mode — todo_write tracks execution progress, not the planning step itself.
- In auto mode, keep acting unless the user explicitly asks to plan first. Use ask for a concrete choice among options; if plan-mode tools are unavailable, explain the plan in text instead of calling hidden tools.
- Risky or destructive actions such as deletes, resets, force pushes, installs, network fetches, or long-running commands require explicit approval through the tool policy before execution.
- When a command or test fails, inspect the error and environment before changing strategy. Do not claim tests, builds, or checks passed unless they actually ran and passed.
- Keep user-facing updates concise and report the real verification status, including commands that were not run or did not pass.
</coding_agent_instructions>`
	return message.Text(message.RoleSystem, body)
}

func workspaceInstructionsMessage(workspaceRoot string, cfg config.PromptSectionConfig) (message.Message, bool) {
	if !promptSectionEnabled(cfg.Enabled) || strings.TrimSpace(workspaceRoot) == "" {
		return message.Message{}, false
	}
	workspaceRoot = filepath.Clean(workspaceRoot)
	var sections []string
	for _, rel := range workspaceInstructionFiles {
		path := filepath.Join(workspaceRoot, rel)
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		body := strings.TrimSpace(string(raw))
		if body == "" {
			continue
		}
		sections = append(sections, fmt.Sprintf("Source: %s\n%s", filepath.ToSlash(rel), body))
	}
	if len(sections) == 0 {
		return message.Message{}, false
	}
	body := "These are explicit workspace instructions discovered at agent startup. Follow them unless they conflict with higher-priority instructions.\n\n" + strings.Join(sections, "\n\n---\n\n")
	body = truncatePromptChars(body, promptSectionMaxChars(cfg.MaxChars, config.DefaultPromptWorkspaceInstructionsMaxChars), "\n... [workspace instructions truncated]")
	return message.Text(message.RoleSystem, "<workspace_instructions>\n"+body+"\n</workspace_instructions>"), true
}

func gitSnapshotMessage(workspaceRoot string, cfg config.PromptSectionConfig, run gitCommandRunner) (message.Message, bool) {
	if !promptSectionEnabled(cfg.Enabled) || strings.TrimSpace(workspaceRoot) == "" || run == nil {
		return message.Message{}, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), gitSnapshotTimeout)
	defer cancel()
	body, ok := buildGitSnapshot(ctx, filepath.Clean(workspaceRoot), promptSectionMaxChars(cfg.MaxChars, config.DefaultPromptGitSnapshotMaxChars), run)
	if !ok {
		return message.Message{}, false
	}
	return message.Text(message.RoleSystem, "<git_snapshot>\n"+body+"\n</git_snapshot>"), true
}

func buildGitSnapshot(ctx context.Context, workspaceRoot string, maxChars int, run gitCommandRunner) (string, bool) {
	inside, err := run(ctx, workspaceRoot, "rev-parse", "--is-inside-work-tree")
	if err != nil || strings.TrimSpace(inside) != "true" {
		return "", false
	}
	branch := strings.TrimSpace(mustGit(ctx, workspaceRoot, run, "branch", "--show-current"))
	if branch == "" {
		commit := strings.TrimSpace(mustGit(ctx, workspaceRoot, run, "rev-parse", "--short", "HEAD"))
		if commit != "" {
			branch = "(detached " + commit + ")"
		}
	}
	defaultBranch := strings.TrimSpace(mustGit(ctx, workspaceRoot, run, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"))
	status := strings.TrimSpace(mustGit(ctx, workspaceRoot, run, "status", "--short"))
	recent := strings.TrimSpace(mustGit(ctx, workspaceRoot, run, "log", "--oneline", "-5"))

	var b strings.Builder
	b.WriteString("Captured when this agent run started; this is not live state. Re-check git status before relying on it.\n")
	if branch != "" {
		fmt.Fprintf(&b, "branch: %s\n", branch)
	}
	if defaultBranch != "" {
		fmt.Fprintf(&b, "default_branch: %s\n", defaultBranch)
	}
	b.WriteString("status:\n")
	if status == "" {
		b.WriteString("  clean\n")
	} else {
		for _, line := range strings.Split(status, "\n") {
			fmt.Fprintf(&b, "  %s\n", line)
		}
	}
	if recent != "" {
		b.WriteString("recent_commits:\n")
		for _, line := range strings.Split(recent, "\n") {
			fmt.Fprintf(&b, "  %s\n", line)
		}
	}
	return truncatePromptChars(strings.TrimSpace(b.String()), maxChars, "\n... [git snapshot truncated]"), true
}

func mustGit(ctx context.Context, dir string, run gitCommandRunner, args ...string) string {
	out, err := run(ctx, dir, args...)
	if err != nil {
		return ""
	}
	return out
}

func realGitCommand(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func promptSectionEnabled(enabled *bool) bool {
	return enabled == nil || *enabled
}

func promptSectionMaxChars(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

func truncatePromptChars(s string, maxChars int, marker string) string {
	if maxChars <= 0 || utf8.RuneCountInString(s) <= maxChars {
		return s
	}
	runes := []rune(s)
	markerRunes := []rune(marker)
	keep := maxChars - len(markerRunes)
	if keep <= 0 {
		return string(runes[:maxChars])
	}
	return string(runes[:keep]) + marker
}
