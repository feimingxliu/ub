package fs

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/feimingxliu/ub/internal/tool"
)

func previewTool(t *testing.T, tl tool.PreviewableTool, args any) tool.Preview {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	p, err := tl.Preview(context.Background(), raw)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	return p
}

func TestWrite_PreviewCreate(t *testing.T) {
	root := t.TempDir()
	w := newWriteTool(root)
	p := previewTool(t, w, writeArgs{Path: "new.txt", Content: "hello\n"})
	if len(p.Files) != 1 {
		t.Fatalf("expected 1 file diff, got %d", len(p.Files))
	}
	fd := p.Files[0]
	if fd.Kind != tool.KindCreate {
		t.Fatalf("expected create kind, got %q", fd.Kind)
	}
	if !strings.Contains(fd.UnifiedDiff, "+hello") {
		t.Fatalf("missing +hello in diff:\n%s", fd.UnifiedDiff)
	}
	if _, err := os.Stat(filepath.Join(root, "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("preview must not create file; got stat err=%v", err)
	}
}

func TestWrite_PreviewModify(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.txt", "old\n")
	w := newWriteTool(root)
	p := previewTool(t, w, writeArgs{Path: "a.txt", Content: "new\n"})
	fd := p.Files[0]
	if fd.Kind != tool.KindModify {
		t.Fatalf("expected modify kind, got %q", fd.Kind)
	}
	if !strings.Contains(fd.UnifiedDiff, "-old") || !strings.Contains(fd.UnifiedDiff, "+new") {
		t.Fatalf("expected -old/+new in diff:\n%s", fd.UnifiedDiff)
	}
	// disk untouched
	b, err := os.ReadFile(filepath.Join(root, "a.txt"))
	if err != nil {
		t.Fatalf("read after preview: %v", err)
	}
	if string(b) != "old\n" {
		t.Fatalf("preview mutated disk; got %q", b)
	}
}

func TestWrite_ExecuteCreatesParentDirs(t *testing.T) {
	root := t.TempDir()
	w := newWriteTool(root)
	res, err := execTool(t, w, writeArgs{Path: "dir/new.txt", Content: "x\n"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(res.Files) != 1 || res.Files[0].Path != "dir/new.txt" || res.Files[0].Kind != tool.KindCreate {
		t.Fatalf("unexpected Result.Files: %+v", res.Files)
	}
	b, err := os.ReadFile(filepath.Join(root, "dir", "new.txt"))
	if err != nil {
		t.Fatalf("read after execute: %v", err)
	}
	if string(b) != "x\n" {
		t.Fatalf("file content mismatch: %q", b)
	}
}

func TestWrite_OutsideRoot(t *testing.T) {
	root := t.TempDir()
	w := newWriteTool(root)
	if _, err := execTool(t, w, writeArgs{Path: "../escape", Content: "x"}); err == nil {
		t.Fatalf("expected sandbox error")
	}
}

func TestWrite_PreviewableSatisfied(t *testing.T) {
	var _ tool.PreviewableTool = newWriteTool(t.TempDir())
}
