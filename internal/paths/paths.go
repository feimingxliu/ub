// Package paths centralizes all XDG-based and project-keyed directory
// calculations for ub. Other packages (memory, plan, tooloutput, etc.)
// should call functions here instead of computing paths locally.
package paths

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// StateRoot returns ub's user state directory ($XDG_STATE_HOME/ub or
// ~/.local/state/ub).
func StateRoot() (string, error) {
	if xdg := strings.TrimSpace(os.Getenv("XDG_STATE_HOME")); xdg != "" {
		return filepath.Join(xdg, "ub"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "ub"), nil
}

// ConfigHome returns the user config directory ($XDG_CONFIG_HOME or
// ~/.config).
func ConfigHome() (string, error) {
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		return xdg, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config"), nil
}

// DataHome returns the user data directory ($XDG_DATA_HOME or
// ~/.local/share).
func DataHome() (string, error) {
	if xdg := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); xdg != "" {
		return xdg, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share"), nil
}

// ProjectKey returns a stable, filesystem-safe key for a workspace. The key
// is the first 16 hex characters of SHA-256(canonicalPath), where
// canonicalPath is the workspace root resolved through symlinks and
// optionally walked up to the git root.
//
// An empty workspace returns an error.
func ProjectKey(workspace string) (string, error) {
	canonical, err := CanonicalPath(workspace)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(h[:8]), nil // 16 hex chars
}

// canonicalPath resolves workspace to a canonical absolute path. It follows
// symlinks and walks up to the git root if one exists.
func CanonicalPath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("paths: workspace path is empty")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("paths: abs workspace %q: %w", path, err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("paths: resolve symlinks %q: %w", abs, err)
	}
	clean := filepath.Clean(resolved)
	if root := gitRoot(clean); root != "" {
		return root, nil
	}
	return clean, nil
}

// gitRoot walks up from path looking for a .git marker.
func gitRoot(path string) string {
	for {
		if validGitMarker(filepath.Join(path, ".git")) {
			return path
		}
		parent := filepath.Dir(path)
		if parent == path {
			return ""
		}
		path = parent
	}
}

func validGitMarker(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	if info.Mode().IsRegular() {
		return true
	}
	if !info.IsDir() {
		return false
	}
	if _, err := os.Stat(filepath.Join(path, "HEAD")); err == nil {
		return true
	}
	return false
}
