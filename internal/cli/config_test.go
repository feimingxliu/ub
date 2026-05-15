package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
)

func TestConfigShowPrintsDefaultYAML(t *testing.T) {
	temp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(temp, "xdg"))
	t.Chdir(filepath.Join(temp))

	cmd := newRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"config", "show"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("config show: %v", err)
	}

	var decoded map[string]any
	if err := yaml.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("stdout is not valid YAML:\n%s\nerr: %v", out.String(), err)
	}
	if _, ok := decoded["context"]; !ok {
		t.Fatalf("stdout missing context section:\n%s", out.String())
	}
}

func TestConfigShowRedactsSecrets(t *testing.T) {
	temp := t.TempDir()
	xdg := filepath.Join(temp, "xdg")
	configPath := filepath.Join(xdg, "ub", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("providers:\n  anthropic:\n    type: anthropic\n    api_key: sk-real-key\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Chdir(temp)

	cmd := newRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"config", "show"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("config show: %v", err)
	}
	if strings.Contains(out.String(), "sk-real-key") {
		t.Fatalf("secret leaked in output:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "***") {
		t.Fatalf("redacted marker missing from output:\n%s", out.String())
	}
}

func TestConfigPath(t *testing.T) {
	temp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(temp, "empty-xdg"))
	t.Chdir(temp)

	cmd := newRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"config", "path"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("config path empty: %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != "(no config files loaded; using built-in defaults)" {
		t.Fatalf("unexpected empty path output %q", got)
	}

	xdg := filepath.Join(temp, "xdg")
	configPath := filepath.Join(xdg, "ub", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("default_model: openai/gpt-4o\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", xdg)

	cmd = newRootCmd()
	out.Reset()
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"config", "path"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("config path with file: %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != configPath {
		t.Fatalf("unexpected path output %q, want %q", got, configPath)
	}
}
