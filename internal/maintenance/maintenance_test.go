package maintenance

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/feimingxliu/ub/internal/config"
	"github.com/feimingxliu/ub/internal/store"
)

func TestStatePathUsesXDGStateHome(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	got, err := StatePath()
	if err != nil {
		t.Fatalf("StatePath: %v", err)
	}
	want := filepath.Join(state, "ub", "cleanup.json")
	if got != want {
		t.Fatalf("StatePath() = %q, want %q", got, want)
	}
}

func TestRunStartupPrunesSessionsAndWritesState(t *testing.T) {
	ctx := context.Background()
	temp := t.TempDir()
	storePath := filepath.Join(temp, "ub.db")
	statePath := filepath.Join(temp, "state", "cleanup.json")
	now := time.UnixMilli(1_800_000_000_000).UTC()
	st := openMaintenanceStore(t, storePath)
	old := now.Add(-60 * 24 * time.Hour)
	if err := st.CreateSession(ctx, store.Session{
		ID:        "old",
		Workspace: "/repo",
		Title:     "old",
		CreatedAt: old,
		UpdatedAt: old,
	}); err != nil {
		t.Fatalf("CreateSession(old): %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	cfg := config.Defaults()
	cfg.Cleanup.Interval = 24 * time.Hour
	cfg.Cleanup.Sessions.MaxAge = 30 * 24 * time.Hour
	cfg.Cleanup.Sessions.MinRecentPerWorkspace = 0
	if err := runStartup(ctx, cfg, startupOptions{
		StorePath: storePath,
		StatePath: statePath,
		Now:       func() time.Time { return now },
	}); err != nil {
		t.Fatalf("runStartup: %v", err)
	}

	st = openMaintenanceStore(t, storePath)
	defer st.Close()
	if _, err := st.GetSession(ctx, "old"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetSession(old) err = %v, want ErrNotFound", err)
	}
	raw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	var state cleanupState
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatalf("parse state: %v", err)
	}
	if !state.LastRun.Equal(now) {
		t.Fatalf("last_run = %s, want %s", state.LastRun, now)
	}
}

func TestRunStartupSkipsWithinInterval(t *testing.T) {
	ctx := context.Background()
	temp := t.TempDir()
	storePath := filepath.Join(temp, "ub.db")
	statePath := filepath.Join(temp, "state", "cleanup.json")
	now := time.UnixMilli(1_800_000_000_000).UTC()
	st := openMaintenanceStore(t, storePath)
	old := now.Add(-60 * 24 * time.Hour)
	if err := st.CreateSession(ctx, store.Session{
		ID:        "old",
		Workspace: "/repo",
		Title:     "old",
		CreatedAt: old,
		UpdatedAt: old,
	}); err != nil {
		t.Fatalf("CreateSession(old): %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	if err := writeCleanupState(statePath, cleanupState{LastRun: now.Add(-time.Hour)}); err != nil {
		t.Fatalf("write state: %v", err)
	}

	cfg := config.Defaults()
	cfg.Cleanup.Interval = 24 * time.Hour
	cfg.Cleanup.Sessions.MaxAge = 30 * 24 * time.Hour
	cfg.Cleanup.Sessions.MinRecentPerWorkspace = 0
	if err := runStartup(ctx, cfg, startupOptions{
		StorePath: storePath,
		StatePath: statePath,
		Now:       func() time.Time { return now },
	}); err != nil {
		t.Fatalf("runStartup: %v", err)
	}

	st = openMaintenanceStore(t, storePath)
	defer st.Close()
	if _, err := st.GetSession(ctx, "old"); err != nil {
		t.Fatalf("session should remain when interval has not elapsed: %v", err)
	}
}

func TestRunStartupDisabledDoesNotPruneOrWriteState(t *testing.T) {
	ctx := context.Background()
	temp := t.TempDir()
	storePath := filepath.Join(temp, "ub.db")
	statePath := filepath.Join(temp, "state", "cleanup.json")
	now := time.UnixMilli(1_800_000_000_000).UTC()
	st := openMaintenanceStore(t, storePath)
	old := now.Add(-60 * 24 * time.Hour)
	if err := st.CreateSession(ctx, store.Session{
		ID:        "old",
		Workspace: "/repo",
		Title:     "old",
		CreatedAt: old,
		UpdatedAt: old,
	}); err != nil {
		t.Fatalf("CreateSession(old): %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	cfg := config.Defaults()
	enabled := false
	cfg.Cleanup.Enabled = &enabled
	cfg.Cleanup.Sessions.MinRecentPerWorkspace = 0
	if err := runStartup(ctx, cfg, startupOptions{
		StorePath: storePath,
		StatePath: statePath,
		Now:       func() time.Time { return now },
	}); err != nil {
		t.Fatalf("runStartup: %v", err)
	}

	st = openMaintenanceStore(t, storePath)
	defer st.Close()
	if _, err := st.GetSession(ctx, "old"); err != nil {
		t.Fatalf("session should remain when cleanup is disabled: %v", err)
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("state file should not be written, stat err = %v", err)
	}
}

func TestRunStartupPrunesOldToolOutputs(t *testing.T) {
	ctx := context.Background()
	temp := t.TempDir()
	storePath := filepath.Join(temp, "ub.db")
	statePath := filepath.Join(temp, "state", "cleanup.json")
	outputRoot := filepath.Join(temp, "state", "tool_outputs", "sess")
	oldPath := filepath.Join(outputRoot, "old.txt")
	newPath := filepath.Join(outputRoot, "new.txt")
	if err := os.MkdirAll(outputRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldPath, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newPath, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.UnixMilli(1_800_000_000_000).UTC()
	oldTime := now.Add(-8 * 24 * time.Hour)
	newTime := now.Add(-time.Hour)
	if err := os.Chtimes(oldPath, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(newPath, newTime, newTime); err != nil {
		t.Fatal(err)
	}

	cfg := config.Defaults()
	cfg.Cleanup.Interval = 24 * time.Hour
	cfg.Cleanup.Sessions.MaxAge = 0
	cfg.Context.ToolResults.SpilloverMaxAge = 7 * 24 * time.Hour
	if err := runStartup(ctx, cfg, startupOptions{
		StorePath: storePath,
		StatePath: statePath,
		Now:       func() time.Time { return now },
	}); err != nil {
		t.Fatalf("runStartup: %v", err)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("old output stat err = %v, want not exist", err)
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("new output should remain: %v", err)
	}
}

func openMaintenanceStore(t *testing.T, path string) *store.Store {
	t.Helper()
	st, err := store.Open(path)
	if err != nil {
		t.Fatalf("Open(%q): %v", path, err)
	}
	return st
}
