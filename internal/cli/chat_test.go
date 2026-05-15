package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChatWithFakeProviderPrintsTextDelta(t *testing.T) {
	temp := t.TempDir()
	writeChatConfig(t, temp, `default_model: fake/test-model
providers:
  fake:
    type: fake
    script:
      - type: text_delta
        text: pong
      - type: done
`)
	t.Chdir(temp)

	cmd := newRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"chat", "--provider", "fake", "ping"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat: %v", err)
	}
	if got := out.String(); got != "pong" {
		t.Fatalf("stdout = %q, want pong", got)
	}
}

func TestChatReadsPromptFromStdin(t *testing.T) {
	temp := t.TempDir()
	writeChatConfig(t, temp, `providers:
  fake:
    type: fake
    script:
      - type: text_delta
        text: stdin-ok
      - type: done
`)
	t.Chdir(temp)

	cmd := newRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(strings.NewReader("hello from stdin"))
	cmd.SetArgs([]string{"chat", "--provider", "fake", "-"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat stdin: %v", err)
	}
	if got := out.String(); got != "stdin-ok" {
		t.Fatalf("stdout = %q, want stdin-ok", got)
	}
}

func TestChatUsesProviderOverride(t *testing.T) {
	temp := t.TempDir()
	writeChatConfig(t, temp, `default_model: first/model
providers:
  first:
    type: fake
    script:
      - type: text_delta
        text: first
  second:
    type: fake
    script:
      - type: text_delta
        text: second
`)
	t.Chdir(temp)

	cmd := newRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"chat", "--provider", "second", "hello"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat provider override: %v", err)
	}
	if got := out.String(); got != "second" {
		t.Fatalf("stdout = %q, want second", got)
	}
}

func TestChatRejectsToolCalls(t *testing.T) {
	temp := t.TempDir()
	writeChatConfig(t, temp, `providers:
  fake:
    type: fake
    script:
      - type: tool_call
        tool_name: fs.read
        input:
          path: main.go
`)
	t.Chdir(temp)

	cmd := newRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"chat", "--provider", "fake", "hello"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected tool call error")
	}
	if !strings.Contains(err.Error(), "does not execute tool calls") || !strings.Contains(err.Error(), "fs.read") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestChatInfersProviderFromDefaultModel(t *testing.T) {
	temp := t.TempDir()
	writeChatConfig(t, temp, `default_model: fake/test-model
providers:
  fake:
    type: fake
    script:
      - type: text_delta
        text: inferred
`)
	t.Chdir(temp)

	cmd := newRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"chat", "hello"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat inferred provider: %v", err)
	}
	if got := out.String(); got != "inferred" {
		t.Fatalf("stdout = %q, want inferred", got)
	}
}

func TestSelectChatProviderRequiresProvider(t *testing.T) {
	_, _, err := selectChatProvider(nil, "", "")
	if err == nil {
		t.Fatal("expected provider selection error")
	}
}

func writeChatConfig(t *testing.T, temp, content string) {
	t.Helper()
	xdg := filepath.Join(temp, "xdg")
	configPath := filepath.Join(xdg, "ub", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", xdg)
}
