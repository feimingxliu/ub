package eval

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/feimingxliu/ub/internal/message"
	"github.com/feimingxliu/ub/internal/rollout"
	"github.com/feimingxliu/ub/internal/store"
	"github.com/feimingxliu/ub/internal/tool"
)

type recordingExecutor struct {
	request  ProcessRequest
	requests []ProcessRequest
	err      error
}

func (e *recordingExecutor) Run(ctx context.Context, request ProcessRequest) (ProcessResult, error) {
	e.request = request
	e.requests = append(e.requests, request)
	if e.err != nil {
		return ProcessResult{Stderr: "provider unavailable"}, e.err
	}
	dataHome := envValue(request.Env, "XDG_DATA_HOME")
	st, err := store.Open(filepath.Join(dataHome, "ub", "ub.db"))
	if err != nil {
		return ProcessResult{}, err
	}
	defer st.Close()
	sessionID := "sess_eval"
	if continued := argValue(request.Args, "--session"); continued != "" {
		sessionID = continued
	} else {
		if err := st.CreateSession(ctx, store.Session{ID: sessionID, Workspace: request.Dir, Title: "eval", Provider: "fake", Model: "model"}); err != nil {
			return ProcessResult{}, err
		}
	}
	ro, err := rollout.New(st)
	if err != nil {
		return ProcessResult{}, err
	}
	defer ro.Close()
	events := []rollout.Event{}
	events = append(events, mustEvent(rollout.UserMessage(sessionID, 1, message.Text(message.RoleUser, "fix"))))
	events = append(events, mustEvent(rollout.ToolResult(sessionID, 1, "call_read", "read", tool.Result{Content: "old"})))
	events = append(events, mustEvent(rollout.ToolResult(sessionID, 1, "call_edit", "edit", tool.Result{Content: "ok"})))
	events = append(events, mustEvent(rollout.UsageWithDetails(sessionID, 1, rollout.UsagePayload{InputTokens: 10, OutputTokens: 4, CacheReadTokens: 3})))
	events = append(events, mustEvent(rollout.AssistantMessage(sessionID, 1, message.Text(message.RoleAssistant, "done"))))
	for _, event := range events {
		if err := ro.Append(ctx, event); err != nil {
			return ProcessResult{}, err
		}
	}
	if err := os.WriteFile(filepath.Join(request.Dir, "result.txt"), []byte("fixed\n"), 0o644); err != nil {
		return ProcessResult{}, err
	}
	return ProcessResult{Stdout: "done"}, nil
}

func TestRunContinuesFollowupPromptsInOneSession(t *testing.T) {
	executor := &recordingExecutor{}
	task := Task{SchemaVersion: 1, Name: "followup", Prompt: "first", Followups: []string{"second"}, Assertions: Assertions{Rollout: RolloutAssertions{ToolsCalled: []string{"read"}}}}
	report, err := Run(context.Background(), TaskFile{Task: task, Dir: t.TempDir()}, RunOptions{Executor: executor, Executable: "/test/ub", KeepWorkspace: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(report.Workspace))
	if len(executor.requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(executor.requests))
	}
	if report.Provider != "fake" || report.Model != "model" {
		t.Fatalf("provider/model = %q/%q, want fake/model", report.Provider, report.Model)
	}
	if got := argValue(executor.requests[1].Args, "--session"); got != "sess_eval" {
		t.Fatalf("second --session = %q, want sess_eval", got)
	}
}

func TestRunUsesIsolatedProcessAndRollout(t *testing.T) {
	executor := &recordingExecutor{}
	exists := true
	task := Task{SchemaVersion: 1, Name: "runner", Prompt: "fix", Assertions: Assertions{
		Files:   []FileAssertion{{Path: "result.txt", Exists: &exists, Contains: []string{"fixed"}}},
		Rollout: RolloutAssertions{ToolOrder: []string{"read", "edit"}, AssistantContains: []string{"done"}},
	}}
	report, err := Run(context.Background(), TaskFile{Task: task, Dir: t.TempDir()}, RunOptions{
		Provider: "fake", Model: "model", KeepWorkspace: true, Executable: "/test/ub", Executor: executor,
	})
	if err != nil {
		t.Fatalf("Run: %v; report=%#v", err, report)
	}
	defer os.RemoveAll(filepath.Dir(report.Workspace))
	if !report.Passed || report.SessionID != "sess_eval" || report.Metrics.InputTokens != 10 || report.Metrics.CacheReadTokens != 3 {
		t.Fatalf("report = %#v", report)
	}
	if executor.request.Executable != "/test/ub" || executor.request.Dir != report.Workspace {
		t.Fatalf("request = %#v", executor.request)
	}
	if envValue(executor.request.Env, "XDG_STATE_HOME") == "" || envValue(executor.request.Env, "XDG_DATA_HOME") == "" {
		t.Fatalf("isolated env missing: %#v", executor.request.Env)
	}
	joined := strings.Join(executor.request.Args, " ")
	for _, want := range []string{"--mode full-access", "--provider fake", "--model model"} {
		if !strings.Contains(joined, want) {
			t.Errorf("args %q missing %q", joined, want)
		}
	}
}

func TestRunClassifiesAgentFailure(t *testing.T) {
	executor := &recordingExecutor{err: context.Canceled}
	task := Task{SchemaVersion: 1, Name: "failed", Prompt: "x", Assertions: Assertions{Rollout: RolloutAssertions{ToolsCalled: []string{"read"}}}}
	report, err := Run(context.Background(), TaskFile{Task: task, Dir: t.TempDir()}, RunOptions{Executor: executor, Executable: "/test/ub"})
	if err == nil || report.FailureCategory != FailureAgent || !strings.Contains(report.AgentStderr, "provider unavailable") {
		t.Fatalf("report=%#v err=%v", report, err)
	}
}

func envValue(env []string, key string) string {
	prefix := key + "="
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix)
		}
	}
	return ""
}

func argValue(args []string, key string) string {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == key {
			return args[i+1]
		}
	}
	return ""
}

func mustEvent(event rollout.Event, err error) rollout.Event {
	if err != nil {
		panic(err)
	}
	return event
}
