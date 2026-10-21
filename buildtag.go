package devflow

import (
	"bufio"
	"bytes"
	"context"
	"go/build/constraint"
	"os"
	"path/filepath"
	"strings"
)

// devTag is the build tag the webtyp dev environment compiles with (client and
// server). Code gated on it — debug-only features such as webtyp/auth's
// DEV_AUTOLOGIN — is invisible to a default `go test ./...`.
const devTag = "dev"

// browserTag is the destination tag of the browser binary: only the client
// compiler passes it (-tags browser). Files gated on it run only in js/wasm,
// in headless Chrome.
const browserTag = "browser"

// hasTaggedFiles reports whether any .go file under root (submodules
// included) carries a build constraint that only holds with the given tag set.
// `//go:build !tag` is the default build and does not count.
func hasTaggedFiles(root, tag string) bool {
	found := false
	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if found {
			return filepath.SkipAll
		}
		if err != nil {
			return nil // unreadable entry: skip it, keep walking
		}
		if d.IsDir() {
			name := d.Name()
			if path != root && (strings.HasPrefix(name, ".") || name == "vendor" || name == "node_modules" || name == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") && fileNeedsTag(path, tag) {
			found = true
		}
		return nil
	})
	return found
}

// fileNeedsTag reads the build constraint at the top of a Go file and
// reports whether setting the given tag can switch it from excluded to included.
// Every other tag is tried both all-on and all-off, so `tag && wasm` counts
// and `!tag` does not.
func fileNeedsTag(path, tagToFind string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || (strings.HasPrefix(line, "//") && !constraint.IsGoBuild(line)) {
			continue
		}
		if !constraint.IsGoBuild(line) {
			return false // reached code: no build constraint
		}
		expr, err := constraint.Parse(line)
		if err != nil {
			return false
		}
		for _, others := range []bool{false, true} {
			eval := func(hasTargetTag bool) bool {
				return expr.Eval(func(tag string) bool {
					if tag == tagToFind {
						return hasTargetTag
					}
					return others
				})
			}
			if eval(true) && !eval(false) {
				return true
			}
		}
		return false
	}
	return false
}

// runDevPass runs the stdlib tests of the root module and of every submodule
// once more with -tags dev. It returns the combined output and whether any run
// failed. The pass skips -race and coverage: its only job is to execute code
// the default run cannot see.
func (g *Go) runDevPass(timeoutFlag string) (string, bool) {
	dirs := append([]string{g.rootDir}, findSubModuleDirs(g.rootDir)...)
	var out bytes.Buffer
	failed := false
	for _, dir := range dirs {
		cmd := GoTestCmdFn(context.Background(), dir, "go", "test", "-tags", devTag, "-count=1", timeoutFlag, "./...")
		cmd.Stdout = &out
		cmd.Stderr = &out
		if err := cmd.Run(); err != nil {
			failed = true
		}
	}
	return out.String(), failed
}
