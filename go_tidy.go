package devflow

import (
	"os/exec"
	"strings"

	wpath "webtyp.com/filepath"
)

// NeedsGoModTidy reports whether compiler/go tool output indicates that
// go.mod or go.sum needs `go mod tidy` before retrying the build.
func NeedsGoModTidy(output string) bool {
	return strings.Contains(output, "updates to go.mod needed") ||
		strings.Contains(output, "missing go.sum entry")
}

// TidyIfNeeded runs `go mod tidy` in the module directory when buildOutput
// reports a stale go.mod/go.sum. Returns (true, nil) when tidy ran and
// succeeded so the caller can retry the failed build once.
func (g *GoModHandler) TidyIfNeeded(buildOutput string) (bool, error) {
	if !NeedsGoModTidy(buildOutput) {
		return false, nil
	}
	dir := g.rootDir
	if dir == "" {
		dir = "."
	}
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return false, execError("go mod tidy", err, out)
	}
	g.Lines = nil
	g.Modified = false
	if g.log != nil {
		g.log("ran go mod tidy in", wpath.Tilde(dir))
	}
	return true, nil
}

func execError(op string, err error, out []byte) error {
	msg := strings.TrimSpace(string(out))
	if msg == "" {
		return &tidyError{op: op, err: err}
	}
	return &tidyError{op: op, err: err, detail: msg}
}

type tidyError struct {
	op     string
	err    error
	detail string
}

func (e *tidyError) Error() string {
	if e.detail != "" {
		return e.op + ": " + e.detail
	}
	return e.op + ": " + e.err.Error()
}

func (e *tidyError) Unwrap() error { return e.err }
