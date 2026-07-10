package fs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLs_BasicListing(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.txt", "x")
	writeFile(t, root, "b.txt", "y")
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	ls := newLsTool(root)
	res, err := execTool(t, ls, lsArgs{Path: "."})
	if err != nil {
		t.Fatalf("ls: %v", err)
	}
	want := "file\ta.txt\nfile\tb.txt\ndir\tsub"
	if res.Content != want {
		t.Fatalf("ls content mismatch:\n got %q\nwant %q", res.Content, want)
	}
}

func TestLs_PathIsFile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.txt", "x")
	ls := newLsTool(root)
	_, err := execTool(t, ls, lsArgs{Path: "a.txt"})
	if err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("expected 'not a directory' error, got: %v", err)
	}
}

func TestLs_OutsideRoot(t *testing.T) {
	root := t.TempDir()
	ls := newLsTool(root)
	if _, err := execTool(t, ls, lsArgs{Path: "../"}); err == nil {
		t.Fatalf("expected sandbox error")
	}
}
