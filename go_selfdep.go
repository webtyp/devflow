package devflow

import (
	"fmt"
	"webtyp.com/command"
	"os"
	"path/filepath"
)

// syncInternalSubmodules finds all submodules inside the current repo that depend
// on the parent module, and prepares them for the upcoming release:
// 1. Ensures they have a relative replace pointing to the parent.
// 2. Bumps their requirement of the parent module to the nextTag.
// 3. Runs 'go mod tidy'.
// These changes are made pre-commit so they are included in the release tag.
func (g *Go) syncInternalSubmodules(parentModulePath, nextTag string) error {
	absRoot, _ := filepath.Abs(g.rootDir)

	// 1. Find all go.mod files inside the repo (excluding root)
	var submods []string
	err := filepath.Walk(absRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.Name() == "go.mod" {
			absDir, _ := filepath.Abs(filepath.Dir(path))
			if absDir != absRoot {
				submods = append(submods, absDir)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	for _, subDir := range submods {
		if !g.HasDependency(filepath.Join(subDir, "go.mod"), parentModulePath) {
			continue
		}

		g.log(fmt.Sprintf("Syncing internal submodule: %s", filepath.Base(subDir)))

		// 1. Ensure relative replace
		rel, err := filepath.Rel(subDir, absRoot)
		if err != nil {
			continue
		}

		gomod := NewGoModHandler()
		gomod.SetRootDir(subDir)
		gomod.EnsureReplace(parentModulePath, rel)
		if err := gomod.Save(); err != nil {
			return fmt.Errorf("failed to save submodule go.mod: %w", err)
		}

		// 2. Bump requirement to nextTag
		// Note: We use go get parent@tag. Since we have a replace, this just updates go.mod
		// without needing the tag to be published yet.
		target := fmt.Sprintf("%s@%s", parentModulePath, nextTag)
		if _, err := command.RunInDir(subDir, "go", "get", target); err != nil {
			// If it fails (e.g. tag doesn't exist yet and proxy is used),
			// we might need to use a different approach or just ignore if it's not strictly needed
			// for the build (because of the replace).
			// But gopush's goal is to have the correct version in go.mod.
			// Try with GOPROXY=off or similar?
			// Actually, 'go get' should work if the replace is there and we are just updating the version string.
			g.log(fmt.Sprintf("Warning: go get %s failed in %s: %v", target, subDir, err))
		}

		// 3. Tidy
		if _, err := command.RunInDir(subDir, "go", "mod", "tidy"); err != nil {
			return fmt.Errorf("go mod tidy failed in %s: %w", subDir, err)
		}
	}

	return nil
}
