// Package maintenance runs low-frequency startup cleanup tasks.
package maintenance

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/feimingxliu/ub/internal/config"
	"github.com/feimingxliu/ub/internal/store"
)

// RunStartup performs best-effort startup maintenance. Failures are logged as
// warnings and do not block the caller.
func RunStartup(ctx context.Context, cfg *config.Config) {
	if err := runStartup(ctx, cfg, startupOptions{}); err != nil {
		slog.Warn("startup cleanup failed", "err", err)
	}
}

type startupOptions struct {
	StorePath string
	StatePath string
	Now       func() time.Time
}

type cleanupState struct {
	LastRun time.Time `json:"last_run"`
}

func runStartup(ctx context.Context, cfg *config.Config, opts startupOptions) error {
	if cfg == nil || !cfg.Cleanup.CleanupEnabled() {
		return nil
	}
	now := time.Now().UTC()
	if opts.Now != nil {
		now = opts.Now().UTC()
	}
	statePath, err := cleanupStatePath(opts.StatePath)
	if err != nil {
		return err
	}
	if cfg.Cleanup.Interval > 0 {
		state, ok, err := readCleanupState(statePath)
		if err != nil {
			return err
		}
		if ok && !state.LastRun.IsZero() && now.Sub(state.LastRun) < cfg.Cleanup.Interval {
			return nil
		}
	}
	if err := pruneSessions(ctx, cfg, opts.StorePath, now); err != nil {
		return err
	}
	if err := writeCleanupState(statePath, cleanupState{LastRun: now}); err != nil {
		return err
	}
	return nil
}

func pruneSessions(ctx context.Context, cfg *config.Config, storePath string, now time.Time) error {
	if cfg.Cleanup.Sessions.MaxAge <= 0 {
		return nil
	}
	path := storePath
	if path == "" {
		var err error
		path, err = store.DefaultPath()
		if err != nil {
			return fmt.Errorf("locate session store: %w", err)
		}
	}
	st, err := store.Open(path)
	if err != nil {
		return err
	}
	defer st.Close()
	result, err := st.PruneSessions(ctx, store.PruneOptions{
		MaxAge:                cfg.Cleanup.Sessions.MaxAge,
		MinRecentPerWorkspace: cfg.Cleanup.Sessions.MinRecentPerWorkspace,
		Now:                   now,
	})
	if err != nil {
		return err
	}
	if result.Deleted > 0 {
		slog.Info("startup cleanup pruned sessions", "deleted", result.Deleted)
	}
	return nil
}

// StatePath returns the user-level startup cleanup state file path.
func StatePath() (string, error) {
	return cleanupStatePath("")
}

func cleanupStatePath(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	if xdg := os.Getenv("XDG_STATE_HOME"); xdg != "" {
		return filepath.Join(xdg, "ub", "cleanup.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "ub", "cleanup.json"), nil
}

func readCleanupState(path string) (cleanupState, bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cleanupState{}, false, nil
		}
		return cleanupState{}, false, fmt.Errorf("read cleanup state %s: %w", path, err)
	}
	var state cleanupState
	if err := json.Unmarshal(raw, &state); err != nil {
		return cleanupState{}, false, fmt.Errorf("parse cleanup state %s: %w", path, err)
	}
	return state, true, nil
}

func writeCleanupState(path string, state cleanupState) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create cleanup state directory: %w", err)
	}
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal cleanup state: %w", err)
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return fmt.Errorf("write cleanup state %s: %w", path, err)
	}
	return nil
}
