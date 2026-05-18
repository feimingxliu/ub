package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadFromDirsEmptyConfigReturnsDefaults(t *testing.T) {
	temp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(temp, "xdg"))
	cwd := filepath.Join(temp, "work")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}

	cfg, files, err := loadFromDirs(cwd)
	if err != nil {
		t.Fatalf("loadFromDirs: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("files = %#v, want empty", files)
	}
	if cfg.Context.TriggerRatio != 0.8 || cfg.Context.KeepRecentTurns != 3 || cfg.TUI.Theme == "" {
		t.Fatalf("defaults not applied: %#v", cfg)
	}
}

func TestLoadFromDirsMergesGlobalAndLocal(t *testing.T) {
	temp := t.TempDir()
	xdg := filepath.Join(temp, "xdg")
	globalPath := filepath.Join(xdg, "ub", "config.yaml")
	localPath := filepath.Join(temp, "work", ".ub", "config.yaml")
	cwd := filepath.Join(temp, "work", "sub")
	mustWriteConfig(t, globalPath, "default_model: openai/gpt-4o\nproviders:\n  openai:\n    type: openai\n    api_key: A\n    base_url: B\n")
	mustWriteConfig(t, localPath, "default_model: anthropic/claude-sonnet-4-7\nproviders:\n  openai:\n    base_url: C\n")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", xdg)

	cfg, files, err := loadFromDirs(cwd)
	if err != nil {
		t.Fatalf("loadFromDirs: %v", err)
	}
	if cfg.DefaultModel != "anthropic/claude-sonnet-4-7" {
		t.Fatalf("DefaultModel = %q", cfg.DefaultModel)
	}
	if cfg.Providers["openai"].APIKey != "" || cfg.Providers["openai"].BaseURL != "C" {
		t.Fatalf("provider merge = %#v", cfg.Providers["openai"])
	}
	if len(files) != 2 || files[0] != globalPath || files[1] != localPath {
		t.Fatalf("files = %#v", files)
	}
}

func TestLoadFromDirsExpandsEnvBeforeYAMLParse(t *testing.T) {
	temp := t.TempDir()
	xdg := filepath.Join(temp, "xdg")
	globalPath := filepath.Join(xdg, "ub", "config.yaml")
	mustWriteConfig(t, globalPath, "providers:\n  anthropic:\n    api_key: ${UB_LOAD_TEST_KEY}\n")
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("UB_LOAD_TEST_KEY", "sk-load")

	cfg, _, err := loadFromDirs(temp)
	if err != nil {
		t.Fatalf("loadFromDirs: %v", err)
	}
	if cfg.Providers["anthropic"].APIKey != "sk-load" {
		t.Fatalf("api_key = %q", cfg.Providers["anthropic"].APIKey)
	}
}

func TestLoadFromDirsParsesFakeProviderScript(t *testing.T) {
	temp := t.TempDir()
	xdg := filepath.Join(temp, "xdg")
	globalPath := filepath.Join(xdg, "ub", "config.yaml")
	mustWriteConfig(t, globalPath, `providers:
  fake:
    type: fake
    script:
      - type: text_delta
        text: pong
      - type: tool_call
        tool_name: fs.read
        tool_use_id: call-1
        input:
          path: README.md
`)
	t.Setenv("XDG_CONFIG_HOME", xdg)

	cfg, _, err := loadFromDirs(temp)
	if err != nil {
		t.Fatalf("loadFromDirs: %v", err)
	}
	script := cfg.Providers["fake"].Script
	if len(script) != 2 {
		t.Fatalf("script len = %d, want 2: %#v", len(script), script)
	}
	if script[0].Type != "text_delta" || script[0].Text != "pong" {
		t.Fatalf("text event = %#v", script[0])
	}
	input, ok := script[1].Input.(map[string]any)
	if !ok || input["path"] != "README.md" {
		t.Fatalf("tool input = %#v", script[1].Input)
	}
}

func TestLoadFromDirsParsesReasoningAndModelCapabilities(t *testing.T) {
	temp := t.TempDir()
	xdg := filepath.Join(temp, "xdg")
	globalPath := filepath.Join(xdg, "ub", "config.yaml")
	mustWriteConfig(t, globalPath, `reasoning:
  effort: high
approval_agent:
  provider: openai
  model: reviewer
  reasoning:
    effort: low
providers:
  openai:
    type: openai
    models:
      custom:
        supports_reasoning: true
        supported_efforts: [low, high]
        default_effort: low
        max_context_tokens: 200000
`)
	t.Setenv("XDG_CONFIG_HOME", xdg)

	cfg, _, err := loadFromDirs(temp)
	if err != nil {
		t.Fatalf("loadFromDirs: %v", err)
	}
	if cfg.Reasoning.Effort != "high" {
		t.Fatalf("reasoning effort = %q", cfg.Reasoning.Effort)
	}
	if cfg.ApprovalAgent.Reasoning.Effort != "low" {
		t.Fatalf("approval reasoning effort = %q", cfg.ApprovalAgent.Reasoning.Effort)
	}
	model := cfg.Providers["openai"].Models["custom"]
	if !model.SupportsReasoning || model.DefaultEffort != "low" || len(model.SupportedEfforts) != 2 || model.MaxContextTokens != 200000 {
		t.Fatalf("model config = %#v", model)
	}
}

func TestLoadFromDirsParsesProfilesWithoutSelectingThem(t *testing.T) {
	temp := t.TempDir()
	xdg := filepath.Join(temp, "xdg")
	globalPath := filepath.Join(xdg, "ub", "config.yaml")
	mustWriteConfig(t, globalPath, "profiles:\n  dev:\n    default_model: fake/model\n")
	t.Setenv("XDG_CONFIG_HOME", xdg)

	cfg, _, err := loadFromDirs(temp)
	if err != nil {
		t.Fatalf("loadFromDirs: %v", err)
	}
	if cfg.DefaultModel != "" {
		t.Fatalf("profiles should not affect Config.DefaultModel, got %q", cfg.DefaultModel)
	}
	if cfg.Profiles["dev"].DefaultModel != "fake/model" {
		t.Fatalf("profile not parsed: %#v", cfg.Profiles)
	}
}

func TestLoadFromDirsAppliesSelectedProfile(t *testing.T) {
	temp := t.TempDir()
	xdg := filepath.Join(temp, "xdg")
	globalPath := filepath.Join(xdg, "ub", "config.yaml")
	mustWriteConfig(t, globalPath,
		"default_model: fake/base\n"+
			"default_provider: fake\n"+
			"execution_mode: work\n"+
			"providers:\n"+
			"  fake:\n"+
			"    type: fake\n"+
			"profiles:\n"+
			"  dev:\n"+
			"    default_model: fake/dev\n"+
			"    default_provider: fake\n"+
			"    execution_mode: plan\n"+
			"    providers:\n"+
			"      fake:\n"+
			"        type: fake\n"+
			"        script:\n"+
			"          - type: text_delta\n"+
			"            text: dev\n")
	t.Setenv("XDG_CONFIG_HOME", xdg)

	cfg, _, err := loadFromDirsWithOptions(temp, LoadOptions{Profile: "dev"})
	if err != nil {
		t.Fatalf("loadFromDirsWithOptions: %v", err)
	}
	if cfg.DefaultModel != "fake/dev" {
		t.Fatalf("DefaultModel = %q", cfg.DefaultModel)
	}
	if cfg.DefaultProvider != "fake" {
		t.Fatalf("DefaultProvider = %q", cfg.DefaultProvider)
	}
	if cfg.ExecutionMode != ModePlan {
		t.Fatalf("ExecutionMode = %q", cfg.ExecutionMode)
	}
	if got := cfg.Providers["fake"].Script[0].Text; got != "dev" {
		t.Fatalf("profile provider replacement failed: %q", got)
	}
}

func TestLoadFromDirsAppliesUBProfile(t *testing.T) {
	temp := t.TempDir()
	xdg := filepath.Join(temp, "xdg")
	globalPath := filepath.Join(xdg, "ub", "config.yaml")
	mustWriteConfig(t, globalPath, "profiles:\n  dev:\n    default_model: fake/dev\n    default_provider: fake\n")
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("UB_PROFILE", "dev")

	cfg, _, err := loadFromDirs(temp)
	if err != nil {
		t.Fatalf("loadFromDirs: %v", err)
	}
	if cfg.DefaultModel != "fake/dev" {
		t.Fatalf("DefaultModel = %q", cfg.DefaultModel)
	}
	if cfg.DefaultProvider != "fake" {
		t.Fatalf("DefaultProvider = %q", cfg.DefaultProvider)
	}
}

func TestLoadFromDirsModeOverrideAndValidation(t *testing.T) {
	temp := t.TempDir()
	xdg := filepath.Join(temp, "xdg")
	globalPath := filepath.Join(xdg, "ub", "config.yaml")
	mustWriteConfig(t, globalPath, "execution_mode: work\nprofiles:\n  dev:\n    execution_mode: plan\n")
	t.Setenv("XDG_CONFIG_HOME", xdg)

	cfg, _, err := loadFromDirsWithOptions(temp, LoadOptions{Profile: "dev", ExecutionMode: ModeAuto})
	if err != nil {
		t.Fatalf("loadFromDirsWithOptions: %v", err)
	}
	if cfg.ExecutionMode != ModeAuto {
		t.Fatalf("ExecutionMode = %q", cfg.ExecutionMode)
	}

	_, _, err = loadFromDirsWithOptions(temp, LoadOptions{ExecutionMode: "invalid"})
	if err == nil || !strings.Contains(err.Error(), "execution_mode") {
		t.Fatalf("invalid mode error = %v", err)
	}
}

func TestLoadFromDirsProfileErrors(t *testing.T) {
	temp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(temp, "xdg"))

	_, _, err := loadFromDirsWithOptions(temp, LoadOptions{Profile: "prod"})
	if err == nil || !strings.Contains(err.Error(), "profile") {
		t.Fatalf("missing profile error = %v", err)
	}
	_, _, err = loadFromDirsWithOptions(temp, LoadOptions{Profile: "prod", Dev: true})
	if err == nil || !strings.Contains(err.Error(), "--dev") {
		t.Fatalf("conflicting profile error = %v", err)
	}
}

func TestLoadFromDirsInvalidYAMLErrorIncludesPathAndLocation(t *testing.T) {
	temp := t.TempDir()
	xdg := filepath.Join(temp, "xdg")
	globalPath := filepath.Join(xdg, "ub", "config.yaml")
	mustWriteConfig(t, globalPath, "default_model: [unterminated\n")
	t.Setenv("XDG_CONFIG_HOME", xdg)

	_, _, err := loadFromDirs(temp)
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, globalPath) {
		t.Fatalf("error missing path %q: %s", globalPath, msg)
	}
	if !strings.Contains(strings.ToLower(msg), "line") && !strings.Contains(msg, "[1:") {
		t.Fatalf("error missing approximate location: %s", msg)
	}
}
