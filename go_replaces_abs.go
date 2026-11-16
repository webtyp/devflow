package devflow

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/mod/modfile"
)

// rewriteRelativeReplaces makes every relative replace target of the go.mod at goModPath resolve from where the
// module really lives. realDir is the module's directory in the real checkout and top the repository root there.
// A target that stays inside top keeps pointing inside the temporary tree (path unchanged); one that leaves top
// becomes the absolute path it has from realDir.
func rewriteRelativeReplaces(goModPath, realDir, top string) error {
	data, err := os.ReadFile(goModPath)
	if err != nil {
		return err
	}
	f, err := modfile.Parse(goModPath, data, nil)
	if err != nil {
		return err
	}
	changed := false
	for _, r := range append([]*modfile.Replace(nil), f.Replace...) {
		if r.New.Version != "" || filepath.IsAbs(r.New.Path) || !isLocalPath(r.New.Path) {
			continue
		}
		abs := filepath.Clean(filepath.Join(realDir, r.New.Path))
		if abs == top || strings.HasPrefix(abs, top+string(filepath.Separator)) {
			continue
		}
		if err := f.AddReplace(r.Old.Path, r.Old.Version, abs, ""); err != nil {
			return err
		}
		changed = true
	}
	if !changed {
		return nil
	}
	out, err := f.Format()
	if err != nil {
		return err
	}
	return os.WriteFile(goModPath, out, 0o644)
}

// isLocalPath reports whether a replace target is a directory path (./x, ../x) rather than a module path.
func isLocalPath(p string) bool {
	return p == "." || p == ".." || strings.HasPrefix(p, "./") || strings.HasPrefix(p, "../")
}
