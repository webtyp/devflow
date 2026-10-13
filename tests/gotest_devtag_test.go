package devflow_test

import (
	"os"
	"path/filepath"
	"testing"

	"webtyp.com/devflow"
)

func hasDevTag(a []string) bool {
	for i, s := range a {
		if s == "-tags=dev" || (s == "-tags" && i+1 < len(a) && a[i+1] == "dev") {
			return true
		}
	}
	return false
}

// Code behind //go:build dev (debug-only features such as webtyp/auth's
// DEV_AUTOLOGIN) is invisible to the default run. gotest runs the suite once
// more with -tags dev when the module has such files, so that code stays
// tested instead of rotting unseen.
func TestGo_DevTaggedFilesGetADevPass(t *testing.T) {
	t.Run("ModuleWithDevFileRunsDevPass", func(t *testing.T) {
		dir, cleanup := testCreateGoModule("example.com/devtag")
		defer cleanup()
		os.WriteFile(filepath.Join(dir, "debug_dev.go"), []byte("//go:build dev\n\npackage main\n"), 0644)

		any := captureGoTest(t, dir, devflow.TestOptions{NoCache: true})
		if !any(hasDevTag) {
			t.Error("a module with //go:build dev files must get a go test run with -tags dev")
		}
	})

	t.Run("ModuleWithoutDevFileRunsNoDevPass", func(t *testing.T) {
		dir, cleanup := testCreateGoModule("example.com/nodevtag")
		defer cleanup()

		any := captureGoTest(t, dir, devflow.TestOptions{NoCache: true})
		if any(hasDevTag) {
			t.Error("a module without //go:build dev files must not get a -tags dev run")
		}
	})

	t.Run("NegatedDevTagIsNotADevFile", func(t *testing.T) {
		dir, cleanup := testCreateGoModule("example.com/notdev")
		defer cleanup()
		os.WriteFile(filepath.Join(dir, "off.go"), []byte("//go:build !dev\n\npackage main\n"), 0644)

		any := captureGoTest(t, dir, devflow.TestOptions{NoCache: true})
		if any(hasDevTag) {
			t.Error("//go:build !dev is the default build, not a dev-only file: no -tags dev run")
		}
	})
}
