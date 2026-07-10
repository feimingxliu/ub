package config

import (
	"os"
	"path/filepath"
)

// maxLocalAncestorWalk is how many parent directories to walk up from cwd
// when looking for .ub/config.yaml. 5 matches the spec.
const maxLocalAncestorWalk = 5

// globalConfigPath returns the canonical path to the user-global config
// file. It prefers $XDG_CONFIG_HOME, falling back to $HOME/.config.
//
// The file is NOT required to exist; callers check separately.
func globalConfigPath() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "ub", "config.yaml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "ub", "config.yaml"), nil
}

// localConfigPath walks up from cwd at most maxLocalAncestorWalk levels
// looking for a .ub/config.yaml. Returns the absolute path of the first
// match, or empty string when none is found.
func localConfigPath(cwd string) string {
	dir := cwd
	for i := 0; i <= maxLocalAncestorWalk; i++ {
		candidate := filepath.Join(dir, ".ub", "config.yaml")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break // reached filesystem root
		}
		dir = parent
	}
	return ""
}
