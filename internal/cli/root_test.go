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
}

func TestSubcommandsExist(t *testing.T) {
	cmd := newRootCmd()
	for _, name := range []string{"run", "config", "sessions"} {
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

func TestPlaceholderSubcommandsErrorWithIterationHint(t *testing.T) {
	cases := []struct {
		args []string
		hint string
	}{
		{[]string{"run"}, "I-2"},
	}
	for _, c := range cases {
		t.Run(strings.Join(c.args, " "), func(t *testing.T) {
			cmd := newRootCmd()
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetArgs(c.args)
			err := cmd.Execute()
			if err == nil {
				t.Fatalf("expected error from %v, got nil", c.args)
			}
			if !strings.Contains(err.Error(), c.hint) {
				t.Errorf("error %q missing iteration hint %q", err, c.hint)
			}
		})
	}
}
