package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/feimingxliu/ub/internal/store"
)

func TestSessionsListEmpty(t *testing.T) {
	temp := t.TempDir()
	workspace := filepath.Join(temp, "repo")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_DATA_HOME", filepath.Join(temp, "data"))
	t.Chdir(workspace)

	cmd := newRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"sessions", "ls"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("sessions ls: %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != "no sessions" {
		t.Fatalf("sessions ls output = %q, want no sessions", got)
	}
}

func TestSessionsDeleteRemovesSession(t *testing.T) {
	temp := t.TempDir()
	workspace := filepath.Join(temp, "repo")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_DATA_HOME", filepath.Join(temp, "data"))
	t.Chdir(workspace)

	path, err := store.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.UnixMilli(1_700_000_000_000).UTC()
	if err := st.CreateSession(context.Background(), store.Session{
		ID:        "current",
		Workspace: workspace,
		Title:     "Current Session",
		Model:     "fake/model",
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	cmd := newRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"sessions", "rm", "current"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("sessions rm: %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != "deleted current" {
		t.Fatalf("sessions rm output = %q, want deleted current", got)
	}

	st, err = store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.GetSession(context.Background(), "current"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetSession after delete err = %v, want ErrNotFound", err)
	}
}

func TestSessionsDeleteMissingSession(t *testing.T) {
	temp := t.TempDir()
	workspace := filepath.Join(temp, "repo")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_DATA_HOME", filepath.Join(temp, "data"))
	t.Chdir(workspace)

	cmd := newRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"sessions", "delete", "missing"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected missing session error")
	}
	if !strings.Contains(err.Error(), "session") || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing session error = %v", err)
	}
}

func TestSessionsClearRequiresYes(t *testing.T) {
	temp := t.TempDir()
	workspace := filepath.Join(temp, "repo")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_DATA_HOME", filepath.Join(temp, "data"))
	t.Chdir(workspace)

	cmd := newRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"sessions", "clear"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected confirmation error")
	}
	if !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("confirmation error = %v", err)
	}
}

func TestSessionsClearDeletesCurrentWorkspaceOnly(t *testing.T) {
	temp := t.TempDir()
	workspace := filepath.Join(temp, "repo")
	other := filepath.Join(temp, "other")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_DATA_HOME", filepath.Join(temp, "data"))
	t.Chdir(workspace)

	path, err := store.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.UnixMilli(1_700_000_000_000).UTC()
	for _, sess := range []store.Session{
		{ID: "current-1", Workspace: workspace, Title: "one", Model: "fake/model", CreatedAt: now, UpdatedAt: now},
		{ID: "current-2", Workspace: workspace, Title: "two", Model: "fake/model", CreatedAt: now, UpdatedAt: now.Add(time.Second)},
		{ID: "elsewhere", Workspace: other, Title: "other", Model: "fake/model", CreatedAt: now, UpdatedAt: now.Add(time.Hour)},
	} {
		if err := st.CreateSession(context.Background(), sess); err != nil {
			t.Fatalf("CreateSession(%s): %v", sess.ID, err)
		}
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	cmd := newRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"sessions", "clear", "--yes"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("sessions clear: %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != "deleted 2 sessions" {
		t.Fatalf("sessions clear output = %q, want deleted 2 sessions", got)
	}

	st, err = store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for _, id := range []string{"current-1", "current-2"} {
		if _, err := st.GetSession(context.Background(), id); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("GetSession(%s) err = %v, want ErrNotFound", id, err)
		}
	}
	if _, err := st.GetSession(context.Background(), "elsewhere"); err != nil {
		t.Fatalf("other workspace session should remain: %v", err)
	}
}

func TestSessionsListShowsCurrentWorkspaceOnly(t *testing.T) {
	temp := t.TempDir()
	workspace := filepath.Join(temp, "repo")
	other := filepath.Join(temp, "other")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_DATA_HOME", filepath.Join(temp, "data"))
	t.Chdir(workspace)

	path, err := store.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.UnixMilli(1_700_000_000_000).UTC()
	for _, sess := range []store.Session{
		{ID: "current", Workspace: workspace, Title: "Current Session", Model: "fake/model", CreatedAt: now, UpdatedAt: now},
		{ID: "elsewhere", Workspace: other, Title: "Other Session", Model: "other/model", CreatedAt: now, UpdatedAt: now.Add(time.Hour)},
	} {
		if err := st.CreateSession(context.Background(), sess); err != nil {
			t.Fatalf("CreateSession(%s): %v", sess.ID, err)
		}
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	cmd := newRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"sessions", "ls"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("sessions ls: %v", err)
	}
	got := out.String()
	for _, want := range []string{"ID", "UPDATED", "TITLE", "MODEL", "current", "Current Session", "fake/model"} {
		if !strings.Contains(got, want) {
			t.Fatalf("sessions ls output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "elsewhere") || strings.Contains(got, "Other Session") {
		t.Fatalf("sessions ls leaked other workspace:\n%s", got)
	}
}

func TestSessionsListUsesGitRootWorkspace(t *testing.T) {
	temp := t.TempDir()
	repo := filepath.Join(temp, "repo")
	subdir := filepath.Join(repo, "pkg")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeChatConfig(t, temp, `providers:
  fake:
    type: fake
`)

	path, err := store.DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	st, err := store.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	now := time.Now().UTC()
	if err := st.CreateSession(context.Background(), store.Session{
		ID:        "root-session",
		Workspace: repo,
		Title:     "Root Session",
		Model:     "fake/model",
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	t.Chdir(subdir)
	cmd := newRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"sessions", "ls"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("sessions ls: %v", err)
	}
	if !strings.Contains(out.String(), "root-session") {
		t.Fatalf("sessions ls missing root workspace session:\n%s", out.String())
	}
}
