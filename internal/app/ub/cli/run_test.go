package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/spf13/cobra"
)

func TestRunRendersErrorWithoutUsage(t *testing.T) {
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	code := Run([]string{"run"}, out, errOut)
	if code == 0 {
		t.Fatal("Run(run) returned success, want failure")
	}
	if !strings.Contains(errOut.String(), "error:") || !strings.Contains(errOut.String(), "prompt required") {
		t.Fatalf("stderr missing rendered error:\n%s", errOut.String())
	}
	if strings.Contains(errOut.String(), "Usage:") {
		t.Fatalf("stderr should not contain usage:\n%s", errOut.String())
	}
	if out.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", out.String())
	}
}

func TestRunWithFakeProviderPrintsFinalText(t *testing.T) {
	temp := t.TempDir()
	writeChatConfig(t, temp, `default_model: fake/test-model
providers:
  fake:
    type: fake
    script:
      - type: text_delta
        text: done
      - type: done
`)
	t.Chdir(temp)

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	code := Run([]string{"run", "--provider", "fake", "-p", "hi"}, out, errOut)
	if code != 0 {
		t.Fatalf("Run(run -p) code = %d, stderr:\n%s", code, errOut.String())
	}
	if got := out.String(); got != "done" {
		t.Fatalf("stdout = %q, want done", got)
	}
}

func TestRunModePlanParses(t *testing.T) {
	temp := t.TempDir()
	writeChatConfig(t, temp, `default_model: fake/test-model
providers:
  fake:
    type: fake
    script:
      - type: text_delta
        text: plan-ok
      - type: done
`)
	t.Chdir(temp)

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	code := Run([]string{"--mode", "plan", "run", "--provider", "fake", "-p", "hi"}, out, errOut)
	if code != 0 {
		t.Fatalf("Run(--mode plan run -p) code = %d, stderr:\n%s", code, errOut.String())
	}
	if got := out.String(); got != "plan-ok" {
		t.Fatalf("stdout = %q, want plan-ok", got)
	}
}

func TestRunHelpSucceeds(t *testing.T) {
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	code := Run([]string{"--help"}, out, errOut)
	if code != 0 {
		t.Fatalf("Run(--help) code = %d, stderr:\n%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "ub") {
		t.Fatalf("help output missing program name:\n%s", out.String())
	}
}

func TestRunInvalidLogLevel(t *testing.T) {
	t.Setenv("UB_LOG_LEVEL", "verbose")
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	code := Run([]string{"--help"}, out, errOut)
	if code == 0 {
		t.Fatal("Run with invalid log level returned success")
	}
	if !strings.Contains(errOut.String(), "invalid UB_LOG_LEVEL") {
		t.Fatalf("stderr missing invalid level:\n%s", errOut.String())
	}
}

func TestRunRecoversPanic(t *testing.T) {
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	code := runWithFactory(nil, out, errOut, func() *cobra.Command {
		return &cobra.Command{
			Use:           "panic",
			SilenceUsage:  true,
			SilenceErrors: true,
			Run: func(cmd *cobra.Command, args []string) {
				panic("boom")
			},
		}
	})
	if code == 0 {
		t.Fatal("panic command returned success")
	}
	if out.Len() != 0 {
		t.Fatalf("panic details leaked to stdout:\n%s", out.String())
	}
	if !strings.Contains(errOut.String(), "panic: boom") || !strings.Contains(errOut.String(), "runtime/debug.Stack") {
		t.Fatalf("stderr missing panic stack:\n%s", errOut.String())
	}
}

func TestRunDebugLogsToStderrAndKeepsConfigStdoutYAML(t *testing.T) {
	temp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(temp, "repo"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UB_LOG_LEVEL", "debug")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(temp, "xdg"))
	t.Chdir(filepath.Join(temp, "repo"))

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	code := Run([]string{"config", "show"}, out, errOut)
	if code != 0 {
		t.Fatalf("Run(config show) code = %d, stderr:\n%s", code, errOut.String())
	}
	if !strings.Contains(strings.ToLower(errOut.String()), "debug") {
		t.Fatalf("stderr missing debug log:\n%s", errOut.String())
	}
	var decoded map[string]any
	if err := yaml.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("stdout is not YAML:\n%s\nerr: %v", out.String(), err)
	}
	if _, ok := decoded["context"]; !ok {
		t.Fatalf("stdout missing context:\n%s", out.String())
	}
}
