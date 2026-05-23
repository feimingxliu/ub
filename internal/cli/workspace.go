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
		if info, err := os.Stat(filepath.Join(path, ".git")); err == nil && (info.IsDir() || info.Mode().IsRegular()) {
			return path
		}
		parent := filepath.Dir(path)
		if parent == path {
			return ""
		}
		path = parent
	}
}
