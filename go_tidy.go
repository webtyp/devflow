package devflow

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	wpath "webtyp.com/filepath"
)

// NeedsGoModTidy reports whether compiler/go tool output indicates that
// go.mod or go.sum needs `go mod tidy` before retrying the build.
func NeedsGoModTidy(output string) bool {
	return strings.Contains(output, "updates to go.mod needed") ||
		strings.Contains(output, "missing go.sum entry")
}

// TidyModuleIfNeeded runs `go mod tidy` in the Go module owning dir when
// buildOutput reports a stale go.mod/go.sum. It walks upward from dir to find
// the nearest go.mod (so subdirectories of local replace modules resolve to
// their own module root), falling back to the handler's rootDir when dir is
// empty. Concurrent calls for the same module are serialized.
// Returns (true, nil) when tidy ran and succeeded so the caller can retry once.
func (g *GoModHandler) TidyModuleIfNeeded(dir, buildOutput string) (bool, error) {
	if !NeedsGoModTidy(buildOutput) {
		return false, nil
	}

	modDir := resolveOwningModuleDir(dir, g.rootDir)
	modMu := g.moduleTidyMutex(modDir)
	modMu.Lock()
	defer modMu.Unlock()

	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = modDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return false, execError("go mod tidy", err, out)
	}

	if sameCleanPath(modDir, g.rootDir) {
		g.Lines = nil
		g.Modified = false
	}
	if g.log != nil {
		g.log("ran go mod tidy in", wpath.Tilde(modDir))
	}
	return true, nil
}

func (g *GoModHandler) moduleTidyMutex(modDir string) *sync.Mutex {
	g.tidyMu.Lock()
	defer g.tidyMu.Unlock()
	if g.tidyLocks == nil {
		g.tidyLocks = make(map[string]*sync.Mutex)
	}
	mu, ok := g.tidyLocks[modDir]
	if !ok {
		mu = &sync.Mutex{}
		g.tidyLocks[modDir] = mu
	}
	return mu
}

// resolveOwningModuleDir walks upward from dir to find the directory containing
// go.mod. When dir is empty, fallbackRoot (or ".") is used.
func resolveOwningModuleDir(dir, fallbackRoot string) string {
	start := dir
	if start == "" {
		start = fallbackRoot
	}
	if start == "" {
		start = "."
	}

	abs, err := filepath.Abs(start)
	if err != nil {
		abs = filepath.Clean(start)
	}
	if info, err := os.Stat(abs); err == nil && !info.IsDir() {
		abs = filepath.Dir(abs)
	}

	cur := abs
	for {
		if _, err := os.Stat(filepath.Join(cur, "go.mod")); err == nil {
			return cur
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return abs
		}
		cur = parent
	}
}

func sameCleanPath(a, b string) bool {
	if b == "" {
		b = "."
	}
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return absA == absB
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
