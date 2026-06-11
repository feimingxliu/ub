// Package hook runs shell hooks at agent lifecycle points (pre/post tool
// call, pre/post user turn) so users can attach gofmt, audit logging, lint
// gates etc. without rebuilding ub.
//
// The runner is intentionally minimal:
//
//   - hook commands are argv slices, not shell strings, so quoting bugs
//     surface in the config not in execution
//   - stdout/stderr each cap at outputCap; oversized output is dropped
//   - env passed to children is restricted to an explicit whitelist plus
//     five UB_HOOK_* variables this package always sets
//   - block semantics live ONLY on pre_tool_call; all other phases treat
//     a non-zero exit as warn so a misbehaving hook can never wedge the
//     agent loop
package hook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/feimingxliu/ub/internal/pkg/core/config"
	"github.com/feimingxliu/ub/internal/pkg/tool"
)

const (
	defaultTimeout = 10 * time.Second
	maxTimeout     = 60 * time.Second
	outputCap      = 4 * 1024

	// OnFailureWarn means a non-zero hook exit is reported but never blocks
	// the underlying tool call.
	OnFailureWarn = "warn"
	// OnFailureBlock means a non-zero hook exit on a pre_tool_call hook makes
	// the agent skip Execute and surface the hook stderr as an IsError tool
	// result. It has no effect on the other three trigger kinds.
	OnFailureBlock = "block"
)

// Kind identifies a hook trigger point.
type Kind string

const (
	KindPreToolCall  Kind = "pre_tool_call"
	KindPostToolCall Kind = "post_tool_call"
	KindPreUserTurn  Kind = "pre_user_turn"
	KindPostUserTurn Kind = "post_user_turn"
)

// Event carries the contextual data a hook can see via stdin JSON and the
// UB_HOOK_* env variables.
type Event struct {
	Kind      Kind
	SessionID string
	Turn      int
	ToolName  string
	ToolUseID string
	ToolArgs  json.RawMessage
	// Result is only populated on post_tool_call events. The runner
	// serializes a tiny slice of fields (Content / IsError) into the
	// stdin JSON; full Result.Files etc. are deliberately omitted to keep
	// hook payloads small.
	Result *tool.Result
}

// Outcome captures the result of one hook process.
type Outcome struct {
	HookIndex int
	Command   []string
	ExitCode  int
	Stdout    string
	Stderr    string
	Duration  time.Duration
	Err       error
}

// Decision is the aggregated effect of all matched hooks for one event.
// Block is only ever true for KindPreToolCall when at least one matched
// hook had OnFailure=block AND exited non-zero.
type Decision struct {
	Block    bool
	Reason   string
	Outcomes []Outcome
}

// Runner is the contract the agent uses. A nil Runner is treated as no-op
// at the call site.
type Runner interface {
	Run(ctx context.Context, event Event) Decision
}

// NopRunner is a Runner that returns an empty Decision for every event. The
// agent uses it whenever no hooks are configured.
type NopRunner struct{}

// Run implements Runner.
func (NopRunner) Run(_ context.Context, _ Event) Decision { return Decision{} }

// New returns a Runner constructed from the merged config. When no hook
// entries are configured for any kind, NopRunner is returned so callers pay
// zero runtime cost.
func New(cfg config.HooksConfig) Runner {
	if len(cfg.PreToolCall)+len(cfg.PostToolCall)+len(cfg.PreUserTurn)+len(cfg.PostUserTurn) == 0 {
		return NopRunner{}
	}
	return &shellRunner{cfg: cfg}
}

type shellRunner struct {
	cfg config.HooksConfig
}

// Run executes every hook configured for event.Kind that matches the event's
// tool filter, in order. It always runs every matched hook before returning,
// even when one of them blocks, so users see the full failure picture.
func (r *shellRunner) Run(ctx context.Context, event Event) Decision {
	specs := r.specsFor(event.Kind)
	if len(specs) == 0 {
		return Decision{}
	}
	dec := Decision{Outcomes: make([]Outcome, 0, len(specs))}
	for i, spec := range specs {
		if !matchesToolFilter(spec, event) {
			continue
		}
		out := runOne(ctx, spec, event)
		out.HookIndex = i
		dec.Outcomes = append(dec.Outcomes, out)
		if event.Kind == KindPreToolCall && spec.OnFailure == OnFailureBlock && out.ExitCode != 0 {
			dec.Block = true
			if dec.Reason == "" {
				dec.Reason = strings.TrimSpace(out.Stderr)
				if dec.Reason == "" {
					dec.Reason = fmt.Sprintf("hook %s exit %d", spec.Command[0], out.ExitCode)
				}
			}
		}
	}
	return dec
}

func (r *shellRunner) specsFor(kind Kind) []config.HookSpec {
	switch kind {
	case KindPreToolCall:
		return r.cfg.PreToolCall
	case KindPostToolCall:
		return r.cfg.PostToolCall
	case KindPreUserTurn:
		return r.cfg.PreUserTurn
	case KindPostUserTurn:
		return r.cfg.PostUserTurn
	}
	return nil
}

func matchesToolFilter(spec config.HookSpec, event Event) bool {
	// Tool-name filtering only applies to events that carry a tool name.
	if event.Kind == KindPreUserTurn || event.Kind == KindPostUserTurn {
		return true
	}
	if len(spec.Tools) == 0 {
		return true
	}
	for _, name := range spec.Tools {
		if name == event.ToolName {
			return true
		}
	}
	return false
}

func runOne(ctx context.Context, spec config.HookSpec, event Event) Outcome {
	out := Outcome{Command: append([]string(nil), spec.Command...)}
	if len(spec.Command) == 0 {
		out.Err = errors.New("hook command is empty")
		return out
	}
	timeout := spec.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	if timeout > maxTimeout {
		timeout = maxTimeout
	}
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, spec.Command[0], spec.Command[1:]...)
	// WaitDelay stops Wait from hanging on inherited stdio if the child or
	// any grandchild keeps the pipes open after the context fires. Without
	// it a "sleep 5" started by /bin/sh -c keeps Wait blocked even though
	// SIGKILL has reached the shell.
	cmd.WaitDelay = 200 * time.Millisecond
	cmd.Env = buildEnv(spec.Env, event)

	stdinBytes, err := json.Marshal(stdinPayload(event))
	if err != nil {
		out.Err = fmt.Errorf("marshal hook stdin: %w", err)
		return out
	}
	cmd.Stdin = bytes.NewReader(stdinBytes)

	var stdout, stderr cappedBuffer
	stdout.Cap = outputCap
	stderr.Cap = outputCap
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err = cmd.Run()
	out.Duration = time.Since(start)
	out.Stdout = stdout.String()
	out.Stderr = stderr.String()

	if err != nil {
		if errors.Is(cmdCtx.Err(), context.DeadlineExceeded) {
			out.Err = fmt.Errorf("hook %s timeout after %s (killed)", spec.Command[0], timeout)
			out.ExitCode = -1
			return out
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			out.ExitCode = exitErr.ExitCode()
			return out
		}
		out.Err = err
		out.ExitCode = -1
		return out
	}
	out.ExitCode = 0
	return out
}

func buildEnv(whitelist []string, event Event) []string {
	env := []string{
		"UB_HOOK_EVENT=" + string(event.Kind),
		"UB_HOOK_SESSION_ID=" + event.SessionID,
		"UB_HOOK_TURN=" + strconv.Itoa(event.Turn),
	}
	if event.ToolName != "" {
		env = append(env, "UB_HOOK_TOOL_NAME="+event.ToolName)
	}
	if event.ToolUseID != "" {
		env = append(env, "UB_HOOK_TOOL_USE_ID="+event.ToolUseID)
	}
	seen := map[string]struct{}{}
	if len(whitelist) == 0 {
		// Default fallback: PATH is required for nearly every script to
		// resolve its commands, so we always pass it.
		whitelist = []string{"PATH"}
	}
	for _, key := range whitelist {
		if key == "" {
			continue
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		if v, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+v)
		}
	}
	return env
}

func stdinPayload(event Event) map[string]any {
	payload := map[string]any{
		"event":      string(event.Kind),
		"session_id": event.SessionID,
		"turn":       event.Turn,
	}
	if event.ToolName != "" {
		toolBlock := map[string]any{
			"name":   event.ToolName,
			"use_id": event.ToolUseID,
		}
		if len(event.ToolArgs) > 0 {
			// Pass through as raw JSON so the hook sees the same shape the
			// provider saw.
			toolBlock["args"] = json.RawMessage(event.ToolArgs)
		}
		payload["tool"] = toolBlock
	}
	if event.Result != nil {
		payload["result"] = map[string]any{
			"content":  event.Result.Content,
			"is_error": event.Result.IsError,
		}
	}
	return payload
}

// cappedBuffer accepts writes up to Cap bytes and silently drops the rest.
// Drop count is tracked but not exposed; hooks that overrun are expected to
// see truncated output and rely on the runner's log/event sink to report it.
type cappedBuffer struct {
	Cap int
	buf bytes.Buffer
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if b.Cap > 0 && b.buf.Len() >= b.Cap {
		return len(p), nil
	}
	if b.Cap > 0 && b.buf.Len()+len(p) > b.Cap {
		take := b.Cap - b.buf.Len()
		_, _ = b.buf.Write(p[:take])
		return len(p), nil
	}
	return b.buf.Write(p)
}

func (b *cappedBuffer) String() string { return b.buf.String() }

// Ensure io.Writer compile-time check. Without it the field assignment of
// cmd.Stdout = &stdout still works (interface satisfaction is structural)
// but having this catches accidental signature drift.
var _ io.Writer = (*cappedBuffer)(nil)
