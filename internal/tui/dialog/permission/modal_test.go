package permissiondialog

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/feimingxliu/ub/internal/execution"
	"github.com/feimingxliu/ub/internal/permission"
	"github.com/feimingxliu/ub/internal/tool"
)

func TestDecisionForKey(t *testing.T) {
	cases := map[string]permission.Decision{
		"1": permission.DecisionAllow,
		"2": permission.DecisionDeny,
		"3": permission.DecisionAlwaysCmd,
		"4": permission.DecisionAlwaysTool,
		"5": permission.DecisionAlwaysProjectCmd,
		"6": permission.DecisionAlwaysProjectPattern,
	}
	for key, want := range cases {
		got, ok := DecisionForKey(key)
		if !ok || got != want {
			t.Fatalf("DecisionForKey(%q) = %q, %v; want %q, true", key, got, ok, want)
		}
	}
	if _, ok := DecisionForKey("x"); ok {
		t.Fatalf("unexpected decision for x")
	}
}

func TestModalRendersContextAndDiff(t *testing.T) {
	req := permission.Request{
		Tool:           "bash",
		Args:           mustJSON(t, map[string]string{"command": "go test ./..."}),
		Risk:           tool.RiskExec,
		Mode:           execution.ModePlan,
		ApprovalReason: "approval unsure",
		Preview: &tool.Preview{
			Summary: "Edit main.go",
			Files: []tool.FileDiff{{
				Path:        "main.go",
				Kind:        tool.KindModify,
				UnifiedDiff: "--- a/main.go\n+++ b/main.go\n@@\n-old\n+new\n",
			}},
		},
	}

	collapsed := New(req).View()
	for _, want := range []string{
		"tool: bash",
		"risk: exec",
		"approval agent: approval unsure",
		"preview: Edit main.go",
		"press d to show diff",
		"> Allow once",
		"Run only this request",
		"Always allow exact command in this session",
		"Always allow exact command in this project",
		"Persist a project-local allow rule",
		"Always allow similar command in this project",
	} {
		if !strings.Contains(collapsed, want) {
			t.Fatalf("collapsed view missing %q:\n%s", want, collapsed)
		}
	}
	if strings.Contains(collapsed, "[1] Allow once") {
		t.Fatalf("collapsed view should render selectable options instead of numeric-only controls:\n%s", collapsed)
	}
	if strings.Contains(collapsed, "+new") {
		t.Fatalf("collapsed view unexpectedly contains diff:\n%s", collapsed)
	}

	expanded := New(req).ToggleDiff().View()
	if !strings.Contains(expanded, "main.go") || !strings.Contains(expanded, "new") {
		t.Fatalf("expanded view missing diff:\n%s", expanded)
	}
}

func TestModalSelectsDecisionWithArrows(t *testing.T) {
	modal := New(permission.Request{Tool: "bash", Risk: tool.RiskExec})
	if got := modal.SelectedDecision(); got != permission.DecisionAllow {
		t.Fatalf("selected = %q, want allow", got)
	}
	if !modal.HandleKey("down") {
		t.Fatalf("down key not handled")
	}
	if got := modal.SelectedDecision(); got != permission.DecisionDeny {
		t.Fatalf("selected = %q, want deny", got)
	}
	if !strings.Contains(modal.View(), "> Deny") {
		t.Fatalf("view missing selected deny:\n%s", modal.View())
	}
	if !modal.HandleKey("up") {
		t.Fatalf("up key not handled")
	}
	if got := modal.SelectedDecision(); got != permission.DecisionAllow {
		t.Fatalf("selected = %q, want allow", got)
	}
}

func TestModalNavigatesDiffFiles(t *testing.T) {
	req := permission.Request{
		Tool: "edit",
		Risk: tool.RiskWrite,
		Mode: execution.ModeWork,
		Preview: &tool.Preview{
			Summary: "two files",
			Files: []tool.FileDiff{
				{Path: "a.go", Kind: tool.KindModify, UnifiedDiff: "-a\n+a\n"},
				{Path: "b.py", Kind: tool.KindModify, UnifiedDiff: "-b\n+b\n"},
			},
		},
	}
	modal := New(req).ToggleDiff()
	if got := modal.SelectedDiffPath(); got != "a.go" {
		t.Fatalf("selected = %q, want a.go", got)
	}
	if !modal.HandleKey("right") {
		t.Fatalf("right key not handled")
	}
	if got := modal.SelectedDiffPath(); got != "b.py" {
		t.Fatalf("selected = %q, want b.py", got)
	}
	if !strings.Contains(modal.View(), "b.py") {
		t.Fatalf("view missing selected file:\n%s", modal.View())
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return raw
}
