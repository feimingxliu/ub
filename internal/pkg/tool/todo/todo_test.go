package todo

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/feimingxliu/ub/internal/pkg/tool"
)

func todoTestContext(t *testing.T, sessionID string) context.Context {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	return tool.WithSessionID(context.Background(), sessionID)
}

func execTodo(t *testing.T, tl tool.Tool, ctx context.Context, args any) (tool.Result, error) {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	return tl.Execute(ctx, raw)
}

func TestTodoWriteAcceptsStringItemsAndAssignsIDs(t *testing.T) {
	ctx := todoTestContext(t, "sess_1")
	res, err := execTodo(t, newWriteTool(), ctx, map[string]any{
		"items": []string{"inspect code", "patch", "test"},
	})
	if err != nil {
		t.Fatalf("todo_write: %v", err)
	}
	for _, want := range []string{
		"session_id=sess_1",
		"todo_count=3",
		"- [ ] 1. inspect code",
		"- [ ] 2. patch",
		"- [ ] 3. test",
	} {
		if !strings.Contains(res.Content, want) {
			t.Fatalf("result missing %q:\n%s", want, res.Content)
		}
	}
}

func TestTodoWriteRejectsMultipleInProgress(t *testing.T) {
	ctx := todoTestContext(t, "sess_1")
	_, err := execTodo(t, newWriteTool(), ctx, map[string]any{
		"items": []map[string]any{
			{"content": "a", "status": "in_progress"},
			{"content": "b", "status": "running"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "at most one in_progress") {
		t.Fatalf("err = %v, want in_progress rejection", err)
	}
}

func TestTodoUpdatePersistsBySession(t *testing.T) {
	ctx := todoTestContext(t, "sess_1")
	if _, err := execTodo(t, newWriteTool(), ctx, map[string]any{
		"items": []map[string]any{
			{"id": "inspect", "content": "inspect code", "status": "in_progress"},
			{"id": "patch", "content": "patch"},
		},
	}); err != nil {
		t.Fatalf("todo_write: %v", err)
	}
	res, err := execTodo(t, newUpdateTool(), ctx, map[string]any{
		"id":     "inspect",
		"status": "completed",
		"note":   "read tool package",
	})
	if err != nil {
		t.Fatalf("todo_update: %v", err)
	}
	for _, want := range []string{
		"- [x] 1. inspect code {id=inspect} - read tool package",
		"- [ ] 2. patch {id=patch}",
	} {
		if !strings.Contains(res.Content, want) {
			t.Fatalf("result missing %q:\n%s", want, res.Content)
		}
	}

	res, err = execTodo(t, newUpdateTool(), ctx, map[string]any{
		"item_index": 2,
		"status":     "in_progress",
	})
	if err != nil {
		t.Fatalf("todo_update by index after reload: %v", err)
	}
	if !strings.Contains(res.Content, "- [>] 2. patch {id=patch}") {
		t.Fatalf("patch not marked in_progress:\n%s", res.Content)
	}
}

func TestTodoUpdateRequiresExistingList(t *testing.T) {
	ctx := todoTestContext(t, "sess_1")
	_, err := execTodo(t, newUpdateTool(), ctx, map[string]any{
		"item_index": 1,
		"status":     "completed",
	})
	if err == nil || !strings.Contains(err.Error(), "call todo_write first") {
		t.Fatalf("err = %v, want missing-list guidance", err)
	}
}

func TestTodoUpdateRequiresStatus(t *testing.T) {
	ctx := todoTestContext(t, "sess_1")
	if _, err := execTodo(t, newWriteTool(), ctx, map[string]any{"items": []string{"inspect"}}); err != nil {
		t.Fatalf("todo_write: %v", err)
	}
	_, err := execTodo(t, newUpdateTool(), ctx, map[string]any{"item_index": 1})
	if err == nil || !strings.Contains(err.Error(), "status is required") {
		t.Fatalf("err = %v, want status required", err)
	}
}

func TestRegister(t *testing.T) {
	reg := tool.New()
	if err := Register(reg); err != nil {
		t.Fatalf("Register: %v", err)
	}
	for _, name := range []string{"todo_write", "todo_update"} {
		if _, ok := reg.Get(name); !ok {
			t.Fatalf("%s not registered", name)
		}
	}
	if err := Register(nil); err == nil {
		t.Fatal("Register(nil) should fail")
	}
}
