package permission

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/feimingxliu/ub/internal/pkg/core/execution"
	"github.com/feimingxliu/ub/internal/pkg/runtime/approval"
	"github.com/feimingxliu/ub/internal/pkg/tool"
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

func testProjectRulesPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "project", ".ub", "permissions.yaml")
}

func mustRule(t *testing.T, raw string, action RuleAction) Rule {
	t.Helper()
	rule, err := parsePermissionRule(raw, action)
	if err != nil {
		t.Fatalf("parsePermissionRule(%q): %v", raw, err)
	}
	return rule
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
		{name: "always project command", decision: DecisionAlwaysProjectCmd, allowed: true},
		{name: "always project pattern", decision: DecisionAlwaysProjectPattern, allowed: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			projectRulesPath := testProjectRulesPath(t)
			asker := &mockAsker{decision: tc.decision}
			manager, err := NewManager(Options{
				Asker:            asker,
				ProjectRulesPath: projectRulesPath,
			})
			if err != nil {
				t.Fatalf("NewManager: %v", err)
			}
			req := execReq(t, execution.ModeWork, "git status")
			req.Workspace = filepath.Dir(filepath.Dir(projectRulesPath))
			res, err := manager.Ask(context.Background(), req)
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
		Asker: asker,
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

func TestManagerPlanRejectsExecWithoutAsker(t *testing.T) {
	asker := &mockAsker{decision: DecisionAllow}
	manager, err := NewManager(Options{
		Asker: asker,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	res, err := manager.Ask(context.Background(), execReq(t, execution.ModePlan, "git status"))
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
		Asker: asker,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	res, err := manager.Ask(context.Background(), execReq(t, execution.ModeWork, "git status"))
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

func TestManagerFullAccessAllowsExecWithoutHumanOrAgent(t *testing.T) {
	asker := &mockAsker{decision: DecisionDeny}
	agent := &mockAgent{result: approval.Result{Decision: approval.DecisionDeny}}
	manager, err := NewManager(Options{
		Asker:         asker,
		ApprovalAgent: agent,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	res, err := manager.Ask(context.Background(), execReq(t, execution.ModeFullAccess, "git status"))
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if !res.Allowed || res.Source != SourceMode || res.Reason != "allowed by full-access mode" {
		t.Fatalf("result = %#v, want full-access mode allow", res)
	}
	if asker.calls != 0 || agent.calls != 0 {
		t.Fatalf("asker/agent calls = %d/%d, want 0/0", asker.calls, agent.calls)
	}
}

func TestManagerFullAccessStillHonorsDenyAndAskRules(t *testing.T) {
	t.Run("deny rule", func(t *testing.T) {
		asker := &mockAsker{decision: DecisionAllow}
		manager, err := NewManager(Options{
			Asker:     asker,
			DenyRules: []Rule{mustRule(t, "Bash(git push:*)", RuleDeny)},
		})
		if err != nil {
			t.Fatalf("NewManager: %v", err)
		}
		res, err := manager.Ask(context.Background(), execReq(t, execution.ModeFullAccess, "git push origin main"))
		if err != nil {
			t.Fatalf("Ask: %v", err)
		}
		if res.Allowed || res.Source != SourceRule || res.Decision != DecisionDeny {
			t.Fatalf("result = %#v, want deny rule", res)
		}
		if asker.calls != 0 {
			t.Fatalf("asker calls = %d, want 0", asker.calls)
		}
	})

	t.Run("ask rule", func(t *testing.T) {
		asker := &mockAsker{decision: DecisionAllow}
		manager, err := NewManager(Options{
			Asker:    asker,
			AskRules: []Rule{mustRule(t, "Bash(git push:*)", RuleAsk)},
		})
		if err != nil {
			t.Fatalf("NewManager: %v", err)
		}
		res, err := manager.Ask(context.Background(), execReq(t, execution.ModeFullAccess, "git push origin main"))
		if err != nil {
			t.Fatalf("Ask: %v", err)
		}
		if !res.Allowed || res.Source != SourceHuman {
			t.Fatalf("result = %#v, want human allow from ask rule", res)
		}
		if asker.calls != 1 {
			t.Fatalf("asker calls = %d, want 1", asker.calls)
		}
		if got := asker.requests[0].ApprovalReason; !strings.Contains(got, "matched ask rule") {
			t.Fatalf("approval reason = %q, want ask rule reason", got)
		}
	})
}

func TestManagerFullAccessDoesNotBypassBlacklist(t *testing.T) {
	asker := &mockAsker{decision: DecisionAllow}
	manager, err := NewManager(Options{
		Asker: asker,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	res, err := manager.Ask(context.Background(), execReq(t, execution.ModeFullAccess, "rm -rf /"))
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if !res.Allowed || res.Source != SourceHuman {
		t.Fatalf("result = %#v, want human allow for blacklisted command", res)
	}
	if asker.calls != 1 {
		t.Fatalf("asker calls = %d, want 1", asker.calls)
	}
}

func TestManagerAgentApproveAllowSkipsHuman(t *testing.T) {
	asker := &mockAsker{decision: DecisionDeny}
	agent := &mockAgent{result: approval.Result{Decision: approval.DecisionAllow, Reason: "safe command"}}
	manager, err := NewManager(Options{
		Asker:         asker,
		ApprovalAgent: agent,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	res, err := manager.Ask(context.Background(), execReq(t, execution.ModeAuto, "git status"))
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

func TestManagerLogsApprovalAgentDecision(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	asker := &mockAsker{decision: DecisionDeny}
	agent := &mockAgent{result: approval.Result{Decision: approval.DecisionAllow, Reason: "safe command"}}
	manager, err := NewManager(Options{
		Asker:         asker,
		ApprovalAgent: agent,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if _, err := manager.Ask(context.Background(), execReq(t, execution.ModeAuto, "git status")); err != nil {
		t.Fatalf("Ask: %v", err)
	}
	got := logs.String()
	for _, want := range []string{"approval agent decision", "decision=allow", "reason=\"safe command\""} {
		if !strings.Contains(got, want) {
			t.Fatalf("logs missing %q:\n%s", want, got)
		}
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
				Asker:         asker,
				ApprovalAgent: agent,
			})
			if err != nil {
				t.Fatalf("NewManager: %v", err)
			}
			res, err := manager.Ask(context.Background(), execReq(t, execution.ModeAuto, "git status"))
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
		Asker:         asker,
		ApprovalAgent: agent,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if _, err := manager.Ask(context.Background(), execReq(t, execution.ModeAuto, "git status")); err != nil {
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
			Asker: asker,
		})
		if err != nil {
			t.Fatalf("NewManager: %v", err)
		}
		if _, err := manager.Ask(context.Background(), execReq(t, execution.ModeWork, "git status")); err != nil {
			t.Fatalf("first Ask: %v", err)
		}
		asker.decision = DecisionDeny
		res, err := manager.Ask(context.Background(), execReq(t, execution.ModeWork, "git status"))
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
			Asker: asker,
		})
		if err != nil {
			t.Fatalf("NewManager: %v", err)
		}
		if _, err := manager.Ask(context.Background(), execReq(t, execution.ModeWork, "git status")); err != nil {
			t.Fatalf("first Ask: %v", err)
		}
		asker.decision = DecisionDeny
		res, err := manager.Ask(context.Background(), execReq(t, execution.ModeWork, "go test ./..."))
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

func TestManagerBlacklistBypassesAllowRule(t *testing.T) {
	asker := &mockAsker{decision: DecisionAllow}
	manager, err := NewManager(Options{
		Asker:      asker,
		AllowRules: []Rule{mustRule(t, "Bash", RuleAllow)},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	res, err := manager.Ask(context.Background(), execReq(t, execution.ModeWork, "rm -rf /"))
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
		Asker:         asker,
		ApprovalAgent: agent,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	res, err := manager.Ask(context.Background(), execReq(t, execution.ModeAuto, "dd if=file of=/dev/sda"))
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

func TestManagerAlwaysProjectCommandAcrossManagers(t *testing.T) {
	path := testProjectRulesPath(t)
	workspace := filepath.Dir(filepath.Dir(path))
	asker := &mockAsker{decision: DecisionAlwaysProjectCmd}
	manager, err := NewManager(Options{
		Asker:            asker,
		ProjectRulesPath: path,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	req := execReq(t, execution.ModeWork, "git status")
	req.Workspace = workspace
	if _, err := manager.Ask(context.Background(), req); err != nil {
		t.Fatalf("Ask: %v", err)
	}

	nextAsker := &mockAsker{decision: DecisionDeny}
	nextManager, err := NewManager(Options{
		Asker:            nextAsker,
		ProjectRulesPath: path,
	})
	if err != nil {
		t.Fatalf("NewManager next: %v", err)
	}
	res, err := nextManager.Ask(context.Background(), execReq(t, execution.ModeWork, "git status"))
	if err != nil {
		t.Fatalf("Ask next same: %v", err)
	}
	if !res.Allowed || res.Source != SourceRule || !strings.Contains(res.Reason, "matched allow exact command rule") {
		t.Fatalf("result = %#v, want persisted project command rule allow", res)
	}
	res, err = nextManager.Ask(context.Background(), execReq(t, execution.ModeWork, "go test ./..."))
	if err != nil {
		t.Fatalf("Ask next different: %v", err)
	}
	if res.Allowed || res.Source != SourceHuman {
		t.Fatalf("different command result = %#v, want human deny", res)
	}
	if nextAsker.calls != 1 {
		t.Fatalf("next asker calls = %d, want 1", nextAsker.calls)
	}
}

func TestManagerAlwaysProjectPatternAcrossManagers(t *testing.T) {
	path := testProjectRulesPath(t)
	workspace := filepath.Dir(filepath.Dir(path))
	asker := &mockAsker{decision: DecisionAlwaysProjectPattern}
	manager, err := NewManager(Options{
		Asker:            asker,
		ProjectRulesPath: path,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	req := execReq(t, execution.ModeWork, "go test ./internal/permission")
	req.Workspace = workspace
	if _, err := manager.Ask(context.Background(), req); err != nil {
		t.Fatalf("Ask: %v", err)
	}

	nextAsker := &mockAsker{decision: DecisionDeny}
	nextManager, err := NewManager(Options{
		Asker:            nextAsker,
		ProjectRulesPath: path,
	})
	if err != nil {
		t.Fatalf("NewManager next: %v", err)
	}
	res, err := nextManager.Ask(context.Background(), execReq(t, execution.ModeWork, "go test ./internal/tui"))
	if err != nil {
		t.Fatalf("Ask next similar: %v", err)
	}
	if !res.Allowed || res.Source != SourceRule || !strings.Contains(res.Reason, "matched allow rule") {
		t.Fatalf("result = %#v, want persisted project command pattern allow", res)
	}
	res, err = nextManager.Ask(context.Background(), execReq(t, execution.ModeWork, "go build ./cmd/ub"))
	if err != nil {
		t.Fatalf("Ask next different: %v", err)
	}
	if res.Allowed || res.Source != SourceHuman {
		t.Fatalf("different command result = %#v, want human deny", res)
	}
	if nextAsker.calls != 1 {
		t.Fatalf("next asker calls = %d, want 1", nextAsker.calls)
	}
}

func TestManagerCommandPatternDoesNotCoverCompoundCommand(t *testing.T) {
	asker := &mockAsker{decision: DecisionDeny}
	manager, err := NewManager(Options{
		Asker:      asker,
		AllowRules: []Rule{mustRule(t, "Bash(git status:*)", RuleAllow)},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	res, err := manager.Ask(context.Background(), execReq(t, execution.ModeWork, "git status && rm -rf ./build"))
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if res.Allowed || res.Source != SourceHuman {
		t.Fatalf("result = %#v, want human deny because second subcommand is uncovered", res)
	}
	if asker.calls != 1 {
		t.Fatalf("asker calls = %d, want 1", asker.calls)
	}
}

func TestManagerCommandPatternCoversCompoundWhenEachSubcommandMatches(t *testing.T) {
	asker := &mockAsker{decision: DecisionDeny}
	manager, err := NewManager(Options{
		Asker: asker,
		AllowRules: []Rule{
			mustRule(t, "Bash(git status:*)", RuleAllow),
			mustRule(t, "Bash(go test:*)", RuleAllow),
		},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	res, err := manager.Ask(context.Background(), execReq(t, execution.ModeWork, "git status && go test ./..."))
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if !res.Allowed || res.Source != SourceRule {
		t.Fatalf("result = %#v, want rule allow", res)
	}
	if asker.calls != 0 {
		t.Fatalf("asker calls = %d, want 0", asker.calls)
	}
}

func TestManagerDenyRuleBlocksBeforeHumanOrAgent(t *testing.T) {
	asker := &mockAsker{decision: DecisionAllow}
	agent := &mockAgent{result: approval.Result{Decision: approval.DecisionAllow}}
	manager, err := NewManager(Options{
		Asker:         asker,
		ApprovalAgent: agent,
		DenyRules:     []Rule{mustRule(t, "Bash(curl:*)", RuleDeny)},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	res, err := manager.Ask(context.Background(), execReq(t, execution.ModeAuto, "curl https://example.test"))
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if res.Allowed || res.Source != SourceRule || res.Decision != DecisionDeny {
		t.Fatalf("result = %#v, want deny rule", res)
	}
	if asker.calls != 0 || agent.calls != 0 {
		t.Fatalf("asker/agent calls = %d/%d, want 0/0", asker.calls, agent.calls)
	}
}

func TestManagerAskRuleForcesHumanInAutoMode(t *testing.T) {
	asker := &mockAsker{decision: DecisionAllow}
	agent := &mockAgent{result: approval.Result{Decision: approval.DecisionAllow}}
	manager, err := NewManager(Options{
		Asker:         asker,
		ApprovalAgent: agent,
		AskRules:      []Rule{mustRule(t, "Bash(git push:*)", RuleAsk)},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	res, err := manager.Ask(context.Background(), execReq(t, execution.ModeAuto, "git push origin main"))
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if !res.Allowed || res.Source != SourceHuman {
		t.Fatalf("result = %#v, want human allow from ask rule", res)
	}
	if asker.calls != 1 || agent.calls != 0 {
		t.Fatalf("asker/agent calls = %d/%d, want 1/0", asker.calls, agent.calls)
	}
	if got := asker.requests[0].ApprovalReason; !strings.Contains(got, "matched ask rule") {
		t.Fatalf("approval reason = %q, want ask rule reason", got)
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
		Asker: asker,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	res, err := manager.Ask(context.Background(), Request{
		Tool:    "edit",
		Args:    json.RawMessage(`{"path":"notes.txt"}`),
		Risk:    tool.RiskExec,
		Mode:    execution.ModeWork,
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
