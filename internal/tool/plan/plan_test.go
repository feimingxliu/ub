package plan

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/feimingxliu/ub/internal/tool"
)

func freezeTime(t *testing.T, instant time.Time) {
	t.Helper()
	orig := nowFunc
	nowFunc = func() time.Time { return instant }
	t.Cleanup(func() { nowFunc = orig })
}

func execTool(t *testing.T, tl tool.Tool, args any) (tool.Result, error) {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	return tl.Execute(context.Background(), raw)
}

func testWorkspace(t *testing.T) string {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	return t.TempDir()
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"":                                "plan",
		"Fix Login Bug":                   "fix-login-bug",
		"Path/With:Funny#Chars":           "path-with-funny-chars",
		strings.Repeat("longtitle ", 20):  "longtitle-longtitle-longtitle-longtitle",
		"   leading and trailing spaces ": "leading-and-trailing-spaces",
		"-already-dashed-":                "already-dashed",
	}
	for in, want := range cases {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNewPlanID_MonotonicallyDifferentTimestamps(t *testing.T) {
	freezeTime(t, time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC))
	a := newPlanID("hello")
	freezeTime(t, time.Date(2026, 5, 27, 10, 0, 1, 0, time.UTC))
	b := newPlanID("hello")
	if a == b {
		t.Fatalf("plan id must differ with second-resolution clock advance: %s == %s", a, b)
	}
}

func TestPlanWrite_HappyPath(t *testing.T) {
	ws := testWorkspace(t)
	freezeTime(t, time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC))
	tl := newWriteTool(ws)
	res, err := execTool(t, tl, writeArgs{
		Title: "Fix Login Bug",
		Steps: []string{"reproduce", "patch", "test"},
		Notes: "see issue #42",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(res.Content, "plan_id=20260527T100000Z-fix-login-bug") {
		t.Fatalf("Content missing plan_id: %q", res.Content)
	}
	body := readPlan(t, ws, "20260527T100000Z-fix-login-bug")
	content := string(body)
	for _, want := range []string{
		"# Fix Login Bug",
		"Status: in_progress",
		"## Steps",
		"- [ ] 1. reproduce",
		"- [ ] 2. patch",
		"- [ ] 3. test",
		"## Notes",
		"see issue #42",
		"## Log",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("plan file missing %q in:\n%s", want, content)
		}
	}
	if len(res.Files) != 1 || res.Files[0].Kind != tool.KindCreate {
		t.Fatalf("Result.Files = %+v", res.Files)
	}
}

func TestPlanWrite_AcceptsJSONEncodedStepsString(t *testing.T) {
	ws := testWorkspace(t)
	freezeTime(t, time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC))
	tl := newWriteTool(ws)
	res, err := execTool(t, tl, map[string]any{
		"title": "Fix Trackpad Zoom",
		"steps": `["disable chromium pinch zoom","smooth wheel zoom","run typecheck"]`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	planID := extractPlanID(t, res.Content)
	body := readPlan(t, ws, planID)
	for _, want := range []string{
		"- [ ] 1. disable chromium pinch zoom",
		"- [ ] 2. smooth wheel zoom",
		"- [ ] 3. run typecheck",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("plan file missing %q in:\n%s", want, body)
		}
	}
}

func TestPlanWrite_EmptyStepsRejected(t *testing.T) {
	ws := testWorkspace(t)
	tl := newWriteTool(ws)
	_, err := execTool(t, tl, writeArgs{Title: "x", Steps: []string{}})
	if err == nil || !strings.Contains(err.Error(), "steps is required") {
		t.Fatalf("expected steps-required error, got: %v", err)
	}
}

func TestPlanWrite_EmptyTitleRejected(t *testing.T) {
	ws := testWorkspace(t)
	tl := newWriteTool(ws)
	_, err := execTool(t, tl, writeArgs{Title: "", Steps: []string{"x"}})
	if err == nil || !strings.Contains(err.Error(), "title is required") {
		t.Fatalf("expected title-required error, got: %v", err)
	}
}

func TestPlanRevise_UpdatesExistingPlanInPlace(t *testing.T) {
	ws := testWorkspace(t)
	freezeTime(t, time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC))
	w := newWriteTool(ws)
	res, err := execTool(t, w, writeArgs{Title: "Initial Plan", Steps: []string{"inspect", "patch"}, Notes: "draft"})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	planID := extractPlanID(t, res.Content)
	path, err := planPath(ws, planID)
	if err != nil {
		t.Fatalf("plan path: %v", err)
	}

	freezeTime(t, time.Date(2026, 5, 27, 10, 5, 0, 0, time.UTC))
	u := newReviseTool(ws)
	reviseRes, err := execTool(t, u, reviseArgs{
		PlanID: planID,
		Title:  "Corrected Plan",
		Steps:  []string{"re-read scope", "patch narrow change", "run focused tests"},
		Notes:  "corrected by user",
		Reason: "user correction",
	})
	if err != nil {
		t.Fatalf("revise: %v", err)
	}
	if !strings.Contains(reviseRes.Content, "plan_id="+planID) || !strings.Contains(reviseRes.Content, "path="+path) {
		t.Fatalf("revise content should keep same plan id/path:\n%s", reviseRes.Content)
	}
	body := readPlan(t, ws, planID)
	for _, want := range []string{
		"# Corrected Plan",
		"- [ ] 1. re-read scope",
		"- [ ] 2. patch narrow change",
		"- [ ] 3. run focused tests",
		"corrected by user",
		"plan updated: user correction",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("revised plan missing %q:\n%s", want, body)
		}
	}
	root, err := planRoot(ws)
	if err != nil {
		t.Fatalf("plan root: %v", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read plan root: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("plan_update should not create a second plan, entries=%d", len(entries))
	}
	if len(reviseRes.Files) != 1 || reviseRes.Files[0].Kind != tool.KindModify || reviseRes.Files[0].Path != path {
		t.Fatalf("revise Files = %+v, want modify %s", reviseRes.Files, path)
	}
}

func TestPlanRevise_CanClearNotes(t *testing.T) {
	ws := testWorkspace(t)
	w := newWriteTool(ws)
	res, err := execTool(t, w, writeArgs{Title: "x", Steps: []string{"a"}, Notes: "remove me"})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	planID := extractPlanID(t, res.Content)

	u := newReviseTool(ws)
	if _, err := execTool(t, u, map[string]any{
		"plan_id": planID,
		"notes":   "",
	}); err != nil {
		t.Fatalf("revise clear notes: %v", err)
	}
	body := readPlan(t, ws, planID)
	if strings.Contains(body, "remove me") {
		t.Fatalf("notes were not cleared:\n%s", body)
	}
}

func TestPlanRevise_RequiresChange(t *testing.T) {
	ws := testWorkspace(t)
	u := newReviseTool(ws)
	_, err := execTool(t, u, map[string]any{"plan_id": "missing"})
	if err == nil || !strings.Contains(err.Error(), "at least one") {
		t.Fatalf("expected change-required error, got: %v", err)
	}
}

func TestPlanUpdate_MarksDoneAndAppendsLog(t *testing.T) {
	ws := testWorkspace(t)
	freezeTime(t, time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC))
	w := newWriteTool(ws)
	res, err := execTool(t, w, writeArgs{Title: "x", Steps: []string{"a", "b", "c"}})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	planID := extractPlanID(t, res.Content)

	freezeTime(t, time.Date(2026, 5, 27, 10, 5, 0, 0, time.UTC))
	u := newUpdateTool(ws)
	if _, err := execTool(t, u, updateArgs{PlanID: planID, StepIndex: tool.IntArg(2), Status: "done", Note: "patched"}); err != nil {
		t.Fatalf("update: %v", err)
	}
	body := readPlan(t, ws, planID)
	if !strings.Contains(body, "- [x] 2. b") {
		t.Fatalf("step 2 not marked done:\n%s", body)
	}
	if !strings.Contains(body, "step 2 → done: patched") {
		t.Fatalf("log line missing:\n%s", body)
	}
	// Status must stay in_progress because steps 1 and 3 are still open.
	if !strings.Contains(body, "Status: in_progress") {
		t.Fatalf("status changed prematurely:\n%s", body)
	}
}

func TestPlanUpdate_AcceptsStringStepIndex(t *testing.T) {
	ws := testWorkspace(t)
	w := newWriteTool(ws)
	res, err := execTool(t, w, writeArgs{Title: "x", Steps: []string{"a", "b"}})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	planID := extractPlanID(t, res.Content)
	u := newUpdateTool(ws)
	if _, err := execTool(t, u, map[string]any{
		"plan_id":    planID,
		"step_index": "1",
		"status":     "done",
	}); err != nil {
		t.Fatalf("update with string step_index: %v", err)
	}
	body := readPlan(t, ws, planID)
	if !strings.Contains(body, "- [x] 1. a") {
		t.Fatalf("step 1 not marked done:\n%s", body)
	}
}

func TestPlanUpdate_InProgressIsNonTerminal(t *testing.T) {
	ws := testWorkspace(t)
	w := newWriteTool(ws)
	res, err := execTool(t, w, writeArgs{Title: "x", Steps: []string{"a"}})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	planID := extractPlanID(t, res.Content)
	u := newUpdateTool(ws)
	if _, err := execTool(t, u, map[string]any{
		"plan_id":    planID,
		"step_index": "1",
		"status":     "in_progress",
		"note":       "started",
	}); err != nil {
		t.Fatalf("update in_progress: %v", err)
	}
	body := readPlan(t, ws, planID)
	for _, want := range []string{
		"Status: in_progress",
		"- [>] 1. a",
		"step 1 → in_progress: started",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("plan file missing %q in:\n%s", want, body)
		}
	}
}

func TestPlanUpdate_AutoCompleteWhenAllDone(t *testing.T) {
	ws := testWorkspace(t)
	w := newWriteTool(ws)
	res, _ := execTool(t, w, writeArgs{Title: "x", Steps: []string{"a", "b"}})
	planID := extractPlanID(t, res.Content)
	u := newUpdateTool(ws)
	if _, err := execTool(t, u, updateArgs{PlanID: planID, StepIndex: tool.IntArg(1), Status: "done"}); err != nil {
		t.Fatalf("update 1: %v", err)
	}
	if _, err := execTool(t, u, updateArgs{PlanID: planID, StepIndex: tool.IntArg(2), Status: "skipped"}); err != nil {
		t.Fatalf("update 2: %v", err)
	}
	body := readPlan(t, ws, planID)
	if !strings.Contains(body, "Status: complete") {
		t.Fatalf("status not auto-completed:\n%s", body)
	}
}

func TestPlanUpdate_OutOfRange(t *testing.T) {
	ws := testWorkspace(t)
	w := newWriteTool(ws)
	res, _ := execTool(t, w, writeArgs{Title: "x", Steps: []string{"a"}})
	planID := extractPlanID(t, res.Content)
	u := newUpdateTool(ws)
	_, err := execTool(t, u, updateArgs{PlanID: planID, StepIndex: tool.IntArg(5), Status: "done"})
	if err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("expected out-of-range error, got: %v", err)
	}
}

func TestPlanUpdate_InvalidStatus(t *testing.T) {
	ws := testWorkspace(t)
	w := newWriteTool(ws)
	res, _ := execTool(t, w, writeArgs{Title: "x", Steps: []string{"a"}})
	planID := extractPlanID(t, res.Content)
	u := newUpdateTool(ws)
	_, err := execTool(t, u, updateArgs{PlanID: planID, StepIndex: tool.IntArg(1), Status: "wat"})
	if err == nil || !strings.Contains(err.Error(), "invalid status") {
		t.Fatalf("expected invalid status, got: %v", err)
	}
}

func TestPlanUpdate_FileNotFound(t *testing.T) {
	ws := testWorkspace(t)
	u := newUpdateTool(ws)
	_, err := execTool(t, u, updateArgs{PlanID: "missing", StepIndex: tool.IntArg(1), Status: "done"})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not-found, got: %v", err)
	}
}

func TestRegister_RegistersPlanTools(t *testing.T) {
	reg := tool.New()
	if err := Register(reg, t.TempDir()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	names := map[string]bool{}
	for _, tl := range reg.All() {
		names[tl.Name()] = true
	}
	for _, want := range []string{"plan_write", "plan_update", "plan_update_step"} {
		if !names[want] {
			t.Errorf("missing tool %s in %v", want, names)
		}
	}
}

func TestRegister_RejectsEmpties(t *testing.T) {
	if err := Register(nil, "/tmp"); err == nil {
		t.Fatalf("expected nil registry error")
	}
	if err := Register(tool.New(), ""); err == nil {
		t.Fatalf("expected empty workspace error")
	}
}

func extractPlanID(t *testing.T, content string) string {
	t.Helper()
	for _, line := range strings.Split(content, "\n") {
		if id, ok := strings.CutPrefix(line, "plan_id="); ok {
			return id
		}
	}
	t.Fatalf("plan_id not in content:\n%s", content)
	return ""
}

func readPlan(t *testing.T, ws, planID string) string {
	t.Helper()
	path, err := planPath(ws, planID)
	if err != nil {
		t.Fatalf("plan path: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read plan: %v", err)
	}
	return string(body)
}
