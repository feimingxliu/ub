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

func TestEdit_LineRangeReplacementAvoidsExactOldWhitespace(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "tabs.go", "if ok {\n\treturn value\n}\n")
	e := newEditTool(root)
	res, err := execTool(t, e, editArgs{
		Path:      "tabs.go",
		StartLine: 2,
		New:       "\treturn other",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "tabs.go"))
	if string(got) != "if ok {\n\treturn other\n}\n" {
		t.Fatalf("content mismatch: %q", got)
	}
	if len(res.Files) != 1 || !strings.Contains(res.Files[0].UnifiedDiff, "+\treturn other") {
		t.Fatalf("result diff missing line replacement:\n%+v", res.Files)
	}
}

func TestEdit_LineRangeRequiresOldForMultiLineReplacement(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "target.txt", "one\ntwo\nthree\n")
	e := newEditTool(root)

	_, err := execTool(t, e, editArgs{
		Path:      "target.txt",
		StartLine: 2,
		New:       "TWO\ninserted",
	})
	if err == nil || !strings.Contains(err.Error(), "old is required for multi-line line edits") {
		t.Fatalf("expected line anchor requirement, got: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "target.txt"))
	if string(got) != "one\ntwo\nthree\n" {
		t.Fatalf("disk changed on missing anchor: %q", got)
	}
}

func TestEdit_LineRangeAllowsAnchoredMultiLineReplacement(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "target.txt", "one\ntwo\nthree\n")
	e := newEditTool(root)

	if _, err := execTool(t, e, editArgs{
		Path:      "target.txt",
		StartLine: 2,
		Old:       "two",
		New:       "TWO\ninserted",
	}); err != nil {
		t.Fatalf("execute: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "target.txt"))
	if string(got) != "one\nTWO\ninserted\nthree\n" {
		t.Fatalf("content mismatch: %q", got)
	}
}

func TestEdit_LineRangeOldAnchorsSelectedLines(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "anchor.go", "func before() {}\nfunc target() {}\n")
	e := newEditTool(root)

	if _, err := execTool(t, e, editArgs{
		Path:      "anchor.go",
		StartLine: 1,
		Old:       "func target() {}",
		New:       "func inserted() {}",
	}); err == nil || !strings.Contains(err.Error(), "line range old mismatch") {
		t.Fatalf("expected line anchor mismatch, got: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "anchor.go"))
	if string(got) != "func before() {}\nfunc target() {}\n" {
		t.Fatalf("disk changed on anchor mismatch: %q", got)
	}
}

func TestEdit_LineRangeOldAnchorAllowsOmittedTrailingNewline(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "anchor.txt", "one\ntwo\n")
	e := newEditTool(root)

	if _, err := execTool(t, e, editArgs{
		Path:      "anchor.txt",
		StartLine: 2,
		Old:       "two",
		New:       "TWO",
	}); err != nil {
		t.Fatalf("execute: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "anchor.txt"))
	if string(got) != "one\nTWO\n" {
		t.Fatalf("content mismatch: %q", got)
	}
}

func TestEdit_LineRangeReplacementPreservesCRLF(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "win.txt", "one\r\ntwo\r\nthree\r\n")
	e := newEditTool(root)
	if _, err := execTool(t, e, editArgs{Path: "win.txt", StartLine: 2, EndLine: 2, New: "TWO"}); err != nil {
		t.Fatalf("execute: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "win.txt"))
	if string(got) != "one\r\nTWO\r\nthree\r\n" {
		t.Fatalf("content mismatch: %q", got)
	}
}

func TestEdit_LineRangeCanDeleteLines(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "delete.txt", "one\ntwo\nthree\n")
	e := newEditTool(root)
	if _, err := execTool(t, e, editArgs{Path: "delete.txt", StartLine: 2, EndLine: 2}); err != nil {
		t.Fatalf("execute: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "delete.txt"))
	if string(got) != "one\nthree\n" {
		t.Fatalf("content mismatch: %q", got)
	}
}

func TestEdit_LineRangeValidation(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "lines.txt", "one\ntwo\n")
	e := newEditTool(root)
	cases := []struct {
		name string
		args editArgs
		want string
	}{
		{name: "end without start", args: editArgs{Path: "lines.txt", EndLine: 2, New: "x"}, want: "start_line is required"},
		{name: "reversed", args: editArgs{Path: "lines.txt", StartLine: 2, EndLine: 1, New: "x"}, want: "end_line must be greater"},
		{name: "outside", args: editArgs{Path: "lines.txt", StartLine: 3, New: "x"}, want: "outside file"},
		{name: "replace all", args: editArgs{Path: "lines.txt", StartLine: 1, New: "x", ReplaceAll: true}, want: "replace_all cannot be used"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := execTool(t, e, tc.args)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
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
	for _, name := range []string{"start_line", "end_line"} {
		prop := props[name].(map[string]any)
		if prop["type"] != "integer" {
			t.Fatalf("%s schema type = %#v, want integer\nschema=%s", name, prop["type"], raw)
		}
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

func TestEdit_OldNotFoundHintsExactWhitespace(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "tabs.go", "if ok {\n\treturn value\n}\n")
	e := newEditTool(root)
	_, err := execTool(t, e, editArgs{Path: "tabs.go", Old: "if ok {\n    return value\n}", New: "x"})
	if err == nil {
		t.Fatal("expected not-found error")
	}
	for _, want := range []string{
		"whitespace-normalized match exists",
		"tabs vs spaces",
		"re-read a narrow range",
		"retry apply_patch with context or edit/multiedit",
		"do not use bash/sed/python",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error missing %q:\n%v", want, err)
		}
	}
}

func TestEdit_DescriptionSteersAwayFromShellEdits(t *testing.T) {
	desc := newEditTool(t.TempDir()).Description()
	for _, want := range []string{"tabs", "line endings", "multi-line replacements", "re-read a narrow range", "Prefer this over bash/sed/python"} {
		if !strings.Contains(desc, want) {
			t.Fatalf("description missing %q:\n%s", want, desc)
		}
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
