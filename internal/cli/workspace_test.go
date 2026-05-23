package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCanonicalWorkspaceUsesGitRoot(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	sub := filepath.Join(repo, "cmd", "ub")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := canonicalWorkspace(sub)
	if err != nil {
		t.Fatalf("canonicalWorkspace: %v", err)
	}
	if got != repo {
		t.Fatalf("canonicalWorkspace = %q, want git root %q", got, repo)
	}
}

func TestCanonicalWorkspaceIgnoresEmptyGitDirectory(t *testing.T) {
	temp := t.TempDir()
	repo := filepath.Join(temp, "repo")
	if err := os.MkdirAll(filepath.Join(temp, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := canonicalWorkspace(repo)
	if err != nil {
		t.Fatalf("canonicalWorkspace: %v", err)
	}
	if got != repo {
		t.Fatalf("canonicalWorkspace = %q, want repo path %q", got, repo)
	}
}

func TestCanonicalWorkspaceResolvesSymlinks(t *testing.T) {
	temp := t.TempDir()
	repo := filepath.Join(temp, "repo")
	link := filepath.Join(temp, "repo-link")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(repo, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	got, err := canonicalWorkspace(link)
	if err != nil {
		t.Fatalf("canonicalWorkspace: %v", err)
	}
	if got != repo {
		t.Fatalf("canonicalWorkspace = %q, want %q", got, repo)
	}
}
