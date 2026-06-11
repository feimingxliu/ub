package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestRootHelp(t *testing.T) {
	cmd := newRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("--help: %v", err)
	}
	if !strings.Contains(out.String(), "ub") {
		t.Errorf("help output missing program name: %s", out.String())
	}
	for _, want := range []string{"--provider", "--model"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("help output missing %s flag: %s", want, out.String())
		}
	}
}

func TestSubcommandsExist(t *testing.T) {
	cmd := newRootCmd()
	for _, name := range []string{"run", "chat", "config", "doctor", "rollout", "sessions"} {
		found, _, err := cmd.Find([]string{name})
		if err != nil {
			t.Errorf("subcommand %q not found: %v", name, err)
		}
		if found == nil || found.Name() != name {
			t.Errorf("Find(%q) returned wrong command: %v", name, found)
		}
	}
}

func TestVersionNonEmpty(t *testing.T) {
	if v := Version(); v == "" {
		t.Error("Version() returned empty string")
	}
}

func TestRunWithoutPromptErrors(t *testing.T) {
	cmd := newRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"run"})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected run without prompt to fail")
	}
	if !strings.Contains(err.Error(), "prompt required") {
		t.Errorf("error %q missing prompt hint", err)
	}
}
