package cli

import (
	"bytes"
	"context"
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
