package permission

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/feimingxliu/ub/internal/approval"
	"github.com/feimingxliu/ub/internal/execution"
	"github.com/feimingxliu/ub/internal/tool"
)

type mockAsker struct {
	decision Decision
	err      error
	calls    int
	requests []Request
}

func (m *mockAsker) Ask(_ context.Context, req Request) (Decision, error) {
	m.calls++
	m.requests = append(m.requests, req)
	if m.err != nil {
		return "", m.err
	}
	return m.decision, nil
}

type mockAgent struct {
	result   approval.Result
	err      error
	calls    int
	requests []approval.Request
}

func (m *mockAgent) ReviewCommand(_ context.Context, req approval.Request) (approval.Result, error) {
	m.calls++
	m.requests = append(m.requests, req)
	return m.result, m.err
}

func testRulesPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "ub", "permissions.yaml")
}

func commandArgs(t *testing.T, command string) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]string{
		"command": command,
		"cwd":     ".",
	})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	return raw
}

func execReq(t *testing.T, mode execution.Mode, command string) Request {
	t.Helper()
	return Request{
		Tool: "bash",
		Args: commandArgs(t, command),
		Risk: tool.RiskExec,
		Mode: mode,
	}
}

func TestManagerHumanDecisionPaths(t *testing.T) {
	cases := []struct {
		name     string
		decision Decision
		allowed  bool
	}{
		{name: "allow", decision: DecisionAllow, allowed: true},
		{name: "deny", decision: DecisionDeny, allowed: false},
		{name: "always command", decision: DecisionAlwaysCmd, allowed: true},
		{name: "always tool", decision: DecisionAlwaysTool, allowed: true},
		{name: "always global", decision: DecisionAlwaysGlobal, allowed: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			asker := &mockAsker{decision: tc.decision}
			manager, err := NewManager(Options{
				Asker:           asker,
				GlobalRulesPath: testRulesPath(t),
			})
			if err != nil {
				t.Fatalf("NewManager: %v", err)
			}
			res, err := manager.Ask(context.Background(), execReq(t, execution.ModeDefault, "git status"))
			if err != nil {
				t.Fatalf("Ask: %v", err)
			}
			if res.Decision != tc.decision || res.Allowed != tc.allowed || res.Source != SourceHuman {
				t.Fatalf("result = %#v, want decision=%q allowed=%v source=%q", res, tc.decision, tc.allowed, SourceHuman)
			}
			if asker.calls != 1 {
				t.Fatalf("asker calls = %d, want 1", asker.calls)
			}
		})
	}
}

func TestManagerPlanRejectsWriteWithoutAsker(t *testing.T) {
	asker := &mockAsker{decision: DecisionAllow}
	manager, err := NewManager(Options{
		Asker:           asker,
		GlobalRulesPath: testRulesPath(t),
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	res, err := manager.Ask(context.Background(), Request{
		Tool: "write",
		Args: json.RawMessage(`{"path":"notes.txt","content":"x"}`),
		Risk: tool.RiskWrite,
		Mode: execution.ModePlan,
	})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if res.Allowed || res.Decision != DecisionDeny || res.Source != SourceMode {
		t.Fatalf("result = %#v, want mode denial", res)
	}
	if asker.calls != 0 {
		t.Fatalf("asker calls = %d, want 0", asker.calls)
	}
}

func TestManagerDefaultExecUsesHumanAsker(t *testing.T) {
	asker := &mockAsker{decision: DecisionAllow}
	manager, err := NewManager(Options{
		Asker:           asker,
		GlobalRulesPath: testRulesPath(t),
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	res, err := manager.Ask(context.Background(), execReq(t, execution.ModeDefault, "git status"))
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if !res.Allowed || res.Source != SourceHuman {
		t.Fatalf("result = %#v, want human allow", res)
	}
	if asker.calls != 1 {
		t.Fatalf("asker calls = %d, want 1", asker.calls)
	}
}

func TestManagerAgentApproveAllowSkipsHuman(t *testing.T) {
	asker := &mockAsker{decision: DecisionDeny}
	agent := &mockAgent{result: approval.Result{Decision: approval.DecisionAllow, Reason: "safe command"}}
	manager, err := NewManager(Options{
		Asker:           asker,
		ApprovalAgent:   agent,
		GlobalRulesPath: testRulesPath(t),
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	res, err := manager.Ask(context.Background(), execReq(t, execution.ModeAgentApprove, "git status"))
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if !res.Allowed || res.Source != SourceApprovalAgent || res.Reason != "safe command" {
		t.Fatalf("result = %#v, want approval-agent allow", res)
	}
	if agent.calls != 1 {
		t.Fatalf("agent calls = %d, want 1", agent.calls)
	}
	if asker.calls != 0 {
		t.Fatalf("asker calls = %d, want 0", asker.calls)
	}
}

func TestManagerAgentApproveFallbacksToHuman(t *testing.T) {
	cases := []struct {
		name   string
		result approval.Result
		err    error
	}{
		{name: "deny", result: approval.Result{Decision: approval.DecisionDeny}},
		{name: "unsure", result: approval.Result{Decision: approval.DecisionUnsure}},
		{name: "error", err: errors.New("review failed")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			asker := &mockAsker{decision: DecisionAllow}
			agent := &mockAgent{result: tc.result, err: tc.err}
			manager, err := NewManager(Options{
				Asker:           asker,
				ApprovalAgent:   agent,
				GlobalRulesPath: testRulesPath(t),
			})
			if err != nil {
				t.Fatalf("NewManager: %v", err)
			}
			res, err := manager.Ask(context.Background(), execReq(t, execution.ModeAgentApprove, "git status"))
			if err != nil {
				t.Fatalf("Ask: %v", err)
			}
			if !res.Allowed || res.Source != SourceHuman {
				t.Fatalf("result = %#v, want human fallback allow", res)
			}
			if agent.calls != 1 {
				t.Fatalf("agent calls = %d, want 1", agent.calls)
			}
			if asker.calls != 1 {
				t.Fatalf("asker calls = %d, want 1", asker.calls)
			}
		})
	}
}

func TestManagerPassesApprovalReasonToHuman(t *testing.T) {
	asker := &mockAsker{decision: DecisionAllow}
	agent := &mockAgent{result: approval.Result{Decision: approval.DecisionUnsure, Reason: "needs repo context"}}
	manager, err := NewManager(Options{
		Asker:           asker,
		ApprovalAgent:   agent,
		GlobalRulesPath: testRulesPath(t),
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if _, err := manager.Ask(context.Background(), execReq(t, execution.ModeAgentApprove, "git status")); err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if asker.calls != 1 {
		t.Fatalf("asker calls = %d, want 1", asker.calls)
	}
	if got := asker.requests[0].ApprovalReason; got != "needs repo context" {
		t.Fatalf("approval reason = %q, want needs repo context", got)
	}
}

func TestManagerSessionRules(t *testing.T) {
	t.Run("always command matches exact command", func(t *testing.T) {
		asker := &mockAsker{decision: DecisionAlwaysCmd}
		manager, err := NewManager(Options{
			Asker:           asker,
			GlobalRulesPath: testRulesPath(t),
		})
		if err != nil {
			t.Fatalf("NewManager: %v", err)
		}
		if _, err := manager.Ask(context.Background(), execReq(t, execution.ModeDefault, "git status")); err != nil {
			t.Fatalf("first Ask: %v", err)
		}
		asker.decision = DecisionDeny
		res, err := manager.Ask(context.Background(), execReq(t, execution.ModeDefault, "git status"))
		if err != nil {
			t.Fatalf("second Ask: %v", err)
		}
		if !res.Allowed || res.Source != SourceRule {
			t.Fatalf("result = %#v, want session rule allow", res)
		}
		if asker.calls != 1 {
			t.Fatalf("asker calls = %d, want 1", asker.calls)
		}
	})

	t.Run("always tool matches different commands", func(t *testing.T) {
		asker := &mockAsker{decision: DecisionAlwaysTool}
		manager, err := NewManager(Options{
			Asker:           asker,
			GlobalRulesPath: testRulesPath(t),
		})
		if err != nil {
			t.Fatalf("NewManager: %v", err)
		}
		if _, err := manager.Ask(context.Background(), execReq(t, execution.ModeDefault, "git status")); err != nil {
			t.Fatalf("first Ask: %v", err)
		}
		asker.decision = DecisionDeny
		res, err := manager.Ask(context.Background(), execReq(t, execution.ModeDefault, "go test ./..."))
		if err != nil {
			t.Fatalf("second Ask: %v", err)
		}
		if !res.Allowed || res.Source != SourceRule {
			t.Fatalf("result = %#v, want session rule allow", res)
		}
		if asker.calls != 1 {
			t.Fatalf("asker calls = %d, want 1", asker.calls)
		}
	})
}

func TestManagerBlacklistBypassesGlobalRule(t *testing.T) {
	asker := &mockAsker{decision: DecisionAllow}
	manager, err := NewManager(Options{
		Asker:           asker,
		GlobalRulesPath: testRulesPath(t),
		GlobalRules:     []Rule{{Tool: "bash"}},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	res, err := manager.Ask(context.Background(), execReq(t, execution.ModeDefault, "rm -rf /"))
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if !res.Allowed || res.Source != SourceHuman {
		t.Fatalf("result = %#v, want human allow", res)
	}
	if asker.calls != 1 {
		t.Fatalf("asker calls = %d, want 1", asker.calls)
	}
}

func TestManagerBlacklistBypassesApprovalAgent(t *testing.T) {
	asker := &mockAsker{decision: DecisionAllow}
	agent := &mockAgent{result: approval.Result{Decision: approval.DecisionAllow}}
	manager, err := NewManager(Options{
		Asker:           asker,
		ApprovalAgent:   agent,
		GlobalRulesPath: testRulesPath(t),
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	res, err := manager.Ask(context.Background(), execReq(t, execution.ModeAgentApprove, "dd if=file of=/dev/sda"))
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if !res.Allowed || res.Source != SourceHuman {
		t.Fatalf("result = %#v, want human allow", res)
	}
	if agent.calls != 0 {
		t.Fatalf("agent calls = %d, want 0", agent.calls)
	}
	if asker.calls != 1 {
		t.Fatalf("asker calls = %d, want 1", asker.calls)
	}
}

func TestManagerAlwaysGlobalAcrossManagers(t *testing.T) {
	path := testRulesPath(t)
	asker := &mockAsker{decision: DecisionAlwaysGlobal}
	manager, err := NewManager(Options{
		Asker:           asker,
		GlobalRulesPath: path,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if _, err := manager.Ask(context.Background(), execReq(t, execution.ModeDefault, "git status")); err != nil {
		t.Fatalf("Ask: %v", err)
	}

	nextAsker := &mockAsker{decision: DecisionDeny}
	nextManager, err := NewManager(Options{
		Asker:           nextAsker,
		GlobalRulesPath: path,
	})
	if err != nil {
		t.Fatalf("NewManager next: %v", err)
	}
	res, err := nextManager.Ask(context.Background(), execReq(t, execution.ModeDefault, "go test ./..."))
	if err != nil {
		t.Fatalf("Ask next: %v", err)
	}
	if !res.Allowed || res.Source != SourceRule {
		t.Fatalf("result = %#v, want persisted global rule allow", res)
	}
	if nextAsker.calls != 0 {
		t.Fatalf("next asker calls = %d, want 0", nextAsker.calls)
	}
}

func TestManagerPreviewPassedToAsker(t *testing.T) {
	preview := &tool.Preview{
		Summary: "modify file",
		Files: []tool.FileDiff{{
			Path: "notes.txt",
			Kind: tool.KindModify,
		}},
	}
	asker := &mockAsker{decision: DecisionAllow}
	manager, err := NewManager(Options{
		Asker:           asker,
		GlobalRulesPath: testRulesPath(t),
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	res, err := manager.Ask(context.Background(), Request{
		Tool:    "edit",
		Args:    json.RawMessage(`{"path":"notes.txt"}`),
		Risk:    tool.RiskExec,
		Mode:    execution.ModeDefault,
		Preview: preview,
	})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if !res.Allowed {
		t.Fatalf("result = %#v, want allow", res)
	}
	if asker.calls != 1 {
		t.Fatalf("asker calls = %d, want 1", asker.calls)
	}
	if asker.requests[0].Preview != preview {
		t.Fatalf("preview pointer was not preserved")
	}
}
