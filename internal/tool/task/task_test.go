package task

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/feimingxliu/ub/internal/tool"
)

type fakeRunner struct {
	calls  int
	answer string
	err    error
	depth  int
}

func (f *fakeRunner) RunSubagent(ctx context.Context, _ string, _ int) (string, error) {
	f.calls++
	f.depth = tool.SubagentDepthFromContext(ctx)
	return f.answer, f.err
}

func execTool(t *testing.T, tl tool.Tool, ctx context.Context, args any) (tool.Result, error) {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return tl.Execute(ctx, raw)
}

func TestTask_HappyPath(t *testing.T) {
	r := &fakeRunner{answer: "sub did the thing"}
	ctx := tool.WithSubagentRunner(context.Background(), r)
	res, err := execTool(t, newTaskTool(), ctx, taskArgs{Prompt: "explore"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(res.Content, "sub did the thing") {
		t.Fatalf("Content = %q", res.Content)
	}
	if r.calls != 1 {
		t.Fatalf("runner called %d times, want 1", r.calls)
	}
	if r.depth != 1 {
		t.Fatalf("runner saw depth %d, want 1", r.depth)
	}
}

func TestTask_MissingRunner(t *testing.T) {
	_, err := execTool(t, newTaskTool(), context.Background(), taskArgs{Prompt: "x"})
	if err == nil || !strings.Contains(err.Error(), "subagent runner not configured") {
		t.Fatalf("expected missing-runner error, got: %v", err)
	}
}

func TestTask_DepthLimit(t *testing.T) {
	r := &fakeRunner{answer: "should not run"}
	ctx := tool.WithSubagentRunner(context.Background(), r)
	ctx = tool.WithSubagentDepth(ctx, 1)
	_, err := execTool(t, newTaskTool(), ctx, taskArgs{Prompt: "recurse"})
	if err == nil || !strings.Contains(err.Error(), "max subagent depth") {
		t.Fatalf("expected depth error, got: %v", err)
	}
	if r.calls != 0 {
		t.Fatalf("runner ran despite depth limit: %d", r.calls)
	}
}

func TestTask_EmptyPrompt(t *testing.T) {
	ctx := tool.WithSubagentRunner(context.Background(), &fakeRunner{})
	_, err := execTool(t, newTaskTool(), ctx, taskArgs{Prompt: "   "})
	if err == nil || !strings.Contains(err.Error(), "prompt is required") {
		t.Fatalf("expected prompt-required, got: %v", err)
	}
}

func TestTask_SubagentError(t *testing.T) {
	r := &fakeRunner{answer: "partial", err: errors.New("provider down")}
	ctx := tool.WithSubagentRunner(context.Background(), r)
	res, err := execTool(t, newTaskTool(), ctx, taskArgs{Prompt: "x"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected IsError on subagent failure")
	}
	if !strings.Contains(res.Content, "provider down") || !strings.Contains(res.Content, "partial") {
		t.Fatalf("Content = %q", res.Content)
	}
}

func TestRegister(t *testing.T) {
	reg := tool.New()
	if err := Register(reg); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, ok := reg.Get("task"); !ok {
		t.Fatalf("task not registered")
	}
}
