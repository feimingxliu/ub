package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/feimingxliu/ub/internal/config"
	"github.com/feimingxliu/ub/internal/message"
)

func TestBuildStartupPromptMessagesOrderTruncationAndDisable(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "AGENTS.md"), []byte("read files before edits\n"+strings.Repeat("x", 200)), 0o644); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}
	gitEnabled := false
	cfg := config.PromptConfig{
		WorkspaceInstructions: config.PromptSectionConfig{MaxChars: 90},
		GitSnapshot:           config.PromptSectionConfig{Enabled: &gitEnabled},
	}
	msgs := buildStartupPromptMessages(RuntimeContext{Workspace: ws, Shell: "/bin/sh", OS: "linux"}, ws, cfg)
	gotIDs := promptSectionIDs(msgs)
	wantIDs := []string{"coding", "environment", "workspace"}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("section ids = %#v, want %#v", gotIDs, wantIDs)
	}
	if !strings.Contains(msgs[2].Text(), "... [workspace instructions truncated]") {
		t.Fatalf("workspace instructions not truncated:\n%s", msgs[2].Text())
	}

	workspaceEnabled := false
	msgs = buildStartupPromptMessages(RuntimeContext{Workspace: ws}, ws, config.PromptConfig{
		WorkspaceInstructions: config.PromptSectionConfig{Enabled: &workspaceEnabled},
		GitSnapshot:           config.PromptSectionConfig{Enabled: &gitEnabled},
	})
	if gotIDs := promptSectionIDs(msgs); !reflect.DeepEqual(gotIDs, []string{"coding", "environment"}) {
		t.Fatalf("section ids with workspace disabled = %#v", gotIDs)
	}
}

func TestWorkspaceInstructionsDiscovery(t *testing.T) {
	parent := t.TempDir()
	ws := filepath.Join(parent, "repo")
	if err := os.MkdirAll(filepath.Join(ws, ".ub"), 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	if err := os.WriteFile(filepath.Join(parent, "AGENTS.md"), []byte("parent should not load"), 0o644); err != nil {
		t.Fatalf("write parent AGENTS.md: %v", err)
	}
	for rel, body := range map[string]string{
		"AGENTS.md":             "agent rules",
		"CLAUDE.md":             "claude rules",
		".ub/instructions.md":   "ub rules",
		".ub/ignored-empty.md":  "",
		".ub/ignored-binary.md": "\x00\x01",
	} {
		path := filepath.Join(ws, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	msg, ok := workspaceInstructionsMessage(ws, config.PromptSectionConfig{MaxChars: 1000})
	if !ok {
		t.Fatal("workspace instructions not discovered")
	}
	text := msg.Text()
	if !strings.Contains(text, "Source: AGENTS.md\nagent rules") {
		t.Fatalf("instructions missing AGENTS.md:\n%s", text)
	}
	for _, unwanted := range []string{"parent should not load", "claude rules", "ub rules"} {
		if strings.Contains(text, unwanted) {
			t.Fatalf("loaded unwanted instructions %q:\n%s", unwanted, text)
		}
	}
}

func TestGitSnapshotBuilderDirtyLongAndOptionalFailures(t *testing.T) {
	run := fakeGitRunner(map[string]gitResponse{
		"rev-parse --is-inside-work-tree":                       {out: "true\n"},
		"branch --show-current":                                 {out: "feature/prompt\n"},
		"symbolic-ref --quiet --short refs/remotes/origin/HEAD": {out: "origin/main\n"},
		"status --short":                                        {out: " M internal/agent/prompt_harness.go\n?? docs/prompt.md\n" + strings.Repeat(" M generated.txt\n", 40)},
		"log --oneline -5":                                      {err: errors.New("log unavailable")},
	})
	got, ok := buildGitSnapshot(context.Background(), "/repo", 260, run)
	if !ok {
		t.Fatal("snapshot omitted")
	}
	for _, want := range []string{
		"Captured when this agent run started; this is not live state",
		"branch: feature/prompt",
		"default_branch: origin/main",
		" M internal/agent/prompt_harness.go",
		"?? docs/prompt.md",
		"... [git snapshot truncated]",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("snapshot missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "recent_commits:") {
		t.Fatalf("optional log failure should omit recent commits:\n%s", got)
	}
}

func TestGitSnapshotOmittedOutsideGitOrWhenProbeFails(t *testing.T) {
	for name, run := range map[string]gitCommandRunner{
		"outside": fakeGitRunner(map[string]gitResponse{
			"rev-parse --is-inside-work-tree": {out: "false\n"},
		}),
		"failure": fakeGitRunner(map[string]gitResponse{
			"rev-parse --is-inside-work-tree": {err: errors.New("git failed")},
		}),
	} {
		t.Run(name, func(t *testing.T) {
			if got, ok := buildGitSnapshot(context.Background(), "/repo", 1000, run); ok || got != "" {
				t.Fatalf("snapshot = %q ok=%v, want omitted", got, ok)
			}
		})
	}
}

func promptSectionIDs(msgs []message.Message) []string {
	ids := make([]string, 0, len(msgs))
	for _, msg := range msgs {
		text := msg.Text()
		switch {
		case strings.Contains(text, "<coding_agent_instructions>"):
			ids = append(ids, "coding")
		case strings.Contains(text, "<environment_context>"):
			ids = append(ids, "environment")
		case strings.Contains(text, "<workspace_instructions>"):
			ids = append(ids, "workspace")
		case strings.Contains(text, "<git_snapshot>"):
			ids = append(ids, "git")
		}
	}
	return ids
}

type gitResponse struct {
	out string
	err error
}

func fakeGitRunner(responses map[string]gitResponse) gitCommandRunner {
	return func(_ context.Context, _ string, args ...string) (string, error) {
		resp, ok := responses[strings.Join(args, " ")]
		if !ok {
			return "", errors.New("unexpected git command: " + strings.Join(args, " "))
		}
		return resp.out, resp.err
	}
}
