package command

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/feimingxliu/ub/internal/workspace/paths"
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
	canonical, err := paths.CanonicalPath(path)
	if err != nil {
		return "", err
	}
	return canonical, nil
}

// shortenWorkspaceForDisplay replaces the home directory prefix with "~" so
// workspace paths fit better in a table column.
func shortenWorkspaceForDisplay(workspace string) string {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return "-"
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return workspace
	}
	rel, err := filepath.Rel(home, workspace)
	if err != nil {
		return workspace
	}
	if rel == "." {
		return "~"
	}
	if strings.HasPrefix(rel, "..") {
		return workspace
	}
	return filepath.Join("~", rel)
}
