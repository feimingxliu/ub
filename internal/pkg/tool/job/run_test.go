package job

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/feimingxliu/ub/internal/pkg/tool"
)

func execTool(t *testing.T, tl tool.Tool, args any) (tool.Result, error) {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return tl.Execute(context.Background(), raw)
}

func TestRunTool_HappyPath(t *testing.T) {
	mgr := NewManager(t.TempDir())
	tl := newRunTool(mgr)
	res, err := execTool(t, tl, runArgs{Command: "echo hi"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(res.Content, "job_id=") || !strings.Contains(res.Content, "started_at=") {
		t.Fatalf("missing job_id/started_at in:\n%s", res.Content)
	}
	id := extractField(t, res.Content, "job_id=")
	if len(id) != 36 {
		t.Fatalf("job_id len=%d want 36 (uuid)", len(id))
	}
	if _, ok := mgr.Get(id); !ok {
		t.Fatalf("manager does not know job %q", id)
	}
}

func TestRunTool_EmptyCommand(t *testing.T) {
	mgr := NewManager(t.TempDir())
	tl := newRunTool(mgr)
	if _, err := execTool(t, tl, runArgs{Command: ""}); err == nil {
		t.Fatalf("expected required-command error")
	}
}

func TestRunTool_CwdOutsideRoot(t *testing.T) {
	mgr := NewManager(t.TempDir())
	tl := newRunTool(mgr)
	if _, err := execTool(t, tl, runArgs{Command: "pwd", Cwd: "../"}); err == nil {
		t.Fatalf("expected sandbox error")
	}
}

func extractField(t *testing.T, content, key string) string {
	t.Helper()
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, key) {
			return strings.TrimPrefix(line, key)
		}
	}
	t.Fatalf("missing %s in:\n%s", key, content)
	return ""
}
