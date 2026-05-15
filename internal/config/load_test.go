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

func TestLoadFromDirsToleratesUnknownTopLevelKeys(t *testing.T) {
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
