package devflow_test

import (
	"os"
	"path/filepath"
	"testing"
	"webtyp.com/devflow"
)

func hasArgs(want ...string) func([]string) bool {
	return func(a []string) bool {
		for _, w := range want {
			found := false
			for _, s := range a {
				if s == w {
					found = true
				}
			}
			if !found {
				return false
			}
		}
		return true
	}
}

func TestBrowserAndWorkerPasses(t *testing.T) {
	// D1
	t.Run("D1: view.go with browser tag", func(t *testing.T) {
		dir, cleanup := testCreateGoModule("testd1")
		defer cleanup()

		os.WriteFile(filepath.Join(dir, "view.go"), []byte("//go:build browser\n\npackage main\n"), 0644)

		check := captureGoTest(t, dir, devflow.TestOptions{NoCache: true})
		if !check(hasArgs("test", "-tags=browser")) {
			t.Error("D1: missing expected args for test -tags=browser")
		}
	})

	// D2
	t.Run("D2: no file with browser tag", func(t *testing.T) {
		dir, cleanup := testCreateGoModule("testd2")
		defer cleanup()

		check := captureGoTest(t, dir, devflow.TestOptions{NoCache: true})
		if check(hasArgs("-tags=browser")) {
			t.Error("D2: found unexpected -tags=browser")
		}
	})

	// D3
	t.Run("D3: srv.go with !browser tag", func(t *testing.T) {
		dir, cleanup := testCreateGoModule("testd3")
		defer cleanup()

		os.WriteFile(filepath.Join(dir, "srv.go"), []byte("//go:build !browser\n\npackage main\n"), 0644)

		check := captureGoTest(t, dir, devflow.TestOptions{NoCache: true})
		if check(hasArgs("-tags=browser")) {
			t.Error("D3: found unexpected -tags=browser")
		}
	})

	// D4
	t.Run("D4: web/edge.go with wasm && !browser", func(t *testing.T) {
		dir, cleanup := testCreateGoModule("testd4")
		defer cleanup()

		os.MkdirAll(filepath.Join(dir, "web"), 0755)
		os.WriteFile(filepath.Join(dir, "web", "edge.go"), []byte("//go:build wasm && !browser\n\npackage main\n"), 0644)

		check := captureGoTest(t, dir, devflow.TestOptions{NoCache: true})
		if !check(hasArgs("vet", "./web")) {
			t.Error("D4: missing expected args for vet ./web")
		}
		if check(func(a []string) bool {
			return hasArgs("vet", "./web")(a) && hasArgs("-tags=")(a)
		}) {
			t.Error("D4: found unexpected -tags= in vet run")
		}
	})

	// D5
	t.Run("D5: web/browser.go with browser tag", func(t *testing.T) {
		dir, cleanup := testCreateGoModule("testd5")
		defer cleanup()

		os.MkdirAll(filepath.Join(dir, "web"), 0755)
		os.WriteFile(filepath.Join(dir, "web", "browser.go"), []byte("//go:build browser\n\npackage main\n"), 0644)

		check := captureGoTest(t, dir, devflow.TestOptions{NoCache: true})
		if !check(hasArgs("vet", "-tags=browser", "./web")) {
			t.Error("D5: missing expected args for vet -tags=browser ./web")
		}
	})

	// D6
	t.Run("D6: no web/edge.go", func(t *testing.T) {
		dir, cleanup := testCreateGoModule("testd6")
		defer cleanup()

		check := captureGoTest(t, dir, devflow.TestOptions{NoCache: true})
		if check(func(a []string) bool {
			return len(a) > 0 && a[0] == "vet" && hasArgs("./web")(a)
		}) {
			t.Error("D6: found unexpected vet ./web")
		}
	})

	// D7
	t.Run("D7: D1 setup with RunAll", func(t *testing.T) {
		dir, cleanup := testCreateGoModule("testd7")
		defer cleanup()

		os.WriteFile(filepath.Join(dir, "view.go"), []byte("//go:build browser\n\npackage main\n"), 0644)

		check := captureGoTest(t, dir, devflow.TestOptions{NoCache: true, RunAll: true})
		if !check(hasArgs("-tags=integration,browser")) {
			t.Error("D7: missing expected args for -tags=integration,browser")
		}
	})

	// D8
	t.Run("D8: D1 setup with run TestX", func(t *testing.T) {
		dir, cleanup := testCreateGoModule("testd8")
		defer cleanup()

		os.WriteFile(filepath.Join(dir, "view.go"), []byte("//go:build browser\n\npackage main\n"), 0644)

		check := captureGoTest(t, dir, devflow.TestOptions{Args: []string{"-run", "TestX"}})
		if !check(hasArgs("-tags=browser", "-run", "TestX")) {
			t.Error("D8: missing expected args for -tags=browser -run TestX")
		}
	})
}
