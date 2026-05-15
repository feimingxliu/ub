package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefaultPathUsesXDGDataHome(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/tmp/ub-data")
	got, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	want := filepath.Join("/tmp/ub-data", "ub", "ub.db")
	if got != want {
		t.Fatalf("DefaultPath() = %q, want %q", got, want)
	}
}

func TestOpenCreatesParentAndAppliesMigrations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "nested", "ub.db")
	st := openTestStore(t, path)
	defer st.Close()

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("database file was not created: %v", err)
	}
	for _, name := range []string{"schema_version", "sessions", "events"} {
		if !sqliteObjectExists(t, st.db, "table", name) {
			t.Fatalf("missing table %s", name)
		}
	}
	for _, name := range []string{"idx_sessions_ws_updated", "idx_events_session"} {
		if !sqliteObjectExists(t, st.db, "index", name) {
			t.Fatalf("missing index %s", name)
		}
	}

	var foreignKeys int
	if err := st.db.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		t.Fatalf("query foreign_keys pragma: %v", err)
	}
	if foreignKeys != 1 {
		t.Fatalf("foreign_keys = %d, want 1", foreignKeys)
	}
	var journalMode string
	if err := st.db.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatalf("query journal_mode pragma: %v", err)
	}
	if strings.ToLower(journalMode) != "wal" {
		t.Fatalf("journal_mode = %q, want wal", journalMode)
	}
	var synchronous int
	if err := st.db.QueryRow("PRAGMA synchronous").Scan(&synchronous); err != nil {
		t.Fatalf("query synchronous pragma: %v", err)
	}
	if synchronous != 1 {
		t.Fatalf("synchronous = %d, want 1 (NORMAL)", synchronous)
	}
}

func TestMigrationIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ub.db")
	st := openTestStore(t, path)
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st = openTestStore(t, path)
	defer st.Close()

	var count int
	if err := st.db.QueryRow("SELECT COUNT(*) FROM schema_version WHERE version = 1").Scan(&count); err != nil {
		t.Fatalf("count schema_version: %v", err)
	}
	if count != 1 {
		t.Fatalf("migration version rows = %d, want 1", count)
	}
}

func TestSessionCRUD(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t, filepath.Join(t.TempDir(), "ub.db"))
	defer st.Close()

	created := time.UnixMilli(1_700_000_000_123).UTC()
	updated := created.Add(2 * time.Second)
	sess := Session{
		ID:        "s1",
		Workspace: "/repo",
		Title:     "first",
		Model:     "fake/model",
		Summary:   "summary",
		CreatedAt: created,
		UpdatedAt: updated,
	}
	if err := st.CreateSession(ctx, sess); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	got, err := st.GetSession(ctx, "s1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	assertSessionEqual(t, *got, sess)

	sess.Title = "renamed"
	sess.Model = "other/model"
	sess.UpdatedAt = updated.Add(time.Minute)
	if err := st.UpdateSession(ctx, sess); err != nil {
		t.Fatalf("UpdateSession: %v", err)
	}
	got, err = st.GetSession(ctx, "s1")
	if err != nil {
		t.Fatalf("GetSession after update: %v", err)
	}
	assertSessionEqual(t, *got, sess)

	if err := st.DeleteSession(ctx, "s1"); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if _, err := st.GetSession(ctx, "s1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetSession deleted err = %v, want ErrNotFound", err)
	}
	if err := st.UpdateSession(ctx, sess); !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdateSession missing err = %v, want ErrNotFound", err)
	}
	if err := st.DeleteSession(ctx, "s1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DeleteSession missing err = %v, want ErrNotFound", err)
	}
}

func TestListSessionsFiltersSortsAndLimits(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t, filepath.Join(t.TempDir(), "ub.db"))
	defer st.Close()

	base := time.UnixMilli(1_700_000_000_000).UTC()
	sessions := []Session{
		{ID: "old", Workspace: "/repo/a", Title: "old", CreatedAt: base, UpdatedAt: base},
		{ID: "new", Workspace: "/repo/a", Title: "new", CreatedAt: base, UpdatedAt: base.Add(2 * time.Hour)},
		{ID: "middle", Workspace: "/repo/a", Title: "middle", CreatedAt: base, UpdatedAt: base.Add(time.Hour)},
		{ID: "other", Workspace: "/repo/b", Title: "other", CreatedAt: base, UpdatedAt: base.Add(3 * time.Hour)},
	}
	for _, sess := range sessions {
		if err := st.CreateSession(ctx, sess); err != nil {
			t.Fatalf("CreateSession(%s): %v", sess.ID, err)
		}
	}

	got, err := st.ListSessions(ctx, "/repo/a", 2)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListSessions len = %d, want 2", len(got))
	}
	if got[0].ID != "new" || got[1].ID != "middle" {
		t.Fatalf("ListSessions order = [%s %s], want [new middle]", got[0].ID, got[1].ID)
	}
	for _, sess := range got {
		if sess.Workspace != "/repo/a" {
			t.Fatalf("returned other workspace: %+v", sess)
		}
	}
}

func openTestStore(t *testing.T, path string) *Store {
	t.Helper()
	st, err := Open(path)
	if err != nil {
		t.Fatalf("Open(%q): %v", path, err)
	}
	return st
}

func sqliteObjectExists(t *testing.T, db *sql.DB, typ, name string) bool {
	t.Helper()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type = ? AND name = ?", typ, name).Scan(&count); err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	return count > 0
}

func assertSessionEqual(t *testing.T, got, want Session) {
	t.Helper()
	if got.ID != want.ID ||
		got.Workspace != want.Workspace ||
		got.Title != want.Title ||
		got.Model != want.Model ||
		got.Summary != want.Summary ||
		!got.CreatedAt.Equal(want.CreatedAt) ||
		!got.UpdatedAt.Equal(want.UpdatedAt) {
		t.Fatalf("session mismatch\ngot:  %+v\nwant: %+v", got, want)
	}
}
