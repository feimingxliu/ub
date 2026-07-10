package filehistory

import (
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
