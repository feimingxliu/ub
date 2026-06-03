package cli

import (
	"fmt"
	"os"

	"github.com/feimingxliu/ub/internal/paths"
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
