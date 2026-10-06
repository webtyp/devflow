package devflow_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"webtyp.com/devflow"
)

// TestSubmoduleRunCarriesCoverageFlags guards the defect found in webtyp/auth:
// the suite lived in tests/ with its own go.mod, gotest ran it, its tests
// covered 65.7% of the parent — and the reported number was 1.2%.
//
// The submodule ran with -cover but WITHOUT -coverprofile, so its result
// existed only as text in the log while the percentage was read from the root
// profile alone. The two flags below are what make the submodule's coverage
// survive as data: -coverpkg so it measures the parent's code rather than the
// test package's own, and -coverprofile so the number can be merged in.
func TestSubmoduleRunCarriesCoverageFlags(t *testing.T) {
	parent, cleanup := testCreateGoModule("example.com/parent")
	defer cleanup()

	subDir := filepath.Join(parent, "tests")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	subMod := "module example.com/parent/tests\n\ngo 1.20\n\n" +
		"require example.com/parent v0.0.1\n\nreplace example.com/parent => ../\n"
	if err := os.WriteFile(filepath.Join(subDir, "go.mod"), []byte(subMod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "sub_test.go"), []byte("package tests\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	any := captureGoTest(t, parent, devflow.TestOptions{NoCache: true})

	// The submodule run is the one carrying -coverpkg at the parent module.
	isSubRun := func(a []string) bool {
		return hasArg(a, "-coverpkg=example.com/parent/...")
	}
	if !any(isSubRun) {
		t.Fatal("submodule run must pass -coverpkg at the parent module, or it measures the test package instead of the code under test")
	}
	if !any(func(a []string) bool {
		return isSubRun(a) && hasArgWithPrefix(a, "-coverprofile=")
	}) {
		t.Error("submodule run must pass -coverprofile: without it the submodule's coverage never reaches the reported percentage (auth reported 1.2% instead of 64.4%)")
	}

	// The root and submodule runs must not write to the same profile path, or
	// the second overwrites the first and the merge has nothing to combine.
	var profiles []string
	_ = any(func(a []string) bool {
		for _, arg := range a {
			if strings.HasPrefix(arg, "-coverprofile=") {
				profiles = append(profiles, arg)
			}
		}
		return false
	})
	seen := map[string]bool{}
	for _, p := range profiles {
		if seen[p] {
			t.Errorf("two runs share the coverage profile %q; the second overwrites the first", p)
		}
		seen[p] = true
	}
}
