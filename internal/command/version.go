package command

import (
	"fmt"
	"runtime/debug"
)

// Version returns a human-readable build identifier derived from
// runtime/debug.ReadBuildInfo. For tagged release builds, Main.Version is
// the module version (e.g. "v0.1.0"). For builds from a git checkout, Go
// already encodes a pseudo-version with a "+dirty" suffix when applicable.
// Only when Main.Version is empty or "(devel)" do we fall back to the
// VCS revision from Settings.
func Version() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "(unknown)"
	}
	if v := info.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	var rev, dirty string
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			if len(s.Value) >= 7 {
				rev = s.Value[:7]
			}
		case "vcs.modified":
			if s.Value == "true" {
				dirty = "-dirty"
			}
		}
	}
	if rev != "" {
		return fmt.Sprintf("dev+%s%s", rev, dirty)
	}
	return "(unknown)"
}
