package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGlobalConfigPathUsesXDGConfigHome(t *testing.T) {
	temp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", temp)

	got, err := globalConfigPath()
	if err != nil {
		t.Fatalf("globalConfigPath: %v", err)
	}
	want := filepath.Join(temp, "ub", "config.yaml")
	if got != want {
		t.Fatalf("globalConfigPath() = %q, want %q", got, want)
	}
}

func TestLocalConfigPathFindsNearestWithinFiveLevels(t *testing.T) {
	root := t.TempDir()
	near := filepath.Join(root, "a", "b")
	far := filepath.Join(root, "a")
	cwd := filepath.Join(near, "c", "d", "e")
	mustWriteConfig(t, filepath.Join(near, ".ub", "config.yaml"), "default_model: near\n")
	mustWriteConfig(t, filepath.Join(far, ".ub", "config.yaml"), "default_model: far\n")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}

	got := localConfigPath(cwd)
	want := filepath.Join(near, ".ub", "config.yaml")
	if got != want {
		t.Fatalf("localConfigPath() = %q, want %q", got, want)
	}
}

func TestLocalConfigPathStopsAfterFiveParents(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "a", ".ub", "config.yaml")
	cwd := filepath.Join(root, "a", "b", "c", "d", "e", "f", "g")
	mustWriteConfig(t, configPath, "default_model: too-far\n")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}

	if got := localConfigPath(cwd); got != "" {
		t.Fatalf("localConfigPath() = %q, want empty", got)
	}
}

func mustWriteConfig(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
