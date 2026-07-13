package filehistory

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRmDeletedPathsParsesOnlySafeLiteralDeletes(t *testing.T) {
	got := rmDeletedPaths(`rm -f "docs/shell-completion.md" 'examples/bad-refactor/.gitignore' && echo done`)
	want := []string{"docs/shell-completion.md", "examples/bad-refactor/.gitignore"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("rmDeletedPaths = %#v, want %#v", got, want)
	}

	got = rmDeletedPaths(`git rm -- docs/shell-completion.md`)
	if len(got) != 1 || got[0] != "docs/shell-completion.md" {
		t.Fatalf("git rm paths = %#v, want docs/shell-completion.md", got)
	}

	for _, command := range []string{
		`rm $TARGET`,
		`rm *.md`,
		`cd docs && rm shell-completion.md`,
		`rm docs/${NAME}.md`,
	} {
		if got := rmDeletedPaths(command); len(got) != 0 {
			t.Fatalf("rmDeletedPaths(%q) = %#v, want no safe literal paths", command, got)
		}
	}
}

func TestToolTargetsApplyPatchTracksAddDeleteAndMove(t *testing.T) {
	patch := `*** Begin Patch
*** Add File: new.txt
+new
*** Update File: old.txt
*** Move to: moved.txt
@@
-old
+updated
*** Delete File: delete.txt
*** End Patch`
	raw, err := json.Marshal(map[string]string{"patch": patch})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	targets := toolTargets("apply_patch", raw)
	var paths []string
	for _, target := range targets {
		paths = append(paths, target.Path)
	}
	if strings.Join(paths, ",") != "new.txt,old.txt,moved.txt,delete.txt" {
		t.Fatalf("targets = %#v", paths)
	}

	invalid, _ := json.Marshal(map[string]string{"patch": "not a patch"})
	if got := toolTargets("apply_patch", invalid); len(got) != 0 {
		t.Fatalf("invalid patch targets = %#v", got)
	}
}
