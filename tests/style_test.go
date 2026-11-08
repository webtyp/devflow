package devflow_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"webtyp.com/command"
	"webtyp.com/devflow"
	gitmod "webtyp.com/git"
)

// runStyle writes src as main.go of a temp module and runs the full suite with the
// toolchain mocked except for the `go vet -vettool=...` call, which runs for real.
// It returns the summary line and everything printed to the console.
func runStyle(t *testing.T, src string) (summary, console string) {
	t.Helper()
	dir, cleanup := testCreateGoModule("example.com/styletest")
	defer cleanup()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	git, _ := gitmod.NewGit()
	g, _ := devflow.NewGo(git)
	g.SetRootDir(dir)
	var out strings.Builder
	g.SetConsoleOutput(func(s string) { out.WriteString(s + "\n") })

	origCmd, origExec := devflow.GoTestCmdFn, command.Exec
	t.Cleanup(func() { devflow.GoTestCmdFn, command.Exec = origCmd, origExec })
	devflow.GoTestCmdFn = func(ctx context.Context, d, name string, args ...string) *exec.Cmd {
		if len(args) > 0 && args[0] == "vet" && hasArgWithPrefix(args, "-vettool=") {
			c := exec.CommandContext(ctx, name, args...)
			c.Dir = d
			return c
		}
		return exec.Command("true")
	}
	command.Exec = func(name string, args ...string) *exec.Cmd {
		if name == "go" {
			return exec.Command("true")
		}
		return origExec(name, args...)
	}

	summary, _ = g.Test(devflow.TestOptions{NoCache: true})
	return summary, out.String()
}

func TestStyle_TaggedSwitchFails(t *testing.T) {
	src := `package main

type St string

const (
	A St = "a"
	B St = "b"
	C St = "c"
)

func H(s St) int {
	if s == A {
		return 1
	} else if s == B {
		return 2
	} else if s == C {
		return 3
	}
	return 0
}

func main() {}
`
	summary, console := runStyle(t, src)
	if !strings.Contains(summary, "style ❌") {
		t.Fatalf("summary %q lacks style ❌", summary)
	}
	if !strings.Contains(console, "could use tagged switch on s") {
		t.Fatalf("console %q lacks the QF1003 message", console)
	}
}

func TestStyle_SimplifyFails(t *testing.T) {
	src := "package main\n\nfunc B(ok bool) bool {\n\tif ok {\n\t\treturn true\n\t}\n\treturn false\n}\n\nfunc main() {}\n"
	summary, console := runStyle(t, src)
	if !strings.Contains(summary, "style ❌") || !strings.Contains(console, "should use 'return ok'") {
		t.Fatalf("summary %q console %q", summary, console)
	}
}

func TestStyle_BugFails(t *testing.T) {
	src := "package main\n\nfunc A(a int) bool { return a == a }\n\nfunc main() {}\n"
	summary, console := runStyle(t, src)
	if !strings.Contains(summary, "style ❌") || !strings.Contains(console, "identical expressions") {
		t.Fatalf("summary %q console %q", summary, console)
	}
}

func TestStyle_CleanPasses(t *testing.T) {
	summary, _ := runStyle(t, "package main\n\nfunc main() {}\n")
	if !strings.Contains(summary, "style ✅") {
		t.Fatalf("summary %q lacks style ✅", summary)
	}
}

func TestStyle_NoPackagesPasses(t *testing.T) {
	src := "//go:build never\n\npackage main\n\nfunc main() {}\n"
	summary, _ := runStyle(t, src)
	if !strings.Contains(summary, "style ✅") {
		t.Fatalf("summary %q lacks style ✅", summary)
	}
}
