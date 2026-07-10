package fs

import (
	"reflect"
	"strings"
	"testing"
)

func TestGlob_RecursiveMatch(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a/b/c.go", "")
	writeFile(t, root, "a/d.go", "")
	writeFile(t, root, "a/e.md", "")

	g := newGlobTool(root)
	res, err := execTool(t, g, globArgs{Pattern: "**/*.go"})
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	got := strings.Split(res.Content, "\n")
	want := []string{"a/b/c.go", "a/d.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("glob result mismatch:\n got %v\nwant %v", got, want)
	}
}

func TestGlob_NoMatch(t *testing.T) {
	root := t.TempDir()
	g := newGlobTool(root)
	res, err := execTool(t, g, globArgs{Pattern: "**/*.go"})
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if res.Content != "" {
		t.Fatalf("expected empty result, got %q", res.Content)
	}
	if res.IsError {
		t.Fatalf("expected IsError=false on empty match")
	}
}
