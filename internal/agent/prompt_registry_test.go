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
	execmode "github.com/feimingxliu/ub/internal/mode"
)

func TestPromptRegistryMainManifestOrderAndMetadata(t *testing.T) {
	temp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(temp, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(temp, "state"))
	workspace := filepath.Join(temp, "repo")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	workspaceSecret := "workspace-secret-marker-" + strings.Repeat("x", 120)
	if err := os.WriteFile(filepath.Join(workspace, "AGENTS.md"), []byte(workspaceSecret), 0o644); err != nil {
		t.Fatal(err)
	}
	configDir := filepath.Join(temp, "config", "ub")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "instructions.md"), []byte("memory-secret-marker"), 0o644); err != nil {
		t.Fatal(err)
	}
	run := fakeGitRunner(map[string]gitResponse{
		"rev-parse --is-inside-work-tree":                       {out: "true\n"},
		"branch --show-current":                                 {out: "main\n"},
		"symbolic-ref --quiet --short refs/remotes/origin/HEAD": {out: "origin/main\n"},
		"status --short":                                        {out: ""},
		"log --oneline -5":                                      {out: "abc123 test\n"},
	})
	registry := newPromptRegistryWithGit(
		RuntimeContext{Workspace: workspace, Shell: "/bin/sh", OS: "linux"},
		workspace,
		config.PromptConfig{WorkspaceInstructions: config.PromptSectionConfig{MaxChars: 90}},
		4000,
		run,
	)
	sections := registry.mainSections(execmode.ModePlan)
	manifest := promptManifest(promptVariantMain, "fake/model", sections, false)

	wantIDs := []string{
		promptSectionCodingAgent,
		promptSectionRuntime,
		promptSectionWorkspaceInstructions,
		promptSectionGitSnapshot,
		promptSectionExecutionMode,
		promptSectionMemory,
	}
	var gotIDs []string
	for _, section := range manifest.Sections {
		gotIDs = append(gotIDs, section.ID)
		if section.Status != promptStatusIncluded {
			t.Fatalf("section %s status = %s, want included", section.ID, section.Status)
		}
		if section.Content != "" {
			t.Fatalf("section %s leaked content by default", section.ID)
		}
		if section.Chars <= 0 || section.EstimatedTokens <= 0 {
			t.Fatalf("section %s missing estimates: %#v", section.ID, section)
		}
	}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("section ids = %#v, want %#v", gotIDs, wantIDs)
	}
	if manifest.TotalChars <= 0 || manifest.EstimatedTokens <= 0 {
		t.Fatalf("manifest missing totals: %#v", manifest)
	}
	if !manifest.Sections[2].Truncated {
		t.Fatalf("workspace section not marked truncated: %#v", manifest.Sections[2])
	}

	// Cacheable flag: stable sections should be cacheable, dynamic sections
	// (git_snapshot, execution_mode, memory) should not.
	stableIDs := map[string]bool{
		promptSectionCodingAgent:           true,
		promptSectionRuntime:               true,
		promptSectionWorkspaceInstructions: true,
	}
	for _, section := range manifest.Sections {
		wantCacheable := stableIDs[section.ID]
		if section.Cacheable != wantCacheable {
			t.Fatalf("section %s cacheable = %v, want %v", section.ID, section.Cacheable, wantCacheable)
		}
	}

	withContent := promptManifest(promptVariantMain, "fake/model", sections, true)
	if !strings.Contains(withContent.Sections[2].Content, "[workspace instructions truncated]") {
		t.Fatalf("workspace content missing truncation marker: %q", withContent.Sections[2].Content)
	}
	if !strings.Contains(withContent.Sections[5].Content, "memory-secret-marker") {
		t.Fatalf("memory content missing from explicit manifest: %q", withContent.Sections[5].Content)
	}
}

func TestPromptRegistryStatusesAndNoToolProjection(t *testing.T) {
	temp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(temp, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(temp, "state"))
	workspace := filepath.Join(temp, "repo")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	configDir := filepath.Join(temp, "config", "ub")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "instructions.md"), []byte("no-tool-memory"), 0o644); err != nil {
		t.Fatal(err)
	}

	noTool := newNoToolPromptRegistry(RuntimeContext{Workspace: workspace, Shell: "/bin/sh", OS: "linux"}, workspace, 4000)
	sections := noTool.noToolSections()
	gotStatuses := make([]string, 0, len(sections))
	for _, section := range sections {
		gotStatuses = append(gotStatuses, section.status)
	}
	wantStatuses := []string{
		promptStatusOmitted,
		promptStatusIncluded,
		promptStatusOmitted,
		promptStatusOmitted,
		promptStatusOmitted,
		promptStatusIncluded,
	}
	if !reflect.DeepEqual(gotStatuses, wantStatuses) {
		t.Fatalf("no-tool statuses = %#v, want %#v", gotStatuses, wantStatuses)
	}
	messages := promptMessages(sections)
	if len(messages) != 2 {
		t.Fatalf("no-tool messages = %d, want 2", len(messages))
	}
	joined := messages[0].Text() + "\n" + messages[1].Text()
	if !strings.Contains(joined, "No-tool context rules") || !strings.Contains(joined, "no-tool-memory") {
		t.Fatalf("no-tool prompt missing expected content:\n%s", joined)
	}
	for _, unwanted := range []string{"<coding_agent_instructions>", "<workspace_instructions>", "<git_snapshot>", "<execution_mode>"} {
		if strings.Contains(joined, unwanted) {
			t.Fatalf("no-tool prompt contained %q:\n%s", unwanted, joined)
		}
	}

	disabled := false
	main := newPromptRegistryWithGit(
		RuntimeContext{Workspace: workspace},
		workspace,
		config.PromptConfig{
			WorkspaceInstructions: config.PromptSectionConfig{Enabled: &disabled},
			GitSnapshot:           config.PromptSectionConfig{Enabled: &disabled},
		},
		4000,
		func(_ context.Context, _ string, _ ...string) (string, error) {
			return "", errors.New("disabled git runner must not execute")
		},
	).mainSections(execmode.ModeWork)
	if main[2].status != promptStatusDisabled || main[3].status != promptStatusDisabled {
		t.Fatalf("disabled statuses = workspace:%s git:%s", main[2].status, main[3].status)
	}
	if main[4].status != promptStatusUnavailable {
		t.Fatalf("work mode status = %s, want unavailable", main[4].status)
	}
}

func TestPromptRegistryOptionalSectionsUnavailable(t *testing.T) {
	temp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(temp, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(temp, "state"))
	workspace := filepath.Join(temp, "repo")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	run := fakeGitRunner(map[string]gitResponse{
		"rev-parse --is-inside-work-tree": {out: "false\n"},
	})
	registry := newPromptRegistryWithGit(
		RuntimeContext{Workspace: workspace},
		workspace,
		config.PromptConfig{},
		4000,
		run,
	)
	manifest := promptManifest(promptVariantMain, "", registry.mainSections(execmode.ModeWork), false)
	wantStatuses := []string{
		promptStatusIncluded,
		promptStatusIncluded,
		promptStatusUnavailable,
		promptStatusUnavailable,
		promptStatusUnavailable,
		promptStatusUnavailable,
	}
	gotStatuses := make([]string, 0, len(manifest.Sections))
	for _, section := range manifest.Sections {
		gotStatuses = append(gotStatuses, section.Status)
	}
	if !reflect.DeepEqual(gotStatuses, wantStatuses) {
		t.Fatalf("statuses = %#v, want %#v", gotStatuses, wantStatuses)
	}
	if got := promptMessages(registry.mainSections(execmode.ModeWork)); len(got) != 2 {
		t.Fatalf("provider messages = %d, want coding + runtime", len(got))
	}
}

func TestCompactPromptManifestUsesSummaryTemplateWithoutContentByDefault(t *testing.T) {
	manifest := InspectPrompt(PromptInspectOptions{
		Prompt:  config.PromptConfig{CompactStyle: config.CompactStyleShort},
		Model:   "fake/summary",
		Variant: promptVariantCompact,
	})
	if manifest.Variant != promptVariantCompact || manifest.Model != "fake/summary" || manifest.ToolsEnabled {
		t.Fatalf("manifest header = %#v", manifest)
	}
	if len(manifest.Sections) != 1 {
		t.Fatalf("sections = %#v, want one compact section", manifest.Sections)
	}
	section := manifest.Sections[0]
	if section.ID != promptSectionCompactInstructions || section.Status != promptStatusIncluded || section.Stability != promptStabilityStable || section.Source != "builtin" {
		t.Fatalf("compact section = %#v", section)
	}
	if section.Content != "" || section.EstimatedTokens <= 0 {
		t.Fatalf("default compact manifest leaked or lacked estimate: %#v", section)
	}
	withContent := InspectPrompt(PromptInspectOptions{
		Prompt:      config.PromptConfig{CompactStyle: config.CompactStyleShort},
		Model:       "fake/summary",
		Variant:     promptVariantCompact,
		ShowContent: true,
	})
	if got := withContent.Sections[0].Content; got != summaryShortPromptTemplate {
		t.Fatalf("compact manifest template differs from summary request template:\n%s", got)
	}
}

// TestStableSectionsProduceStableCachePrefix verifies that stable prompt
// sections produce identical content across multiple registry constructions,
// which is the prerequisite for provider prefix cache hits. Dynamic sections
// (git_snapshot, execution_mode, memory) are expected to vary.
func TestStableSectionsProduceStableCachePrefix(t *testing.T) {
	temp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(temp, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(temp, "state"))
	workspace := filepath.Join(temp, "repo")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "AGENTS.md"), []byte("# Project\nStable instructions."), 0o644); err != nil {
		t.Fatal(err)
	}
	run := fakeGitRunner(map[string]gitResponse{
		"rev-parse --is-inside-work-tree":                       {out: "true\n"},
		"branch --show-current":                                 {out: "main\n"},
		"symbolic-ref --quiet --short refs/remotes/origin/HEAD": {out: "origin/main\n"},
		"status --short":                                        {out: ""},
		"log --oneline -5":                                      {out: "abc123 test\n"},
	})
	cfg := config.PromptConfig{WorkspaceInstructions: config.PromptSectionConfig{Enabled: ptrBool(true)}}
	runtime := RuntimeContext{Workspace: workspace, Shell: "/bin/sh", OS: "linux"}

	// Build the prompt twice — simulating two consecutive turns.
	registry1 := newPromptRegistryWithGit(runtime, workspace, cfg, 4000, run)
	sections1 := registry1.mainSections(execmode.ModeWork)
	manifest1 := promptManifest(promptVariantMain, "fake/model", sections1, true)

	registry2 := newPromptRegistryWithGit(runtime, workspace, cfg, 4000, run)
	sections2 := registry2.mainSections(execmode.ModeWork)
	manifest2 := promptManifest(promptVariantMain, "fake/model", sections2, true)

	stableIDs := map[string]bool{
		promptSectionCodingAgent:           true,
		promptSectionRuntime:               true,
		promptSectionWorkspaceInstructions: true,
	}
	for i := range manifest1.Sections {
		s1 := manifest1.Sections[i]
		s2 := manifest2.Sections[i]
		if s1.ID != s2.ID {
			t.Fatalf("section order changed: %s vs %s", s1.ID, s2.ID)
		}
		if !stableIDs[s1.ID] {
			continue
		}
		if s1.Content != s2.Content {
			t.Fatalf("stable section %s content differs across turns:\n--- turn 1 ---\n%s\n--- turn 2 ---\n%s", s1.ID, s1.Content, s2.Content)
		}
		if !s1.Cacheable || !s2.Cacheable {
			t.Fatalf("stable section %s should be cacheable in both turns", s1.ID)
		}
	}
}

func ptrBool(v bool) *bool { return &v }
