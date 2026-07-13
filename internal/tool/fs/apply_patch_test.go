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

func patchArgs(patch string) applyPatchArgs {
	return applyPatchArgs{Patch: patch}
}

func TestApplyPatch_PreviewAndExecuteContextUpdate(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.go", "func run() {\n\treturn old\n}\n")
	patch := `*** Begin Patch
*** Update File: a.go
@@ func run() {
 func run() {
-	return old
+	return new
 }
*** End Patch`

	patchTool := newApplyPatchTool(root)
	preview := previewTool(t, patchTool, patchArgs(patch))
	if len(preview.Files) != 1 || preview.Files[0].Kind != tool.KindModify {
		t.Fatalf("preview files = %+v", preview.Files)
	}
	if !strings.Contains(preview.Files[0].UnifiedDiff, "-\treturn old") || !strings.Contains(preview.Files[0].UnifiedDiff, "+\treturn new") {
		t.Fatalf("preview diff =\n%s", preview.Files[0].UnifiedDiff)
	}
	before, _ := os.ReadFile(filepath.Join(root, "a.go"))
	if string(before) != "func run() {\n\treturn old\n}\n" {
		t.Fatalf("preview changed disk: %q", before)
	}

	result, err := execTool(t, patchTool, patchArgs(patch))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	after, _ := os.ReadFile(filepath.Join(root, "a.go"))
	if string(after) != "func run() {\n\treturn new\n}\n" {
		t.Fatalf("after = %q", after)
	}
	if len(result.Files) != 1 || result.Files[0].Path != "a.go" || result.Files[0].Kind != tool.KindModify {
		t.Fatalf("result files = %+v", result.Files)
	}
}

func TestApplyPatch_PreviewBindingRejectsExternalChange(t *testing.T) {
	root := t.TempDir()
	path := writeFile(t, root, "a.txt", "old\n")
	patch := `*** Begin Patch
*** Update File: a.txt
@@
-old
+new
*** End Patch`
	raw, err := json.Marshal(patchArgs(patch))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	patchTool := newApplyPatchTool(root)
	ctx := tool.WithToolUseID(context.Background(), "call-preview")
	if _, err := patchTool.Preview(ctx, raw); err != nil {
		t.Fatalf("preview: %v", err)
	}
	if err := os.WriteFile(path, []byte("prefix\nold\n"), 0o644); err != nil {
		t.Fatalf("external write: %v", err)
	}
	_, err = patchTool.Execute(ctx, raw)
	if err == nil || !strings.Contains(err.Error(), "changed on disk since preview") {
		t.Fatalf("expected preview binding error, got: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "prefix\nold\n" {
		t.Fatalf("execute overwrote external change: %q", got)
	}
}

func TestApplyPatch_RejectsAmbiguousHunk(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.txt", "start\nvalue\nend\nstart\nvalue\nend\n")
	patch := `*** Begin Patch
*** Update File: a.txt
@@
 start
-value
+changed
 end
*** End Patch`

	_, err := execTool(t, newApplyPatchTool(root), patchArgs(patch))
	if err == nil || !strings.Contains(err.Error(), "multiple matches") || !strings.Contains(err.Error(), "add more context") {
		t.Fatalf("expected ambiguity error, got: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	if string(got) != "start\nvalue\nend\nstart\nvalue\nend\n" {
		t.Fatalf("ambiguous patch changed disk: %q", got)
	}
}

func TestApplyPatch_AddUpdateDeleteAtomically(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "edit.txt", "before\n")
	writeFile(t, root, "delete.txt", "remove\n")
	patch := `*** Begin Patch
*** Add File: nested/new.txt
+created
*** Update File: edit.txt
@@
-before
+after
*** Delete File: delete.txt
*** End Patch`

	result, err := execTool(t, newApplyPatchTool(root), patchArgs(patch))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	newContent, _ := os.ReadFile(filepath.Join(root, "nested", "new.txt"))
	edited, _ := os.ReadFile(filepath.Join(root, "edit.txt"))
	if string(newContent) != "created\n" || string(edited) != "after\n" {
		t.Fatalf("new=%q edit=%q", newContent, edited)
	}
	if _, err := os.Stat(filepath.Join(root, "delete.txt")); !os.IsNotExist(err) {
		t.Fatalf("delete.txt stat = %v, want not exist", err)
	}
	if len(result.Files) != 3 {
		t.Fatalf("result files = %+v", result.Files)
	}
	for index, want := range []string{"delete.txt", "edit.txt", "nested/new.txt"} {
		if result.Files[index].Path != want {
			t.Fatalf("result files not sorted: %+v", result.Files)
		}
	}
}

func TestApplyPatch_FailedLaterHunkDoesNotWrite(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.txt", "one\n")
	writeFile(t, root, "b.txt", "two\n")
	patch := `*** Begin Patch
*** Update File: a.txt
@@
-one
+ONE
*** Update File: b.txt
@@
-missing
+TWO
*** End Patch`

	_, err := execTool(t, newApplyPatchTool(root), patchArgs(patch))
	if err == nil || !strings.Contains(err.Error(), "did not match") {
		t.Fatalf("expected hunk failure, got: %v", err)
	}
	a, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	b, _ := os.ReadFile(filepath.Join(root, "b.txt"))
	if string(a) != "one\n" || string(b) != "two\n" {
		t.Fatalf("partial write a=%q b=%q", a, b)
	}
}

func TestApplyPatch_WriteFailureRollsBackEarlierChanges(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "b.txt", "before\n")
	patch := `*** Begin Patch
*** Add File: a.txt
+created
*** Update File: b.txt
@@
-before
+after
*** End Patch`

	original := patchWriteFileFn
	t.Cleanup(func() { patchWriteFileFn = original })
	calls := 0
	patchWriteFileFn = func(file *os.File, content []byte) error {
		calls++
		if calls == 2 {
			return os.ErrPermission
		}
		_, err := file.Write(content)
		return err
	}

	_, err := execTool(t, newApplyPatchTool(root), patchArgs(patch))
	if err == nil || !strings.Contains(err.Error(), "permission") {
		t.Fatalf("expected write error, got: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "a.txt")); !os.IsNotExist(err) {
		t.Fatalf("created file survived failed transaction: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "b.txt"))
	if string(got) != "before\n" {
		t.Fatalf("b.txt not restored: %q", got)
	}
}

func TestApplyPatch_PartialWriteKeepsOriginalFileIntact(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.txt", "before\n")
	patch := `*** Begin Patch
*** Update File: a.txt
@@
-before
+after
*** End Patch`

	original := patchWriteFileFn
	t.Cleanup(func() { patchWriteFileFn = original })
	calls := 0
	patchWriteFileFn = func(file *os.File, content []byte) error {
		calls++
		if calls == 1 {
			if _, err := file.Write([]byte("partial")); err != nil {
				return err
			}
			return os.ErrPermission
		}
		_, err := file.Write(content)
		return err
	}

	_, err := execTool(t, newApplyPatchTool(root), patchArgs(patch))
	if err == nil || !strings.Contains(err.Error(), "permission") {
		t.Fatalf("expected write failure, got: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	if string(got) != "before\n" {
		t.Fatalf("partial write corrupted original: %q", got)
	}
}

func TestApplyPatch_MoveAndPreservesCRLF(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "old/name.txt", "first\r\nsecond\r\n")
	patch := `*** Begin Patch
*** Update File: old/name.txt
*** Move to: new/name.txt
@@
 first
-second
+changed
*** End Patch`

	result, err := execTool(t, newApplyPatchTool(root), patchArgs(patch))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "old", "name.txt")); !os.IsNotExist(err) {
		t.Fatalf("source stat = %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "new", "name.txt"))
	if string(got) != "first\r\nchanged\r\n" {
		t.Fatalf("moved content = %q", got)
	}
	if len(result.Files) != 1 || result.Files[0].Path != "new/name.txt" || result.Files[0].Kind != tool.KindModify {
		t.Fatalf("result files = %+v", result.Files)
	}
}

func TestApplyPatch_MovePreservesMode(t *testing.T) {
	root := t.TempDir()
	source := writeFile(t, root, "old.txt", "old\n")
	if err := os.Chmod(source, 0o666); err != nil {
		t.Fatalf("chmod source: %v", err)
	}
	patch := `*** Begin Patch
*** Update File: old.txt
*** Move to: new.txt
@@
-old
+new
*** End Patch`
	if _, err := execTool(t, newApplyPatchTool(root), patchArgs(patch)); err != nil {
		t.Fatalf("execute: %v", err)
	}
	info, err := os.Stat(filepath.Join(root, "new.txt"))
	if err != nil {
		t.Fatalf("stat target: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o666 {
		t.Fatalf("target mode = %o, want 666", got)
	}
}

func TestApplyPatch_RejectsExternalSymlink(t *testing.T) {
	root := t.TempDir()
	externalRoot := t.TempDir()
	external := writeFile(t, externalRoot, "outside.txt", "old\n")
	link := filepath.Join(root, "escape")
	if err := os.Symlink(externalRoot, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	patch := `*** Begin Patch
*** Update File: escape/outside.txt
@@
-old
+new
*** End Patch`
	_, err := execTool(t, newApplyPatchTool(root), patchArgs(patch))
	if err == nil || !strings.Contains(err.Error(), "outside workspace root") {
		t.Fatalf("expected sandbox error, got: %v", err)
	}
	got, _ := os.ReadFile(external)
	if string(got) != "old\n" {
		t.Fatalf("external file changed: %q", got)
	}
}

func TestApplyPatch_EOFInsertionAndInvalidSyntax(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.txt", "one\n")
	valid := `*** Begin Patch
*** Update File: a.txt
@@
+two
*** End of File
*** End Patch`
	if _, err := execTool(t, newApplyPatchTool(root), patchArgs(valid)); err != nil {
		t.Fatalf("EOF insertion: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	if string(got) != "one\ntwo\n" {
		t.Fatalf("EOF insertion content = %q", got)
	}

	invalid := `*** Begin Patch
*** Update File: a.txt
@@
 not a change
*** End Patch`
	_, err := execTool(t, newApplyPatchTool(root), patchArgs(invalid))
	if err == nil || !strings.Contains(err.Error(), "makes no change") {
		t.Fatalf("invalid syntax error = %v", err)
	}
}

func TestApplyPatch_TOCTOUAndPatchPaths(t *testing.T) {
	root := t.TempDir()
	path := writeFile(t, root, "a.txt", "old\n")
	patch := `*** Begin Patch
*** Add File: new.txt
+new
*** Update File: a.txt
*** Move to: moved.txt
@@
-old
+new
*** Delete File: deleted.txt
*** End Patch`
	paths, err := PatchPaths(patch)
	if err != nil {
		t.Fatalf("PatchPaths: %v", err)
	}
	if strings.Join(paths, ",") != "new.txt,a.txt,moved.txt,deleted.txt" {
		t.Fatalf("paths = %v", paths)
	}

	writeFile(t, root, "deleted.txt", "gone\n")
	original := patchReadFileFn
	t.Cleanup(func() { patchReadFileFn = original })
	calls := 0
	patchReadFileFn = func(workspace *os.Root, name string) ([]byte, error) {
		if name == filepath.Base(path) {
			calls++
			if calls == 2 {
				return []byte("changed\n"), nil
			}
		}
		return workspace.ReadFile(name)
	}
	raw, err := json.Marshal(patchArgs(patch))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	_, err = newApplyPatchTool(root).Execute(context.Background(), raw)
	if err == nil || !strings.Contains(err.Error(), "changed on disk") {
		t.Fatalf("TOCTOU error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("new target created after failed TOCTOU: %v", err)
	}
}

func TestApplyPatch_NotifierAndPreviewable(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.txt", "old\n")
	notifier := &recordingNotifier{}
	patch := `*** Begin Patch
*** Update File: a.txt
@@
-old
+new
*** End Patch`
	if _, err := execTool(t, newApplyPatchToolWithNotifier(root, notifier), patchArgs(patch)); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(notifier.paths) != 1 || notifier.paths[0] != filepath.Join(root, "a.txt") {
		t.Fatalf("notifier paths = %v", notifier.paths)
	}
	var _ tool.PreviewableTool = newApplyPatchTool(root)
}
