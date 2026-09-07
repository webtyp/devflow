package devflow_test

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"webtyp.com/command"
	"webtyp.com/devflow"
	gitmod "webtyp.com/git"
)

// captureGoTest swaps GoTestCmdFn and command.Exec for the duration of one
// Test() call, records every `go test` argument slice built, and restores the
// originals. The returned check reports whether any recorded slice satisfies a
// predicate.
func captureGoTest(t *testing.T, dir string, opts devflow.TestOptions) func(pred func([]string) bool) bool {
	t.Helper()

	git, _ := gitmod.NewGit()
	g, _ := devflow.NewGo(git)
	g.SetRootDir(dir)

	var runs [][]string

	origCmd := devflow.GoTestCmdFn
	origExec := command.Exec
	t.Cleanup(func() {
		devflow.GoTestCmdFn = origCmd
		command.Exec = origExec
	})

	devflow.GoTestCmdFn = func(ctx context.Context, dir, name string, args ...string) *exec.Cmd {
		runs = append(runs, append([]string(nil), args...))
		return exec.Command("true")
	}
	command.Exec = func(name string, args ...string) *exec.Cmd {
		if name == "go" {
			return exec.Command("true")
		}
		return origExec(name, args...)
	}

	// A mocked toolchain produces no cover profile, so Test returns an error
	// after building the command — which is all these cases assert on.
	_, _ = g.Test(opts)

	return func(pred func([]string) bool) bool {
		for _, r := range runs {
			if pred(r) {
				return true
			}
		}
		return false
	}
}

func hasArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

func hasArgWithPrefix(args []string, prefix string) bool {
	for _, a := range args {
		if strings.HasPrefix(a, prefix) {
			return true
		}
	}
	return false
}

func TestGo_TestOptions(t *testing.T) {
	dir, cleanup := testCreateGoModule("example.com/optstest")
	defer cleanup()

	t.Run("ZeroValueRunsWithRace", func(t *testing.T) {
		any := captureGoTest(t, dir, devflow.TestOptions{NoCache: true})
		if !any(func(a []string) bool { return hasArg(a, "-race") }) {
			t.Error("zero TestOptions must build a -race run")
		}
	})

	t.Run("SkipRaceOmitsRace", func(t *testing.T) {
		any := captureGoTest(t, dir, devflow.TestOptions{SkipRace: true, NoCache: true})
		if any(func(a []string) bool { return hasArg(a, "-race") }) {
			t.Error("SkipRace must omit -race from every run")
		}
	})

	t.Run("NoCacheKeepsCountOne", func(t *testing.T) {
		any := captureGoTest(t, dir, devflow.TestOptions{NoCache: true})
		if !any(func(a []string) bool { return hasArg(a, "-count=1") }) {
			t.Error("NoCache run must include -count=1")
		}
	})

	t.Run("TimeoutReachesCommand", func(t *testing.T) {
		any := captureGoTest(t, dir, devflow.TestOptions{Timeout: 90, NoCache: true})
		if !any(func(a []string) bool { return hasArgWithPrefix(a, "-timeout=900s") }) {
			t.Error("Timeout: 90 must reach the built command as -timeout=900s")
		}
	})

	t.Run("ArgsTakeCustomPath", func(t *testing.T) {
		any := captureGoTest(t, dir, devflow.TestOptions{Args: []string{"-run", "TestX"}})
		ok := any(func(a []string) bool {
			for i, v := range a {
				if v == "-run" && i+1 < len(a) && a[i+1] == "TestX" {
					return true
				}
			}
			return false
		})
		if !ok {
			t.Error("Args must be forwarded to the custom test path as -run TestX")
		}
	})
}
