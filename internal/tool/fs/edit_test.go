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

func TestEdit_PreviewSingleMatch(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.go", "foo\nbar\n")
	e := newEditTool(root)
	p := previewTool(t, e, editArgs{Path: "a.go", Old: "foo", New: "baz"})
	if len(p.Files) != 1 {
		t.Fatalf("expected 1 file diff, got %d", len(p.Files))
	}
	if !strings.Contains(p.Files[0].UnifiedDiff, "-foo") || !strings.Contains(p.Files[0].UnifiedDiff, "+baz") {
		t.Fatalf("diff missing -foo/+baz:\n%s", p.Files[0].UnifiedDiff)
	}
	b, err := os.ReadFile(filepath.Join(root, "a.go"))
	if err != nil {
		t.Fatalf("read after preview: %v", err)
	}
	if string(b) != "foo\nbar\n" {
		t.Fatalf("preview mutated disk: %q", b)
	}
}

func TestEdit_MultiMatchWithoutReplaceAll(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "b.go", "x\nx\n")
	e := newEditTool(root)
	_, err := execTool(t, e, editArgs{Path: "b.go", Old: "x", New: "y"})
	if err == nil || !strings.Contains(err.Error(), "replace_all=true") {
		t.Fatalf("expected replace_all hint, got: %v", err)
	}
	b, _ := os.ReadFile(filepath.Join(root, "b.go"))
	if string(b) != "x\nx\n" {
		t.Fatalf("disk changed on error path: %q", b)
	}
}

func TestEdit_MultiMatchReplaceAll(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "c.go", "x\nx\n")
	e := newEditTool(root)
	res, err := execTool(t, e, editArgs{Path: "c.go", Old: "x", New: "y", ReplaceAll: true})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	b, _ := os.ReadFile(filepath.Join(root, "c.go"))
	if string(b) != "y\ny\n" {
		t.Fatalf("content mismatch: %q", b)
	}
	if len(res.Files) != 1 || res.Files[0].Kind != tool.KindModify {
		t.Fatalf("Result.Files: %+v", res.Files)
	}
	if !strings.Contains(res.Files[0].UnifiedDiff, "-x") || !strings.Contains(res.Files[0].UnifiedDiff, "+y") {
		t.Fatalf("execute result diff missing -x/+y:\n%s", res.Files[0].UnifiedDiff)
	}
}

func TestEdit_ReplaceAllAcceptsBooleanString(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "c.go", "x\nx\n")
	e := newEditTool(root)
	_, err := e.Execute(context.Background(), json.RawMessage(`{"path":"c.go","old":"x","new":"y","replace_all":"true"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	b, _ := os.ReadFile(filepath.Join(root, "c.go"))
	if string(b) != "y\ny\n" {
		t.Fatalf("content mismatch: %q", b)
	}
}

func TestEdit_SchemaKeepsReplaceAllBoolean(t *testing.T) {
	raw, err := json.Marshal(newEditTool(t.TempDir()).Schema())
	if err != nil {
		t.Fatalf("marshal schema: %v", err)
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("decode schema: %v", err)
	}
	props := schemaProperties(t, schema, raw)
	replaceAll := props["replace_all"].(map[string]any)
	if replaceAll["type"] != "boolean" {
		t.Fatalf("replace_all schema type = %#v, want boolean\nschema=%s", replaceAll["type"], raw)
	}
}

func TestEdit_OldNotFound(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "d.go", "hello\n")
	e := newEditTool(root)
	_, err := execTool(t, e, editArgs{Path: "d.go", Old: "missing", New: "x"})
	if err == nil || !strings.Contains(err.Error(), "old string not found") {
		t.Fatalf("expected not-found error, got: %v", err)
	}
}

// TestEdit_ExecuteDetectsConcurrentChange swaps the package-level
// readFileFn so the first read (the "before" snapshot used for the
// strings.Replace) returns the original content, but the second read
// (the dual-read guard right before WriteFile) returns mutated bytes.
// Execute MUST refuse to write.
func TestEdit_ExecuteDetectsConcurrentChange(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "e.go")
	writeFile(t, root, "e.go", "a\n")

	orig := readFileFn
	t.Cleanup(func() { readFileFn = orig })

	calls := 0
	readFileFn = func(name string) ([]byte, error) {
		calls++
		if calls == 1 {
			return []byte("a\n"), nil
		}
		return []byte("MUTATED\n"), nil
	}

	e := newEditTool(root)
	_, err := execTool(t, e, editArgs{Path: "e.go", Old: "a", New: "b"})
	if err == nil || !strings.Contains(err.Error(), "changed on disk") {
		t.Fatalf("expected TOCTOU error, got: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "a\n" {
		t.Fatalf("disk MUST be untouched on TOCTOU error; got %q", got)
	}
}

func TestEdit_PreviewableSatisfied(t *testing.T) {
	var _ tool.PreviewableTool = newEditTool(t.TempDir())
}
