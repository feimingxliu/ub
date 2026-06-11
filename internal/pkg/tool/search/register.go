package search

import (
	"fmt"
	"path/filepath"

	"github.com/feimingxliu/ub/internal/pkg/tool"
)

// Register adds the grep tool to reg. The workspace root is cleaned
// and shared by the tool instance.
func Register(reg *tool.Registry, root string) error {
	if reg == nil {
		return fmt.Errorf("search: nil registry")
	}
	if root == "" {
		return fmt.Errorf("search: empty workspace root")
	}
	return reg.Register(newGrepTool(filepath.Clean(root)))
}
