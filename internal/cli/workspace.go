package cli

import (
	"fmt"
	"os"
	"path/filepath"
)

func currentWorkspace() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get cwd: %w", err)
	}
	workspace, err := canonicalWorkspace(cwd)
	if err != nil {
		return "", err
	}
	return workspace, nil
}

func canonicalWorkspace(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("workspace path is empty")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("abs workspace %q: %w", path, err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("resolve workspace symlinks %q: %w", abs, err)
	}
	clean := filepath.Clean(resolved)
	if root := gitRoot(clean); root != "" {
		return root, nil
	}
	return clean, nil
}

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
