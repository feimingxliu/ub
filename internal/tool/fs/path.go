package fs

import "github.com/feimingxliu/ub/internal/tool"

// resolve is a thin alias to tool.Resolve so existing call sites in
// this package keep their short, local-feeling names.
func resolve(root, path string) (string, error) {
	return tool.Resolve(root, path)
}

// relToRoot mirrors tool.RelToRoot for the same reason.
func relToRoot(root, abs string) (string, error) {
	return tool.RelToRoot(root, abs)
}
