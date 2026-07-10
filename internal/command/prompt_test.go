package command

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/feimingxliu/ub/internal/agent"
)

func TestPromptInspectJSONRedactsContentAndHasNoStateSideEffects(t *testing.T) {
	temp := t.TempDir()
	workspace := filepath.Join(temp, "repo")
	configHome := filepath.Join(temp, "config")
	stateHome := filepath.Join(temp, "state")
	if err := os.MkdirAll(filepath.Join(configHome, "ub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "AGENTS.md"), []byte("workspace-sensitive-marker"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configHome, "ub", "instructions.md"), []byte("memory-sensitive-marker"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configHome, "ub", "config.yaml"), []byte("default_model: fake/inspect\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("XDG_DATA_HOME", filepath.Join(temp, "data"))
	t.Chdir(workspace)

	tc := newTestRootCommand("--mode", "plan", "prompt", "inspect", "--json")
	if err := tc.cmd.Execute(); err != nil {
		t.Fatalf("prompt inspect --json: %v", err)
	}
	raw := tc.out.String()
	for _, secret := range []string{"workspace-sensitive-marker", "memory-sensitive-marker", "\"content\""} {
		if strings.Contains(raw, secret) {
			t.Fatalf("default JSON leaked %q:\n%s", secret, raw)
		}
	}
	var manifest agent.PromptManifest
	if err := json.Unmarshal(tc.out.Bytes(), &manifest); err != nil {
		t.Fatalf("decode manifest: %v\n%s", err, raw)
	}
	if manifest.Variant != "main" || manifest.Model != "fake/inspect" {
		t.Fatalf("manifest header = %#v", manifest)
	}
	if len(manifest.Sections) != 6 {
		t.Fatalf("sections = %d, want 6: %#v", len(manifest.Sections), manifest.Sections)
	}
	if manifest.Sections[4].ID != "execution_mode" || manifest.Sections[4].Status != "included" {
		t.Fatalf("plan mode section = %#v", manifest.Sections[4])
	}
	if _, err := os.Stat(stateHome); !os.IsNotExist(err) {
		t.Fatalf("prompt inspect created state home or returned unexpected error: %v", err)
	}
}

func TestPromptInspectTextAndExplicitContent(t *testing.T) {
	temp := t.TempDir()
	workspace := filepath.Join(temp, "repo")
	configHome := filepath.Join(temp, "config")
	if err := os.MkdirAll(filepath.Join(configHome, "ub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "AGENTS.md"), []byte("show-workspace-content"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configHome, "ub", "instructions.md"), []byte("show-memory-content"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("XDG_STATE_HOME", filepath.Join(temp, "state"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(temp, "data"))
	t.Chdir(workspace)

	tc := newTestRootCommand("prompt", "inspect", "--show-content", "--model", "fake/explicit")
	if err := tc.cmd.Execute(); err != nil {
		t.Fatalf("prompt inspect --show-content: %v", err)
	}
	out := tc.out.String()
	for _, want := range []string{
		"variant\tmain",
		"model\tfake/explicit",
		"coding_agent\tincluded\tstable",
		"workspace_instructions\tincluded\tstable",
		"content:",
		"show-workspace-content",
		"show-memory-content",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("text output missing %q:\n%s", want, out)
		}
	}
}
