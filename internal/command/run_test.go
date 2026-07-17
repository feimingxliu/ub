package command

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/feimingxliu/ub/internal/store"
	"github.com/goccy/go-yaml"
	"github.com/spf13/cobra"
)

func TestRunRendersErrorWithoutUsage(t *testing.T) {
	result := runCLITest("run")
	if result.code == 0 {
		t.Fatal("Run(run) returned success, want failure")
	}
	if !strings.Contains(result.err.String(), "error:") || !strings.Contains(result.err.String(), "prompt required") {
		t.Fatalf("stderr missing rendered error:\n%s", result.err.String())
	}
	if strings.Contains(result.err.String(), "Usage:") {
		t.Fatalf("stderr should not contain usage:\n%s", result.err.String())
	}
	if result.out.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", result.out.String())
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

	result := runCLITest("run", "--provider", "fake", "-p", "hi")
	if result.code != 0 {
		t.Fatalf("Run(run -p) code = %d, stderr:\n%s", result.code, result.err.String())
	}
	if got := result.out.String(); got != "done" {
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

	result := runCLITest("--mode", "plan", "run", "--provider", "fake", "-p", "hi")
	if result.code != 0 {
		t.Fatalf("Run(--mode plan run -p) code = %d, stderr:\n%s", result.code, result.err.String())
	}
	if got := result.out.String(); got != "plan-ok" {
		t.Fatalf("stdout = %q, want plan-ok", got)
	}
}

func TestRunHiddenSessionFlagContinuesAgentHistory(t *testing.T) {
	temp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(temp, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(temp, "state"))
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
	first := runCLITest("run", "--provider", "fake", "-p", "first")
	if first.code != 0 {
		t.Fatalf("first run code=%d stderr=%s", first.code, first.err.String())
	}
	st, err := store.Open(filepath.Join(temp, "data", "ub", "ub.db"))
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := st.ListSessions(context.Background(), temp, 10)
	if err != nil {
		_ = st.Close()
		t.Fatal(err)
	}
	_ = st.Close()
	if len(sessions) != 1 {
		t.Fatalf("sessions=%d, want 1", len(sessions))
	}
	second := runCLITest("run", "--provider", "fake", "--session", sessions[0].ID, "-p", "second")
	if second.code != 0 {
		t.Fatalf("second run code=%d stderr=%s", second.code, second.err.String())
	}
	st, err = store.Open(filepath.Join(temp, "data", "ub", "ub.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	sessions, err = st.ListSessions(context.Background(), temp, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("continued run created %d sessions, want 1", len(sessions))
	}
}

func TestRunHelpSucceeds(t *testing.T) {
	result := runCLITest("--help")
	if result.code != 0 {
		t.Fatalf("Run(--help) code = %d, stderr:\n%s", result.code, result.err.String())
	}
	if !strings.Contains(result.out.String(), "ub") {
		t.Fatalf("help output missing program name:\n%s", result.out.String())
	}
}

func TestRunInvalidLogLevel(t *testing.T) {
	t.Setenv("UB_LOG_LEVEL", "verbose")
	result := runCLITest("--help")
	if result.code == 0 {
		t.Fatal("Run with invalid log level returned success")
	}
	if !strings.Contains(result.err.String(), "invalid UB_LOG_LEVEL") {
		t.Fatalf("stderr missing invalid level:\n%s", result.err.String())
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

	result := runCLITest("config", "show")
	if result.code != 0 {
		t.Fatalf("Run(config show) code = %d, stderr:\n%s", result.code, result.err.String())
	}
	if !strings.Contains(strings.ToLower(result.err.String()), "debug") {
		t.Fatalf("stderr missing debug log:\n%s", result.err.String())
	}
	var decoded map[string]any
	if err := yaml.Unmarshal(result.out.Bytes(), &decoded); err != nil {
		t.Fatalf("stdout is not YAML:\n%s\nerr: %v", result.out.String(), err)
	}
	if _, ok := decoded["context"]; !ok {
		t.Fatalf("stdout missing context:\n%s", result.out.String())
	}
}
