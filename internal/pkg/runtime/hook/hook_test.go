package hook

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/feimingxliu/ub/internal/pkg/core/config"
	"github.com/feimingxliu/ub/internal/pkg/tool"
)

func TestNew_NopRunnerForEmptyConfig(t *testing.T) {
	r := New(config.HooksConfig{})
	if _, ok := r.(NopRunner); !ok {
		t.Fatalf("empty config should yield NopRunner, got %T", r)
	}
	if dec := r.Run(context.Background(), Event{Kind: KindPreToolCall}); dec.Block || len(dec.Outcomes) != 0 {
		t.Fatalf("NopRunner produced non-empty decision: %#v", dec)
	}
}

func TestShellRunner_PreToolCall_Success(t *testing.T) {
	if !hasShell() {
		t.Skip("no shell available")
	}
	cfg := config.HooksConfig{PreToolCall: []config.HookSpec{
		{Command: shellCommand("echo hello && exit 0")},
	}}
	dec := New(cfg).Run(context.Background(), Event{Kind: KindPreToolCall, ToolName: "edit"})
	if dec.Block {
		t.Fatalf("unexpected block: %#v", dec)
	}
	if len(dec.Outcomes) != 1 {
		t.Fatalf("expected 1 outcome, got %d", len(dec.Outcomes))
	}
	if dec.Outcomes[0].ExitCode != 0 || !strings.Contains(dec.Outcomes[0].Stdout, "hello") {
		t.Fatalf("outcome: %#v", dec.Outcomes[0])
	}
}

func TestShellRunner_PreToolCall_BlockOnFailure(t *testing.T) {
	if !hasShell() {
		t.Skip("no shell available")
	}
	cfg := config.HooksConfig{PreToolCall: []config.HookSpec{
		{
			Command:   shellCommand("echo refused: secret tools blocked 1>&2 && exit 7"),
			OnFailure: OnFailureBlock,
		},
	}}
	dec := New(cfg).Run(context.Background(), Event{Kind: KindPreToolCall, ToolName: "bash"})
	if !dec.Block {
		t.Fatalf("expected block, got %#v", dec)
	}
	if !strings.Contains(dec.Reason, "refused") {
		t.Fatalf("reason should carry stderr: %q", dec.Reason)
	}
	if dec.Outcomes[0].ExitCode != 7 {
		t.Fatalf("exit code = %d, want 7", dec.Outcomes[0].ExitCode)
	}
}

func TestShellRunner_PreToolCall_WarnOnFailure(t *testing.T) {
	if !hasShell() {
		t.Skip("no shell available")
	}
	cfg := config.HooksConfig{PreToolCall: []config.HookSpec{
		{Command: shellCommand("exit 3")}, // OnFailure default = warn
	}}
	dec := New(cfg).Run(context.Background(), Event{Kind: KindPreToolCall, ToolName: "bash"})
	if dec.Block {
		t.Fatalf("warn must not block: %#v", dec)
	}
	if dec.Outcomes[0].ExitCode != 3 {
		t.Fatalf("exit code: %d", dec.Outcomes[0].ExitCode)
	}
}

func TestShellRunner_PostToolCall_BlockIsIgnored(t *testing.T) {
	if !hasShell() {
		t.Skip("no shell available")
	}
	cfg := config.HooksConfig{PostToolCall: []config.HookSpec{
		{Command: shellCommand("exit 5"), OnFailure: OnFailureBlock},
	}}
	dec := New(cfg).Run(context.Background(), Event{Kind: KindPostToolCall, ToolName: "bash"})
	if dec.Block {
		t.Fatalf("post_tool_call block must be ignored: %#v", dec)
	}
}

func TestShellRunner_ToolFilter_Mismatch(t *testing.T) {
	if !hasShell() {
		t.Skip("no shell available")
	}
	tmp := t.TempDir()
	sentinel := filepath.Join(tmp, "ran")
	cfg := config.HooksConfig{PreToolCall: []config.HookSpec{
		{Command: shellCommand("touch " + sentinel), Tools: []string{"edit"}},
	}}
	dec := New(cfg).Run(context.Background(), Event{Kind: KindPreToolCall, ToolName: "bash"})
	if len(dec.Outcomes) != 0 {
		t.Fatalf("unexpected outcomes: %#v", dec.Outcomes)
	}
	if _, err := os.Stat(sentinel); err == nil {
		t.Fatalf("hook ran despite tool mismatch")
	}
}

func TestShellRunner_ToolFilter_Match(t *testing.T) {
	if !hasShell() {
		t.Skip("no shell available")
	}
	cfg := config.HooksConfig{PreToolCall: []config.HookSpec{
		{Command: shellCommand("exit 0"), Tools: []string{"edit", "write"}},
	}}
	dec := New(cfg).Run(context.Background(), Event{Kind: KindPreToolCall, ToolName: "edit"})
	if len(dec.Outcomes) != 1 {
		t.Fatalf("hook should fire on match: %#v", dec.Outcomes)
	}
}

func TestShellRunner_Timeout(t *testing.T) {
	if !hasShell() {
		t.Skip("no shell available")
	}
	cfg := config.HooksConfig{PreToolCall: []config.HookSpec{
		{Command: shellCommand("sleep 5"), Timeout: 50 * time.Millisecond},
	}}
	start := time.Now()
	dec := New(cfg).Run(context.Background(), Event{Kind: KindPreToolCall, ToolName: "bash"})
	elapsed := time.Since(start)
	if elapsed > 500*time.Millisecond {
		t.Fatalf("hook did not honor timeout, elapsed = %s", elapsed)
	}
	if len(dec.Outcomes) != 1 {
		t.Fatalf("outcome count = %d", len(dec.Outcomes))
	}
	if dec.Outcomes[0].Err == nil || !strings.Contains(dec.Outcomes[0].Err.Error(), "timeout") {
		t.Fatalf("expected timeout err, got: %v", dec.Outcomes[0].Err)
	}
}

func TestShellRunner_EnvWhitelist(t *testing.T) {
	if !hasShell() {
		t.Skip("no shell available")
	}
	t.Setenv("MY_SECRET", "leak-me")
	t.Setenv("ALLOWED_VAR", "ok")
	cfg := config.HooksConfig{PreToolCall: []config.HookSpec{
		{
			Command: shellCommand("echo secret=${MY_SECRET:-unset} && echo allowed=${ALLOWED_VAR:-unset} && echo hook=${UB_HOOK_EVENT:-unset}"),
			Env:     []string{"ALLOWED_VAR"},
		},
	}}
	dec := New(cfg).Run(context.Background(), Event{
		Kind:      KindPreToolCall,
		ToolName:  "bash",
		SessionID: "s1",
		Turn:      2,
	})
	out := dec.Outcomes[0].Stdout
	if !strings.Contains(out, "secret=unset") {
		t.Fatalf("MY_SECRET leaked to child:\n%s", out)
	}
	if !strings.Contains(out, "allowed=ok") {
		t.Fatalf("whitelisted var missing:\n%s", out)
	}
	if !strings.Contains(out, "hook=pre_tool_call") {
		t.Fatalf("UB_HOOK_EVENT missing:\n%s", out)
	}
}

func TestShellRunner_StdinJSON(t *testing.T) {
	if !hasShell() {
		t.Skip("no shell available")
	}
	cfg := config.HooksConfig{PostToolCall: []config.HookSpec{
		{Command: shellCommand("cat")},
	}}
	res := tool.Result{Content: "hi", IsError: false}
	dec := New(cfg).Run(context.Background(), Event{
		Kind:      KindPostToolCall,
		SessionID: "sess",
		Turn:      3,
		ToolName:  "bash",
		ToolUseID: "tu_1",
		ToolArgs:  json.RawMessage(`{"cmd":"ls"}`),
		Result:    &res,
	})
	var payload map[string]any
	if err := json.Unmarshal([]byte(dec.Outcomes[0].Stdout), &payload); err != nil {
		t.Fatalf("stdin payload not JSON: %v\n%s", err, dec.Outcomes[0].Stdout)
	}
	if payload["event"] != "post_tool_call" {
		t.Fatalf("event = %v", payload["event"])
	}
	if payload["session_id"] != "sess" {
		t.Fatalf("session_id = %v", payload["session_id"])
	}
	toolBlock := payload["tool"].(map[string]any)
	if toolBlock["name"] != "bash" || toolBlock["use_id"] != "tu_1" {
		t.Fatalf("tool block = %#v", toolBlock)
	}
	result := payload["result"].(map[string]any)
	if result["content"] != "hi" || result["is_error"] != false {
		t.Fatalf("result block = %#v", result)
	}
}

func TestShellRunner_OutputCap(t *testing.T) {
	if !hasShell() {
		t.Skip("no shell available")
	}
	// Print ~10KB to stdout; cap is 4KB.
	cfg := config.HooksConfig{PreToolCall: []config.HookSpec{
		{Command: shellCommand("python3 -c \"print('a' * 10240, end='')\" 2>/dev/null || yes a | head -c 10240")},
	}}
	dec := New(cfg).Run(context.Background(), Event{Kind: KindPreToolCall, ToolName: "edit"})
	if len(dec.Outcomes) != 1 {
		t.Fatalf("outcome count: %d", len(dec.Outcomes))
	}
	if len(dec.Outcomes[0].Stdout) > outputCap {
		t.Fatalf("stdout exceeded cap: %d > %d", len(dec.Outcomes[0].Stdout), outputCap)
	}
}

func TestShellRunner_UserTurnIgnoresToolFilter(t *testing.T) {
	if !hasShell() {
		t.Skip("no shell available")
	}
	cfg := config.HooksConfig{PreUserTurn: []config.HookSpec{
		{Command: shellCommand("exit 0"), Tools: []string{"edit"}},
	}}
	dec := New(cfg).Run(context.Background(), Event{Kind: KindPreUserTurn})
	if len(dec.Outcomes) != 1 {
		t.Fatalf("user-turn hook should ignore tools filter: %#v", dec.Outcomes)
	}
}

func TestShellRunner_EmptyCommandError(t *testing.T) {
	cfg := config.HooksConfig{PreToolCall: []config.HookSpec{
		{Command: nil},
	}}
	dec := New(cfg).Run(context.Background(), Event{Kind: KindPreToolCall, ToolName: "x"})
	if len(dec.Outcomes) != 1 || dec.Outcomes[0].Err == nil {
		t.Fatalf("expected err outcome: %#v", dec)
	}
}
