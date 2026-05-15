package tool

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Resolve normalizes path relative to root and returns its cleaned
// absolute form. It rejects any path that escapes root via "..", whether
// the input was relative or absolute. The returned path is always an
// absolute path under root.
//
// Resolve is shared by the local tool packages (fs, search, ...) so
// every tool enforces the same workspace sandbox.
func Resolve(root, path string) (string, error) {
	if root == "" {
		return "", fmt.Errorf("tool: empty workspace root")
	}
	cleanRoot := filepath.Clean(root)

	var abs string
	if filepath.IsAbs(path) {
		abs = filepath.Clean(path)
	} else {
		abs = filepath.Clean(filepath.Join(cleanRoot, path))
	}

	rel, err := filepath.Rel(cleanRoot, abs)
	if err != nil {
		return "", fmt.Errorf("tool: resolve %q: %w", path, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("tool: path %q is outside workspace root", path)
	}
	return abs, nil
}

// RelToRoot returns the POSIX-style relative path from root to abs.
// abs MUST already be a path under root (e.g. produced by Resolve).
func RelToRoot(root, abs string) (string, error) {
	rel, err := filepath.Rel(filepath.Clean(root), abs)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}
