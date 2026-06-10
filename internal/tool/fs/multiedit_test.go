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

func TestMultiEdit_SingleFileTwoEdits(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.go", "foo\nbar\nbaz\n")

	me := newMultiEditTool(root)
	args := multiEditArgs{Edits: []editArgs{
		{Path: "a.go", Old: "foo", New: "FOO"},
		{Path: "a.go", Old: "bar", New: "BAR"},
	}}
	res, err := execTool(t, me, args)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "a.go"))
	if string(got) != "FOO\nBAR\nbaz\n" {
		t.Fatalf("file content = %q", got)
	}
	if len(res.Files) != 1 {
		t.Fatalf("Result.Files len = %d, want 1", len(res.Files))
	}
	if res.Files[0].Path != "a.go" || res.Files[0].Kind != tool.KindModify {
		t.Fatalf("Result.Files[0] = %+v", res.Files[0])
	}
	if !strings.Contains(res.Files[0].UnifiedDiff, "+FOO") ||
		!strings.Contains(res.Files[0].UnifiedDiff, "+BAR") {
		t.Fatalf("diff missing +FOO/+BAR:\n%s", res.Files[0].UnifiedDiff)
	}
	if !strings.Contains(res.Content, "1 file") || !strings.Contains(res.Content, "2 replacement") {
		t.Fatalf("Content = %q", res.Content)
	}
}

func TestMultiEdit_MultipleFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.txt", "aaa\n")
	writeFile(t, root, "b.txt", "bbb\n")

	me := newMultiEditTool(root)
	// Pass b.txt first to verify output is sorted by path regardless of input
	// order.
	args := multiEditArgs{Edits: []editArgs{
		{Path: "b.txt", Old: "bbb", New: "BBB"},
		{Path: "a.txt", Old: "aaa", New: "AAA"},
	}}
	res, err := execTool(t, me, args)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	a, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	b, _ := os.ReadFile(filepath.Join(root, "b.txt"))
	if string(a) != "AAA\n" || string(b) != "BBB\n" {
		t.Fatalf("a=%q b=%q", a, b)
	}
	if len(res.Files) != 2 {
		t.Fatalf("Result.Files len = %d, want 2", len(res.Files))
	}
	if res.Files[0].Path != "a.txt" || res.Files[1].Path != "b.txt" {
		t.Fatalf("paths not sorted: %+v", res.Files)
	}
}

func TestMultiEdit_OrderDependent(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.txt", "foo\n")

	me := newMultiEditTool(root)
	args := multiEditArgs{Edits: []editArgs{
		{Path: "a.txt", Old: "foo", New: "bar"},
		{Path: "a.txt", Old: "bar", New: "baz"},
	}}
	if _, err := execTool(t, me, args); err != nil {
		t.Fatalf("execute: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	if string(got) != "baz\n" {
		t.Fatalf("ordered apply broken: %q", got)
	}
}

func TestMultiEdit_LineRangeStep(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.txt", "one\n\ttwo\nthree\n")

	me := newMultiEditTool(root)
	args := multiEditArgs{Edits: []editArgs{
		{Path: "a.txt", StartLine: 2, New: "\tTWO"},
		{Path: "a.txt", Old: "three", New: "THREE"},
	}}
	if _, err := execTool(t, me, args); err != nil {
		t.Fatalf("execute: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	if string(got) != "one\n\tTWO\nTHREE\n" {
		t.Fatalf("file content = %q", got)
	}
}

func TestMultiEdit_LineRangeRequiresOldForMultiLineStep(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.txt", "one\ntwo\nthree\n")

	me := newMultiEditTool(root)
	args := multiEditArgs{Edits: []editArgs{
		{Path: "a.txt", StartLine: 2, New: "TWO\ninserted"},
	}}
	_, err := execTool(t, me, args)
	if err == nil || !strings.Contains(err.Error(), "old is required for multi-line line edits") {
		t.Fatalf("expected line anchor requirement, got: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	if string(got) != "one\ntwo\nthree\n" {
		t.Fatalf("disk changed on missing anchor: %q", got)
	}
}

func TestMultiEdit_LineRangeOldMismatchAbortsBatch(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.txt", "foo\n")
	writeFile(t, root, "b.txt", "one\ntwo\n")

	me := newMultiEditTool(root)
	args := multiEditArgs{Edits: []editArgs{
		{Path: "a.txt", Old: "foo", New: "FOO"},
		{Path: "b.txt", StartLine: 1, Old: "two", New: "TWO"},
	}}
	_, err := execTool(t, me, args)
	if err == nil || !strings.Contains(err.Error(), "line range old mismatch") {
		t.Fatalf("expected line anchor mismatch, got: %v", err)
	}
	a, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	b, _ := os.ReadFile(filepath.Join(root, "b.txt"))
	if string(a) != "foo\n" || string(b) != "one\ntwo\n" {
		t.Fatalf("partial write happened: a=%q b=%q", a, b)
	}
}

func TestMultiEdit_GoSyntaxGuardRejectsBrokenResult(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", "package main\n\nfunc main() {\n}\n")

	me := newMultiEditTool(root)
	args := multiEditArgs{Edits: []editArgs{{
		Path: "main.go",
		Old:  "func main() {\n}",
		New:  "func main() {\nfunc misplaced() {}\n}",
	}}}
	_, err := execTool(t, me, args)
	if err == nil || !strings.Contains(err.Error(), "Go syntax guard rejected") {
		t.Fatalf("expected Go syntax guard error, got: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "main.go"))
	if string(got) != "package main\n\nfunc main() {\n}\n" {
		t.Fatalf("disk changed on syntax guard: %q", got)
	}
}

func TestMultiEdit_PreviewDoesNotMutate(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.txt", "foo\n")

	me := newMultiEditTool(root)
	args := multiEditArgs{Edits: []editArgs{{Path: "a.txt", Old: "foo", New: "bar"}}}
	raw, _ := json.Marshal(args)
	p, err := me.Preview(context.Background(), raw)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if len(p.Files) != 1 || !strings.Contains(p.Files[0].UnifiedDiff, "+bar") {
		t.Fatalf("preview = %+v", p)
	}
	got, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	if string(got) != "foo\n" {
		t.Fatalf("preview mutated disk: %q", got)
	}
}

func TestMultiEdit_AcceptsJSONEncodedEditsString(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.txt", "foo\nbar\n")

	me := newMultiEditTool(root)
	raw := json.RawMessage(`{
		"edits": "[{\"path\":\"a.txt\",\"old\":\"foo\",\"new\":\"FOO\"},{\"path\":\"a.txt\",\"old\":\"bar\",\"new\":\"BAR\"}]"
	}`)
	if _, err := me.Execute(context.Background(), raw); err != nil {
		t.Fatalf("execute: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	if string(got) != "FOO\nBAR\n" {
		t.Fatalf("file content = %q", got)
	}
}

func TestMultiEdit_EmptyEdits(t *testing.T) {
	root := t.TempDir()
	me := newMultiEditTool(root)
	_, err := execTool(t, me, multiEditArgs{Edits: []editArgs{}})
	if err == nil || !strings.Contains(err.Error(), "at least one edit") {
		t.Fatalf("expected empty-edits error, got: %v", err)
	}
}

func TestMultiEdit_MissingPath(t *testing.T) {
	root := t.TempDir()
	me := newMultiEditTool(root)
	_, err := execTool(t, me, multiEditArgs{Edits: []editArgs{{Old: "x", New: "y"}}})
	if err == nil || !strings.Contains(err.Error(), "path is required") {
		t.Fatalf("expected path-required error, got: %v", err)
	}
}

func TestMultiEdit_MissingOld(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.txt", "x")
	me := newMultiEditTool(root)
	_, err := execTool(t, me, multiEditArgs{Edits: []editArgs{{Path: "a.txt", New: "y"}}})
	if err == nil || !strings.Contains(err.Error(), "old is required") {
		t.Fatalf("expected old-required error, got: %v", err)
	}
}

func TestMultiEdit_MultiMatchWithoutReplaceAll(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.txt", "x\nx\n")
	me := newMultiEditTool(root)
	_, err := execTool(t, me, multiEditArgs{Edits: []editArgs{{Path: "a.txt", Old: "x", New: "y"}}})
	if err == nil || !strings.Contains(err.Error(), "replace_all=true") {
		t.Fatalf("expected replace_all hint, got: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	if string(got) != "x\nx\n" {
		t.Fatalf("disk changed on error: %q", got)
	}
}

func TestMultiEdit_DescriptionSteersAwayFromShellEdits(t *testing.T) {
	desc := newMultiEditTool(t.TempDir()).Description()
	for _, want := range []string{"tabs", "line endings", "old anchors", "re-read a narrow range", "instead of bash/sed/python"} {
		if !strings.Contains(desc, want) {
			t.Fatalf("description missing %q:\n%s", want, desc)
		}
	}
}

func TestMultiEdit_OneEditFailsBatchAborted(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.txt", "foo\n")
	writeFile(t, root, "b.txt", "bbb\n")
	me := newMultiEditTool(root)
	args := multiEditArgs{Edits: []editArgs{
		{Path: "a.txt", Old: "foo", New: "FOO"},
		{Path: "b.txt", Old: "NOPE", New: "X"},
	}}
	_, err := execTool(t, me, args)
	if err == nil || !strings.Contains(err.Error(), "old string not found") {
		t.Fatalf("expected batch failure, got: %v", err)
	}
	a, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	b, _ := os.ReadFile(filepath.Join(root, "b.txt"))
	if string(a) != "foo\n" || string(b) != "bbb\n" {
		t.Fatalf("partial write happened: a=%q b=%q", a, b)
	}
}

func TestMultiEdit_PathOutsideRoot(t *testing.T) {
	root := t.TempDir()
	me := newMultiEditTool(root)
	_, err := execTool(t, me, multiEditArgs{Edits: []editArgs{{Path: "../escape", Old: "x", New: "y"}}})
	if err == nil {
		t.Fatalf("expected sandbox error")
	}
}

// TestMultiEdit_TOCTOU exercises the dual-read guard: the per-file before
// snapshot taken during plan() matches, but the re-read in Execute returns
// mutated bytes for the *second* file. The batch MUST abort and the first
// file MUST be left untouched (no partial writes).
func TestMultiEdit_TOCTOU(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.txt", "foo\n")
	writeFile(t, root, "b.txt", "bbb\n")

	// During plan() the read counts go: a=1, b=2.
	// During Execute() re-read loop: a=3, b=4.
	// We mutate b's second read to trigger the TOCTOU error after a was
	// already verified.
	orig := readFileFn
	t.Cleanup(func() { readFileFn = orig })
	calls := map[string]int{}
	readFileFn = func(name string) ([]byte, error) {
		calls[name]++
		if filepath.Base(name) == "b.txt" && calls[name] == 2 {
			return []byte("MUTATED\n"), nil
		}
		return os.ReadFile(name)
	}

	me := newMultiEditTool(root)
	args := multiEditArgs{Edits: []editArgs{
		{Path: "a.txt", Old: "foo", New: "FOO"},
		{Path: "b.txt", Old: "bbb", New: "BBB"},
	}}
	_, err := execTool(t, me, args)
	if err == nil || !strings.Contains(err.Error(), "changed on disk") {
		t.Fatalf("expected TOCTOU error, got: %v", err)
	}
	a, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	b, _ := os.ReadFile(filepath.Join(root, "b.txt"))
	if string(a) != "foo\n" {
		t.Fatalf("a.txt must be untouched on TOCTOU; got %q", a)
	}
	if string(b) != "bbb\n" {
		t.Fatalf("b.txt must be untouched on TOCTOU; got %q", b)
	}
}

func TestMultiEdit_PreviewableSatisfied(t *testing.T) {
	var _ tool.PreviewableTool = newMultiEditTool(t.TempDir())
}
