package command

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/feimingxliu/ub/internal/message"
	"github.com/feimingxliu/ub/internal/rollout"
	"github.com/feimingxliu/ub/internal/store"
)

func TestSessionsListEmpty(t *testing.T) {
	temp := t.TempDir()
	workspace := filepath.Join(temp, "repo")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	setSessionTestHomes(t, temp)
	t.Chdir(workspace)

	tc := newTestRootCommand("sessions", "ls")
	out := tc.out

	if err := tc.cmd.Execute(); err != nil {
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
	setSessionTestHomes(t, temp)
	t.Chdir(workspace)
	workspaceKey := mustCanonicalTestWorkspace(t, workspace)

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
		Workspace: workspaceKey,
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

	tc := newTestRootCommand("sessions", "rm", "current")
	out := tc.out

	if err := tc.cmd.Execute(); err != nil {
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
	setSessionTestHomes(t, temp)
	t.Chdir(workspace)

	tc := newTestRootCommand("sessions", "delete", "missing")
	err := tc.cmd.Execute()
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
	setSessionTestHomes(t, temp)
	t.Chdir(workspace)

	tc := newTestRootCommand("sessions", "clear")
	err := tc.cmd.Execute()
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
	setSessionTestHomes(t, temp)
	t.Chdir(workspace)
	workspaceKey := mustCanonicalTestWorkspace(t, workspace)
	otherKey := mustCanonicalTestWorkspace(t, other)

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
		{ID: "current-1", Workspace: workspaceKey, Title: "one", Model: "fake/model", CreatedAt: now, UpdatedAt: now},
		{ID: "current-2", Workspace: workspaceKey, Title: "two", Model: "fake/model", CreatedAt: now, UpdatedAt: now.Add(time.Second)},
		{ID: "elsewhere", Workspace: otherKey, Title: "other", Model: "fake/model", CreatedAt: now, UpdatedAt: now.Add(time.Hour)},
	} {
		if err := st.CreateSession(context.Background(), sess); err != nil {
			t.Fatalf("CreateSession(%s): %v", sess.ID, err)
		}
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	tc := newTestRootCommand("sessions", "clear", "--yes")
	out := tc.out

	if err := tc.cmd.Execute(); err != nil {
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

func TestSessionsClearAllDeletesEveryWorkspace(t *testing.T) {
	temp := t.TempDir()
	workspace := filepath.Join(temp, "repo")
	other := filepath.Join(temp, "other")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	setSessionTestHomes(t, temp)
	t.Chdir(workspace)
	workspaceKey := mustCanonicalTestWorkspace(t, workspace)
	otherKey := mustCanonicalTestWorkspace(t, other)

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
		{ID: "current-1", Workspace: workspaceKey, Title: "one", Model: "fake/model", CreatedAt: now, UpdatedAt: now},
		{ID: "current-2", Workspace: workspaceKey, Title: "two", Model: "fake/model", CreatedAt: now, UpdatedAt: now.Add(time.Second)},
		{ID: "elsewhere", Workspace: otherKey, Title: "other", Model: "fake/model", CreatedAt: now, UpdatedAt: now.Add(time.Hour)},
	} {
		if err := st.CreateSession(context.Background(), sess); err != nil {
			t.Fatalf("CreateSession(%s): %v", sess.ID, err)
		}
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	tc := newTestRootCommand("sessions", "clear", "--all", "--yes")
	out := tc.out

	if err := tc.cmd.Execute(); err != nil {
		t.Fatalf("sessions clear --all: %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != "deleted 3 sessions" {
		t.Fatalf("sessions clear --all output = %q, want deleted 3 sessions", got)
	}

	st, err = store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for _, id := range []string{"current-1", "current-2", "elsewhere"} {
		if _, err := st.GetSession(context.Background(), id); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("GetSession(%s) err = %v, want ErrNotFound", id, err)
		}
	}
}

func TestSessionsClearAllRequiresYes(t *testing.T) {
	temp := t.TempDir()
	workspace := filepath.Join(temp, "repo")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	setSessionTestHomes(t, temp)
	t.Chdir(workspace)

	tc := newTestRootCommand("sessions", "clear", "--all")
	err := tc.cmd.Execute()
	if err == nil {
		t.Fatal("expected confirmation error")
	}
	if !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("confirmation error = %v", err)
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
	setSessionTestHomes(t, temp)
	t.Chdir(workspace)
	workspaceKey := mustCanonicalTestWorkspace(t, workspace)
	otherKey := mustCanonicalTestWorkspace(t, other)

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
		{ID: "current", Workspace: workspaceKey, Title: "Current Session", Model: "fake/model", CreatedAt: now, UpdatedAt: now},
		{ID: "elsewhere", Workspace: otherKey, Title: "Other Session", Model: "other/model", CreatedAt: now, UpdatedAt: now.Add(time.Hour)},
	} {
		if err := st.CreateSession(context.Background(), sess); err != nil {
			t.Fatalf("CreateSession(%s): %v", sess.ID, err)
		}
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	tc := newTestRootCommand("sessions", "ls")
	out := tc.out

	if err := tc.cmd.Execute(); err != nil {
		t.Fatalf("sessions ls: %v", err)
	}
	got := out.String()
	for _, want := range []string{"ID", "WORKSPACE", "UPDATED", "TITLE", "MODEL", "current", "Current Session", "fake/model"} {
		if !strings.Contains(got, want) {
			t.Fatalf("sessions ls output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "elsewhere") || strings.Contains(got, "Other Session") {
		t.Fatalf("sessions ls leaked other workspace:\n%s", got)
	}
}

func TestSessionsListAllGroupsByWorkspace(t *testing.T) {
	temp := t.TempDir()
	workspace := filepath.Join(temp, "repo")
	other := filepath.Join(temp, "other")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	setSessionTestHomes(t, temp)
	t.Chdir(workspace)
	workspaceKey := mustCanonicalTestWorkspace(t, workspace)
	otherKey := mustCanonicalTestWorkspace(t, other)

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
		{ID: "current-old", Workspace: workspaceKey, Title: "Current Old", Model: "fake/old", CreatedAt: now, UpdatedAt: now},
		{ID: "current-new", Workspace: workspaceKey, Title: "Current New", Model: "fake/new", CreatedAt: now, UpdatedAt: now.Add(time.Hour)},
		{ID: "elsewhere", Workspace: otherKey, Title: "Other Session", Model: "other/model", CreatedAt: now, UpdatedAt: now.Add(2 * time.Hour)},
	} {
		if err := st.CreateSession(context.Background(), sess); err != nil {
			t.Fatalf("CreateSession(%s): %v", sess.ID, err)
		}
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	tc := newTestRootCommand("sessions", "ls", "--all")
	out := tc.out

	if err := tc.cmd.Execute(); err != nil {
		t.Fatalf("sessions ls --all: %v", err)
	}
	got := out.String()
	for _, want := range []string{
		"WORKSPACE " + workspaceKey,
		"WORKSPACE " + otherKey,
		"current-new",
		"current-old",
		"elsewhere",
		"Other Session",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("sessions ls --all output missing %q:\n%s", want, got)
		}
	}
	if strings.Index(got, "current-new") > strings.Index(got, "current-old") {
		t.Fatalf("workspace sessions are not ordered newest first:\n%s", got)
	}
}

func TestSessionsSearchFindsRolloutTextAcrossWorkspaces(t *testing.T) {
	temp := t.TempDir()
	workspace := filepath.Join(temp, "repo")
	other := filepath.Join(temp, "other")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	setSessionTestHomes(t, temp)
	t.Chdir(workspace)
	workspaceKey := mustCanonicalTestWorkspace(t, workspace)
	otherKey := mustCanonicalTestWorkspace(t, other)

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
		{ID: "current", Workspace: workspaceKey, Title: "Current Session", Model: "fake/model", CreatedAt: now, UpdatedAt: now},
		{ID: "elsewhere", Workspace: otherKey, Title: "Other Session", Model: "fake/model", CreatedAt: now, UpdatedAt: now.Add(time.Hour)},
	} {
		if err := st.CreateSession(context.Background(), sess); err != nil {
			t.Fatalf("CreateSession(%s): %v", sess.ID, err)
		}
	}
	ro, err := rollout.New(st)
	if err != nil {
		t.Fatalf("rollout.New: %v", err)
	}
	currentEvent, err := rollout.UserMessage("current", 1, message.Text(message.RoleUser, "please find the needle here"))
	if err != nil {
		t.Fatalf("UserMessage current: %v", err)
	}
	pruneCheckpoint, err := rollout.SummaryWithMessagesAndMaintenance(
		"current",
		1,
		"Context maintenance pruned 1 superseded tool result.",
		[]message.Message{message.Text(message.RoleUser, "please find the needle here")},
		0,
		1,
		20,
		&rollout.ContextMaintenance{Decision: "prune"},
	)
	if err != nil {
		t.Fatalf("SummaryWithMessagesAndMaintenance: %v", err)
	}
	otherEvent, err := rollout.AssistantMessage("elsewhere", 2, message.Text(message.RoleAssistant, "the other Needle is here too"))
	if err != nil {
		t.Fatalf("AssistantMessage elsewhere: %v", err)
	}
	for _, event := range []rollout.Event{currentEvent, pruneCheckpoint, otherEvent} {
		if err := ro.Append(context.Background(), event); err != nil {
			t.Fatalf("Append(%s): %v", event.SessionID, err)
		}
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	tc := newTestRootCommand("sessions", "search", "needle")
	out := tc.out

	if err := tc.cmd.Execute(); err != nil {
		t.Fatalf("sessions search: %v", err)
	}
	got := out.String()
	for _, want := range []string{
		"WORKSPACE",
		workspaceKey,
		otherKey,
		"current",
		"elsewhere",
		"please find the needle here",
		"the other Needle is here too",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("sessions search output missing %q:\n%s", want, got)
		}
	}
	if count := strings.Count(got, "please find the needle here"); count != 1 {
		t.Fatalf("prune checkpoint duplicated session search match %d times:\n%s", count, got)
	}
}

func TestSessionsSearchNoMatches(t *testing.T) {
	temp := t.TempDir()
	workspace := filepath.Join(temp, "repo")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	setSessionTestHomes(t, temp)
	t.Chdir(workspace)

	tc := newTestRootCommand("sessions", "search", "missing")
	out := tc.out

	if err := tc.cmd.Execute(); err != nil {
		t.Fatalf("sessions search: %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != "no matches" {
		t.Fatalf("sessions search output = %q, want no matches", got)
	}
}

func TestSessionsListUsesGitRootWorkspace(t *testing.T) {
	temp := t.TempDir()
	repo := filepath.Join(temp, "repo")
	subdir := filepath.Join(repo, "pkg")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
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
	repoKey := mustCanonicalTestWorkspace(t, repo)
	if err := st.CreateSession(context.Background(), store.Session{
		ID:        "root-session",
		Workspace: repoKey,
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
	tc := newTestRootCommand("sessions", "ls")
	out := tc.out

	if err := tc.cmd.Execute(); err != nil {
		t.Fatalf("sessions ls: %v", err)
	}
	if !strings.Contains(out.String(), "root-session") {
		t.Fatalf("sessions ls missing root workspace session:\n%s", out.String())
	}
}

func setSessionTestHomes(t *testing.T, temp string) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(temp, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(temp, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(temp, "state"))
}
