package devflow_test

import (
	gitmod "webtyp.com/git"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"webtyp.com/devflow"
)

// testCreateGoModule duplicated here because I cannot access it from other files easily in this env it seems?
// Actually it is in helpers_test.go, but maybe the test run didn't include it.
func localTestCreateGoModule(moduleName string) (dir string, cleanup func()) {
	dir, _ = os.MkdirTemp("", "gitgo-gomod-")

	// Create go.mod
	gomod := "module " + moduleName + "\n\ngo 1.20\n"
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0644)

	// Create main.go
	main := "package main\n\nfunc main() {}\n"
	os.WriteFile(filepath.Join(dir, "main.go"), []byte(main), 0644)

	cleanup = func() {
		os.RemoveAll(dir)
	}

	return dir, cleanup
}

func TestInternalSubmoduleHandling(t *testing.T) {
	// 1. Create a parent module and an internal submodule
	tmpDir, cleanup := localTestCreateGoModule("github.com/parent")
	defer cleanup()

	subDir := filepath.Join(tmpDir, "sub")
	os.MkdirAll(subDir, 0755)

	subModContent := "module github.com/parent/sub\n\ngo 1.20\n\nrequire github.com/parent v0.0.1\nreplace github.com/parent => ../\n"
	os.WriteFile(filepath.Join(subDir, "go.mod"), []byte(subModContent), 0644)
	os.WriteFile(filepath.Join(subDir, "main.go"), []byte("package sub\n"), 0644)

	git, _ := gitmod.NewGit()
	git.SetRootDir(tmpDir)

	gh, _ := devflow.NewGo(git)
	gh.SetRootDir(tmpDir)

	// 2. FindDependentModules should EXCLUDE internal submodules
	deps, err := gh.FindDependentModules("github.com/parent", tmpDir)
	if err != nil {
		t.Fatalf("FindDependentModules failed: %v", err)
	}

	for _, dep := range deps {
		if strings.HasPrefix(dep, tmpDir) {
			t.Errorf("Internal submodule %s should be excluded from dependents", dep)
		}
	}

	// 3. EnsureReplace should add/keep replace parent => ../
	gomod := devflow.NewGoModHandler()
	gomod.SetRootDir(subDir)

	// Should keep existing
	changed := gomod.EnsureReplace("github.com/parent", "../")
	if changed {
		t.Error("EnsureReplace should not report change when replace already exists correctly")
	}

	// Should add if missing
	os.WriteFile(filepath.Join(subDir, "go.mod"), []byte("module github.com/parent/sub\n\ngo 1.20\n\nrequire github.com/parent v0.0.1\n"), 0644)
	gomod = devflow.NewGoModHandler()
	gomod.SetRootDir(subDir)
	changed = gomod.EnsureReplace("github.com/parent", "../")
	if !changed {
		t.Error("EnsureReplace should report change when adding missing replace")
	}
	gomod.Save()

	content, _ := os.ReadFile(filepath.Join(subDir, "go.mod"))
	if !strings.Contains(string(content), "replace github.com/parent => ../") {
		t.Errorf("go.mod missing expected replace. Got:\n%s", string(content))
	}

	// Test with replace block
	os.WriteFile(filepath.Join(subDir, "go.mod"), []byte("module github.com/parent/sub\n\ngo 1.20\n\nreplace (\n\tgithub.com/other => ./other\n)\n"), 0644)
	gomod = devflow.NewGoModHandler()
	gomod.SetRootDir(subDir)
	changed = gomod.EnsureReplace("github.com/parent", "../")
	if !changed {
		t.Error("EnsureReplace should report change when adding to block")
	}
	gomod.Save()
	content, _ = os.ReadFile(filepath.Join(subDir, "go.mod"))
	if strings.Contains(string(content), "replace github.com/parent => ../") {
		t.Errorf("Should not contain 'replace' keyword inside block, got:\n%s", string(content))
	}
	if !strings.Contains(string(content), "github.com/parent => ../") {
		t.Errorf("Block should contain module path, got:\n%s", string(content))
	}
}
