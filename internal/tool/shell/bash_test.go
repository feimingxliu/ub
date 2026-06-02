package shell

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/feimingxliu/ub/internal/tool"
)

func execBash(t *testing.T, b *bashTool, args bashArgs) tool.Result {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	res, err := b.Execute(context.Background(), raw)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	return res
}

func shellReadFileCommand(path string) string {
	if runtime.GOOS == "windows" {
		return "type " + path
	}
	return "cat " + path
}

func shellExitCommand(code int) string {
	if runtime.GOOS == "windows" {
		return fmt.Sprintf("exit /B %d", code)
	}
	return fmt.Sprintf("exit %d", code)
}

func shellSleepCommand(seconds int) string {
	if runtime.GOOS == "windows" {
		return fmt.Sprintf("ping 127.0.0.1 -n %d >NUL", seconds+1)
	}
	return fmt.Sprintf("sleep %d", seconds)
}

func shellNoopCommand() string {
	if runtime.GOOS == "windows" {
		return "ver >NUL"
	}
	return "true"
}

func shellLongStdoutCommand(bytes int) string {
	if runtime.GOOS == "windows" {
		return fmt.Sprintf(`for /L %%i in (1,1,%d) do @<nul set /p "=x"`, bytes)
	}
	return fmt.Sprintf("awk 'BEGIN{for(i=0;i<%d;i++)printf \"x\"}'", bytes)
}

func shellReadStdinCommand() string {
	if runtime.GOOS == "windows" {
		return "more"
	}
	return "cat"
}

func TestBash_HappyPath(t *testing.T) {
	root := t.TempDir()
	b := newBashTool(root)
	res := execBash(t, b, bashArgs{Command: "echo hello"})
	if res.IsError {
		t.Fatalf("expected IsError=false, got Content:\n%s", res.Content)
	}
	if !strings.Contains(res.Content, "exit_code=0") {
		t.Errorf("missing exit_code=0 in:\n%s", res.Content)
	}
	if !strings.Contains(res.Content, "duration_ms=") {
		t.Errorf("missing duration_ms in:\n%s", res.Content)
	}
	if !strings.Contains(res.Content, "--- stdout ---\nhello") {
		t.Errorf("stdout section mismatch:\n%s", res.Content)
	}
	if !strings.Contains(res.Content, "--- stderr ---") {
		t.Errorf("missing stderr divider:\n%s", res.Content)
	}
}

func TestBash_CwdInjection(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sub, "marker.txt"), []byte("mark"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	b := newBashTool(root)
	res := execBash(t, b, bashArgs{Command: shellReadFileCommand("marker.txt"), Cwd: "sub"})
	if res.IsError || !strings.Contains(res.Content, "\nmark") {
		t.Fatalf("expected marker in stdout, got:\n%s", res.Content)
	}
}

func TestBash_NonZeroExit(t *testing.T) {
	b := newBashTool(t.TempDir())
	res := execBash(t, b, bashArgs{Command: shellExitCommand(7)})
	if !res.IsError {
		t.Fatalf("expected IsError=true")
	}
	if !strings.Contains(res.Content, "exit_code=7") {
		t.Fatalf("missing exit_code=7:\n%s", res.Content)
	}
}

func TestBash_Timeout(t *testing.T) {
	b := newBashTool(t.TempDir())
	start := time.Now()
	res := execBash(t, b, bashArgs{Command: shellSleepCommand(10), TimeoutMs: 200})
	if time.Since(start) > 5*time.Second {
		t.Fatalf("timeout took too long: %s", time.Since(start))
	}
	if !res.IsError {
		t.Fatalf("expected IsError on timeout")
	}
	if !strings.Contains(res.Content, "timeout=true") {
		t.Fatalf("missing timeout=true flag:\n%s", res.Content)
	}
	if !strings.Contains(res.Content, "<shell_metadata>") || !strings.Contains(res.Content, "</shell_metadata>") {
		t.Fatalf("missing shell_metadata block:\n%s", res.Content)
	}
}

func TestBash_Aborted(t *testing.T) {
	b := newBashTool(t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	raw, _ := json.Marshal(bashArgs{Command: shellSleepCommand(5)})
	res, err := b.Execute(ctx, raw)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(res.Content, "aborted=true") {
		t.Fatalf("missing aborted=true flag:\n%s", res.Content)
	}
	if strings.Contains(res.Content, "timeout=true") {
		t.Fatalf("unexpected timeout flag in aborted run:\n%s", res.Content)
	}
}

func TestBash_ShellMetadataBlockOnSuccess(t *testing.T) {
	b := newBashTool(t.TempDir())
	res := execBash(t, b, bashArgs{Command: shellNoopCommand()})
	if !strings.Contains(res.Content, "<shell_metadata>") || !strings.Contains(res.Content, "</shell_metadata>") {
		t.Fatalf("missing shell_metadata block:\n%s", res.Content)
	}
	if strings.Contains(res.Content, "timeout=true") || strings.Contains(res.Content, "aborted=true") {
		t.Fatalf("unexpected kill flags on happy path:\n%s", res.Content)
	}
}

func TestBash_TimeoutAcceptsNumericString(t *testing.T) {
	b := newBashTool(t.TempDir())
	raw := json.RawMessage(`{"command":"echo ok","timeout_ms":"120000"}`)
	res, err := b.Execute(context.Background(), raw)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected success, got:\n%s", res.Content)
	}
	if !strings.Contains(res.Content, "--- stdout ---\nok") {
		t.Fatalf("stdout mismatch:\n%s", res.Content)
	}
}

func TestBash_SchemaKeepsTimeoutInteger(t *testing.T) {
	raw, err := json.Marshal(newBashTool(t.TempDir()).Schema())
	if err != nil {
		t.Fatalf("marshal schema: %v", err)
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("decode schema: %v", err)
	}
	props := schemaProperties(t, schema, raw)
	timeout := props["timeout_ms"].(map[string]any)
	if timeout["type"] != "integer" {
		t.Fatalf("timeout_ms schema type = %#v, want integer\nschema=%s", timeout["type"], raw)
	}
}

func schemaProperties(t *testing.T, schema map[string]any, raw []byte) map[string]any {
	t.Helper()
	if props, ok := schema["properties"].(map[string]any); ok {
		return props
	}
	ref, _ := schema["$ref"].(string)
	const prefix = "#/$defs/"
	if !strings.HasPrefix(ref, prefix) {
		t.Fatalf("schema missing properties and usable ref: %s", raw)
	}
	defs, ok := schema["$defs"].(map[string]any)
	if !ok {
		t.Fatalf("schema missing $defs: %s", raw)
	}
	def, ok := defs[strings.TrimPrefix(ref, prefix)].(map[string]any)
	if !ok {
		t.Fatalf("schema ref %q missing definition: %s", ref, raw)
	}
	props, ok := def["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema definition missing properties: %s", raw)
	}
	return props
}

func TestBash_StdoutTruncation(t *testing.T) {
	b := newBashTool(t.TempDir())
	const target = 40000
	res := execBash(t, b, bashArgs{Command: shellLongStdoutCommand(target)})
	if res.IsError {
		t.Fatalf("expected success, got Content:\n%s", res.Content[:min(200, len(res.Content))])
	}
	// stdout section is between the two dividers.
	stdoutStart := strings.Index(res.Content, "--- stdout ---\n")
	stderrStart := strings.Index(res.Content, "\n--- stderr ---\n")
	if stdoutStart < 0 || stderrStart <= stdoutStart {
		t.Fatalf("dividers missing:\n%s", res.Content[:min(200, len(res.Content))])
	}
	stdout := res.Content[stdoutStart+len("--- stdout ---\n") : stderrStart]
	footer := fmt.Sprintf("... (truncated, total %d bytes)", target)
	if !strings.HasSuffix(stdout, footer) {
		t.Fatalf("missing truncation footer; stdout tail:\n%s", stdout[max(0, len(stdout)-100):])
	}
	bufferOnly := strings.TrimSuffix(stdout, "\n"+footer)
	if len(bufferOnly) != streamCap {
		t.Fatalf("captured stdout len = %d, want %d", len(bufferOnly), streamCap)
	}
}

func TestBash_OutsideRoot(t *testing.T) {
	b := newBashTool(t.TempDir())
	raw, _ := json.Marshal(bashArgs{Command: "pwd", Cwd: "../"})
	_, err := b.Execute(context.Background(), raw)
	if err == nil || !strings.Contains(err.Error(), "outside workspace") {
		t.Fatalf("expected sandbox error, got: %v", err)
	}
}

func TestBash_EmptyCommand(t *testing.T) {
	b := newBashTool(t.TempDir())
	raw, _ := json.Marshal(bashArgs{Command: ""})
	if _, err := b.Execute(context.Background(), raw); err == nil {
		t.Fatalf("expected empty-command error")
	}
}

func TestBash_NegativeTimeout(t *testing.T) {
	b := newBashTool(t.TempDir())
	raw, _ := json.Marshal(bashArgs{Command: shellNoopCommand(), TimeoutMs: -1})
	if _, err := b.Execute(context.Background(), raw); err == nil {
		t.Fatalf("expected negative-timeout error")
	}
}

func TestBash_StdinIsDevNull(t *testing.T) {
	b := newBashTool(t.TempDir())
	res := execBash(t, b, bashArgs{Command: shellReadStdinCommand()})
	if res.IsError {
		t.Fatalf("expected stdin reader with empty stdin to exit 0, got:\n%s", res.Content)
	}
	if !strings.Contains(res.Content, "exit_code=0") {
		t.Fatalf("missing exit_code=0:\n%s", res.Content)
	}
}
