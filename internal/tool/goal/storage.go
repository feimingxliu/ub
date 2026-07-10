package goal

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/feimingxliu/ub/internal/workspace/paths"
)

const (
	stateDirPerm  = 0o755
	stateFilePerm = 0o644
)

var unsafePathPart = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

var mu sync.Mutex

// statePath returns the file system path for a session's goal state.
func statePath(sessionID string) (string, error) {
	if strings.TrimSpace(sessionID) == "" {
		return "", fmt.Errorf("goal: session id is required")
	}
	root, err := paths.StateRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "goals", safePathPart(sessionID)+".json"), nil
}

func safePathPart(value string) string {
	value = strings.TrimSpace(value)
	value = unsafePathPart.ReplaceAllString(value, "_")
	value = strings.Trim(value, "._-")
	if value == "" {
		return "session"
	}
	if len(value) > 80 {
		value = value[:80]
		value = strings.Trim(value, "._-")
	}
	if value == "" {
		return "session"
	}
	return value
}

// Load reads the goal state for a session. Returns nil, nil when no goal
// exists.
func Load(sessionID string) (*Goal, error) {
	mu.Lock()
	defer mu.Unlock()
	path, err := statePath(sessionID)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read goal state: %w", err)
	}
	var g Goal
	if err := json.Unmarshal(raw, &g); err != nil {
		return nil, fmt.Errorf("decode goal state: %w", err)
	}
	return &g, nil
}

// Save persists the goal state for a session.
func Save(sessionID string, g *Goal) error {
	if g == nil {
		return fmt.Errorf("goal: cannot save nil goal")
	}
	mu.Lock()
	defer mu.Unlock()
	path, err := statePath(sessionID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), stateDirPerm); err != nil {
		return fmt.Errorf("create goal dir: %w", err)
	}
	raw, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		return fmt.Errorf("encode goal state: %w", err)
	}
	raw = append(raw, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".goal-*.tmp")
	if err != nil {
		return fmt.Errorf("create goal tmp: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		if tmpName != "" {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write goal tmp: %w", err)
	}
	if err := tmp.Chmod(stateFilePerm); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod goal tmp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close goal tmp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename goal: %w", err)
	}
	tmpName = ""
	return nil
}

// Delete removes the goal state file for a session.
func Delete(sessionID string) error {
	mu.Lock()
	defer mu.Unlock()
	path, err := statePath(sessionID)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete goal state: %w", err)
	}
	return nil
}
