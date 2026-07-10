package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/feimingxliu/ub/internal/message"
	execmode "github.com/feimingxliu/ub/internal/mode"
	"github.com/feimingxliu/ub/internal/provider/fake"
	"github.com/feimingxliu/ub/internal/rollout"
	"github.com/feimingxliu/ub/internal/tool"
	"github.com/feimingxliu/ub/internal/tool/fs"
)

func containsUserText(messages []message.Message, want string) bool {
	for _, m := range messages {
		if m.Role != message.RoleUser {
			continue
		}
		if strings.Contains(m.Text(), want) {
			return true
		}
	}
	return false
}

func userMessageEvents(events []rollout.Event) []rollout.Event {
	var out []rollout.Event
	for _, e := range events {
		if e.Type == rollout.TypeUserMessage {
			out = append(out, e)
		}
	}
	return out
}

// TestInjectGuidanceConsumedBetweenToolIterations verifies that guidance
// sitting in the inject channel before the run is drained between tool-loop
// iterations and reaches the provider on the next call.
func TestInjectGuidanceConsumedBetweenToolIterations(t *testing.T) {
	root := t.TempDir()
	reg := tool.New()
	if err := fs.Register(reg, root); err != nil {
		t.Fatalf("register fs: %v", err)
	}
	// First iteration: a tool call (ls) so the loop drains inject before the
	// second provider call. Second iteration: plain text ending the turn.
	p := &scriptProvider{scripts: []fake.Script{
		{fake.ToolCall("ls", map[string]any{"path": "."}), fake.Done()},
		{fake.TextDelta("adjusted per guidance"), fake.Done()},
	}}
	a := newTestAgent(t, p, reg, nil, execmode.ModeWork)
	inject := make(chan string, 4)
	inject <- "prefer absolute paths"
	a.inject = inject

	if _, err := a.Run(context.Background(), Request{SessionID: "sess_1", Prompt: "inspect dir", Turn: 1}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(p.requests) < 2 {
		t.Fatalf("want >=2 provider calls, got %d", len(p.requests))
	}
	// The second provider call must carry the injected guidance.
	if !containsUserText(p.requests[1].Messages, "prefer absolute paths") {
		t.Fatalf("second request missing injected guidance: %#v", p.requests[1].Messages)
	}
	// The first provider call must NOT carry it (drain happens after tools).
	if containsUserText(p.requests[0].Messages, "prefer absolute paths") {
		t.Fatalf("first request should not yet contain inject: %#v", p.requests[0].Messages)
	}
}

// TestInjectGuidanceFlushedWhenRunEndsWithoutToolCall verifies the defer-based
// flush: a run that ends immediately on a no-tool-call response still
// persists and returns inject guidance the loop never consumed.
func TestInjectGuidanceFlushedWhenRunEndsWithoutToolCall(t *testing.T) {
	ro := &recordingRollout{}
	reg := tool.New()
	p := &scriptProvider{scripts: []fake.Script{
		{fake.TextDelta("done"), fake.Done()},
	}}
	a, err := New(Options{
		Provider: p,
		Tools:    reg,
		Rollout:  ro,
		Model:    "fake/model",
		Mode:     execmode.ModeWork,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	inject := make(chan string, 4)
	inject <- "remember to be concise"
	a.inject = inject

	res, err := a.Run(context.Background(), Request{SessionID: "sess_1", Prompt: "hi", Turn: 1})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Guidance must be in the returned transcript so the runner's in-memory
	// history stays consistent with the rollout.
	if !containsUserText(res.Messages, "remember to be concise") {
		t.Fatalf("result Messages missing flushed inject: %#v", res.Messages)
	}
	// And persisted as a user_message reusing the current turn.
	ro.mu.Lock()
	defer ro.mu.Unlock()
	userMsgs := userMessageEvents(ro.events)
	var found bool
	for _, e := range userMsgs {
		if e.Turn == 1 && strings.Contains(string(e.Payload), "remember to be concise") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("inject not persisted at turn 1: %#v", userMsgs)
	}
}

// TestInjectGuidanceFlushedOnCancelledContext verifies that a cancelled run
// still flushes inject guidance via the background context.
func TestInjectGuidanceFlushedOnCancelledContext(t *testing.T) {
	ro := &recordingRollout{}
	reg := tool.New()
	// Provider returns an error on the first call; combined with a cancelled
	// context this exercises the error path's deferred flush.
	p := &scriptProvider{
		scripts:    []fake.Script{},
		chatErrors: []error{context.Canceled},
	}
	a, err := New(Options{
		Provider: p,
		Tools:    reg,
		Rollout:  ro,
		Model:    "fake/model",
		Mode:     execmode.ModeWork,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	inject := make(chan string, 4)
	inject <- "this should still land"
	a.inject = inject

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.Run(ctx, Request{SessionID: "sess_1", Prompt: "hi", Turn: 1}); err == nil {
		t.Fatalf("Run: want error from cancelled provider call")
	}
	ro.mu.Lock()
	defer ro.mu.Unlock()
	userMsgs := userMessageEvents(ro.events)
	var found bool
	for _, e := range userMsgs {
		if strings.Contains(string(e.Payload), "this should still land") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("inject not flushed on cancelled context: %#v", userMsgs)
	}
}
