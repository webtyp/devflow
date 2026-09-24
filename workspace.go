package devflow

import (
	"os"
	"path/filepath"
	"strings"
)

// WorkspaceFileExt is the marker that makes a directory a workspace root.
const WorkspaceFileExt = ".code-workspace"

// walkSkipDirs are directory names that never hold a dependent module.
var walkSkipDirs = []string{"node_modules", "vendor"}

// WorkspaceRoot returns the OUTERMOST ancestor of dir (dir included) that
// contains a *.code-workspace file, never climbing to $HOME or above.
// It returns "" when no ancestor qualifies.
func WorkspaceRoot(dir string) string {
	home, _ := os.UserHomeDir() // "" on error: then only the disk root stops the climb
	return WorkspaceRootFrom(dir, home)
}

// WorkspaceRootFrom is WorkspaceRoot with an explicit home directory — the
// seam tests use to build a fake tree under t.TempDir().
func WorkspaceRootFrom(dir, home string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	if home != "" {
		if absHome, err := filepath.Abs(home); err == nil {
			home = absHome
		}
	}

	found := ""
	d := abs
	for {
		if d == home || d == filepath.Dir(d) {
			break
		}
		matches, _ := filepath.Glob(filepath.Join(d, "*"+WorkspaceFileExt))
		for _, match := range matches {
			if info, err := os.Stat(match); err == nil && !info.IsDir() {
				found = d
				break
			}
		}
		d = filepath.Dir(d)
	}
	return found
}

// skipWalkDir reports whether a directory should be skipped during filepath.Walk
// because it is a known non-module directory (e.g., node_modules, vendor, or hidden).
// searchPath itself is never skipped even if it starts with "." (e.g. "..").
func skipWalkDir(path, searchPath string, info os.FileInfo) bool {
	if !info.IsDir() {
		return false
	}
	if path == searchPath || filepath.Clean(path) == filepath.Clean(searchPath) {
		return false
	}
	name := info.Name()
	for _, skip := range walkSkipDirs {
		if name == skip {
			return true
		}
	}
	if strings.HasPrefix(name, ".") {
		return true
	}
	return false
}
