package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"reflect"
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

func TestListAllSessionsOrdersByWorkspaceThenUpdated(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t, filepath.Join(t.TempDir(), "ub.db"))
	defer st.Close()

	base := time.UnixMilli(1_700_000_000_000).UTC()
	sessions := []Session{
		{ID: "b-old", Workspace: "/repo/b", Title: "old", CreatedAt: base, UpdatedAt: base},
		{ID: "a-new", Workspace: "/repo/a", Title: "new", CreatedAt: base, UpdatedAt: base.Add(2 * time.Hour)},
		{ID: "a-old", Workspace: "/repo/a", Title: "old", CreatedAt: base, UpdatedAt: base.Add(time.Hour)},
		{ID: "b-new", Workspace: "/repo/b", Title: "new", CreatedAt: base, UpdatedAt: base.Add(3 * time.Hour)},
	}
	for _, sess := range sessions {
		if err := st.CreateSession(ctx, sess); err != nil {
			t.Fatalf("CreateSession(%s): %v", sess.ID, err)
		}
	}

	got, err := st.ListAllSessions(ctx)
	if err != nil {
		t.Fatalf("ListAllSessions: %v", err)
	}
	var ids []string
	for _, sess := range got {
		ids = append(ids, sess.ID)
	}
	want := []string{"a-new", "a-old", "b-new", "b-old"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("ListAllSessions ids = %#v, want %#v", ids, want)
	}
}

func TestDeleteAllSessionsRemovesEveryWorkspace(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t, filepath.Join(t.TempDir(), "ub.db"))
	defer st.Close()

	base := time.UnixMilli(1_700_000_000_000).UTC()
	for _, sess := range []Session{
		{ID: "a1", Workspace: "/repo/a", Title: "a1", CreatedAt: base, UpdatedAt: base},
		{ID: "a2", Workspace: "/repo/a", Title: "a2", CreatedAt: base, UpdatedAt: base},
		{ID: "b1", Workspace: "/repo/b", Title: "b1", CreatedAt: base, UpdatedAt: base},
	} {
		if err := st.CreateSession(ctx, sess); err != nil {
			t.Fatalf("CreateSession(%s): %v", sess.ID, err)
		}
		if _, err := st.db.ExecContext(ctx, `INSERT INTO events
			(id, session_id, turn, time, type, payload)
			VALUES (?, ?, 1, ?, 'user_message', ?)`,
			"event-"+sess.ID, sess.ID, base.UnixMilli(), []byte(`{"text":"hi"}`)); err != nil {
			t.Fatalf("insert event for %s: %v", sess.ID, err)
		}
	}

	deleted, err := st.DeleteAllSessions(ctx)
	if err != nil {
		t.Fatalf("DeleteAllSessions: %v", err)
	}
	if deleted != 3 {
		t.Fatalf("deleted = %d, want 3", deleted)
	}

	var remainingEvents int
	if err := st.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM events").Scan(&remainingEvents); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if remainingEvents != 0 {
		t.Fatalf("remaining events = %d, want 0", remainingEvents)
	}

	// idempotent on empty store
	deleted, err = st.DeleteAllSessions(ctx)
	if err != nil {
		t.Fatalf("DeleteAllSessions empty: %v", err)
	}
	if deleted != 0 {
		t.Fatalf("second call deleted = %d, want 0", deleted)
	}
}

func TestDeleteWorkspaceSessionsCascadesEvents(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t, filepath.Join(t.TempDir(), "ub.db"))
	defer st.Close()

	base := time.UnixMilli(1_700_000_000_000).UTC()
	for _, sess := range []Session{
		{ID: "a1", Workspace: "/repo/a", Title: "a1", CreatedAt: base, UpdatedAt: base},
		{ID: "a2", Workspace: "/repo/a", Title: "a2", CreatedAt: base, UpdatedAt: base},
		{ID: "b1", Workspace: "/repo/b", Title: "b1", CreatedAt: base, UpdatedAt: base},
	} {
		if err := st.CreateSession(ctx, sess); err != nil {
			t.Fatalf("CreateSession(%s): %v", sess.ID, err)
		}
		if _, err := st.db.ExecContext(ctx, `INSERT INTO events
			(id, session_id, turn, time, type, payload)
			VALUES (?, ?, 1, ?, 'user_message', ?)`,
			"event-"+sess.ID, sess.ID, base.UnixMilli(), []byte(`{"text":"hi"}`)); err != nil {
			t.Fatalf("insert event for %s: %v", sess.ID, err)
		}
	}

	deleted, err := st.DeleteWorkspaceSessions(ctx, "/repo/a")
	if err != nil {
		t.Fatalf("DeleteWorkspaceSessions: %v", err)
	}
	if deleted != 2 {
		t.Fatalf("deleted = %d, want 2", deleted)
	}

	var remainingEvents int
	if err := st.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM events").Scan(&remainingEvents); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if remainingEvents != 1 {
		t.Fatalf("remaining events = %d, want 1", remainingEvents)
	}
	if _, err := st.GetSession(ctx, "b1"); err != nil {
		t.Fatalf("other workspace session should remain: %v", err)
	}
}

func TestPruneSessionsDeletesOldOutsideRecentRetention(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t, filepath.Join(t.TempDir(), "ub.db"))
	defer st.Close()

	now := time.UnixMilli(1_800_000_000_000).UTC()
	for i := 0; i < 5; i++ {
		updated := now.Add(-time.Duration(50-i) * 24 * time.Hour)
		id := "a" + string(rune('0'+i))
		if err := st.CreateSession(ctx, Session{
			ID:        id,
			Workspace: "/repo/a",
			Title:     id,
			CreatedAt: updated,
			UpdatedAt: updated,
		}); err != nil {
			t.Fatalf("CreateSession(%s): %v", id, err)
		}
		if _, err := st.db.ExecContext(ctx, `INSERT INTO events
			(id, session_id, turn, time, type, payload)
			VALUES (?, ?, 1, ?, 'user_message', ?)`,
			"event-"+id, id, updated.UnixMilli(), []byte(`{"text":"hi"}`)); err != nil {
			t.Fatalf("insert event for %s: %v", id, err)
		}
	}

	result, err := st.PruneSessions(ctx, PruneOptions{
		MaxAge:                30 * 24 * time.Hour,
		MinRecentPerWorkspace: 2,
		Now:                   now,
	})
	if err != nil {
		t.Fatalf("PruneSessions: %v", err)
	}
	if result.Deleted != 3 {
		t.Fatalf("deleted = %d, want 3", result.Deleted)
	}
	for _, id := range []string{"a0", "a1", "a2"} {
		if _, err := st.GetSession(ctx, id); !errors.Is(err, ErrNotFound) {
			t.Fatalf("GetSession(%s) err = %v, want ErrNotFound", id, err)
		}
	}
	for _, id := range []string{"a3", "a4"} {
		if _, err := st.GetSession(ctx, id); err != nil {
			t.Fatalf("GetSession(%s) should remain: %v", id, err)
		}
	}
	var remainingEvents int
	if err := st.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM events").Scan(&remainingEvents); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if remainingEvents != 2 {
		t.Fatalf("remaining events = %d, want 2", remainingEvents)
	}
}

func TestPruneSessionsRetainsRecentPerWorkspaceIndependently(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t, filepath.Join(t.TempDir(), "ub.db"))
	defer st.Close()

	now := time.UnixMilli(1_800_000_000_000).UTC()
	for _, workspace := range []string{"/repo/a", "/repo/b"} {
		for i := 0; i < 3; i++ {
			updated := now.Add(-time.Duration(90-i) * 24 * time.Hour)
			id := strings.TrimPrefix(workspace, "/repo/") + string(rune('0'+i))
			if err := st.CreateSession(ctx, Session{
				ID:        id,
				Workspace: workspace,
				Title:     id,
				CreatedAt: updated,
				UpdatedAt: updated,
			}); err != nil {
				t.Fatalf("CreateSession(%s): %v", id, err)
			}
		}
	}

	result, err := st.PruneSessions(ctx, PruneOptions{
		MaxAge:                30 * 24 * time.Hour,
		MinRecentPerWorkspace: 2,
		Now:                   now,
	})
	if err != nil {
		t.Fatalf("PruneSessions: %v", err)
	}
	if result.Deleted != 2 {
		t.Fatalf("deleted = %d, want 2", result.Deleted)
	}
	for _, id := range []string{"a0", "b0"} {
		if _, err := st.GetSession(ctx, id); !errors.Is(err, ErrNotFound) {
			t.Fatalf("GetSession(%s) err = %v, want ErrNotFound", id, err)
		}
	}
	for _, id := range []string{"a1", "a2", "b1", "b2"} {
		if _, err := st.GetSession(ctx, id); err != nil {
			t.Fatalf("GetSession(%s) should remain: %v", id, err)
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
