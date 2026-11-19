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
// toolchain mocked except for the style & unused checks, which run for real.
// It returns the summary line and everything printed to the console.
func runStyle(t *testing.T, src string) (summary, console string) {
	t.Helper()
	return runStyleFiles(t, map[string]string{"main.go": src})
}

func runStyleFiles(t *testing.T, files map[string]string) (summary, console string) {
	t.Helper()
	dir, cleanup := testCreateGoModule("example.com/styletest")
	defer cleanup()
	for name, src := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0644); err != nil {
			t.Fatal(err)
		}
	}

	git, _ := gitmod.NewGit()
	g, _ := devflow.NewGo(git)
	g.SetRootDir(dir)
	var out strings.Builder
	g.SetConsoleOutput(func(s string) {
		out.WriteString(s)
		out.WriteByte('\n')
	})

	origCmd, origExec := devflow.GoTestCmdFn, command.Exec
	t.Cleanup(func() { devflow.GoTestCmdFn, command.Exec = origCmd, origExec })
	devflow.GoTestCmdFn = func(ctx context.Context, d, name string, args ...string) *exec.Cmd {
		isVetTool := len(args) > 0 && args[0] == "vet" && hasArgWithPrefix(args, "-vettool=")
		isUnusedTool := name != "go" && len(args) > 0 && args[len(args)-1] == "./..."
		if isVetTool || isUnusedTool {
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

// A finding repeated in many places is printed once, with every location after it: the reader fixes one
// kind of problem at a time and sees how many places it has (2026-11-10: 18 lines of the same message).
func TestStyle_RepeatedFindingIsGroupedByMessage(t *testing.T) {
	src := `package main

type Cfg struct{ Out string }
type C struct{ Cfg }

func (c *C) A() string { return c.Cfg.Out }
func (c *C) B() string { return c.Cfg.Out + "b" }
func (c *C) D() string { return c.Cfg.Out + "d" }

func main() {}
`
	summary, console := runStyle(t, src)
	if !strings.Contains(summary, "style ❌") {
		t.Fatalf("summary %q lacks style ❌", summary)
	}
	const msg = `could remove embedded field "Cfg" from selector`
	if n := strings.Count(console, msg); n != 1 {
		t.Fatalf("the message must appear once, appeared %d times:\n%s", n, console)
	}
	want := msg + " in:\n  main.go:6:35, 7:35, 8:35"
	if !strings.Contains(console, want) {
		t.Fatalf("want the grouped form\n%s\ngot:\n%s", want, console)
	}
}

func TestStyle_RepeatedFindingAcrossFilesSeparatesByFile(t *testing.T) {
	summary, console := runStyleFiles(t, map[string]string{
		"a.go": "package main\n\ntype Cfg struct{ Out string }\ntype C struct{ Cfg }\n\nfunc (c *C) A() string { return c.Cfg.Out }\nfunc (c *C) B() string { return c.Cfg.Out + \"b\" }\n",
		"b.go": "package main\n\nfunc (c *C) D() string { return c.Cfg.Out + \"d\" }\n",
	})
	if !strings.Contains(summary, "style ❌") {
		t.Fatalf("summary %q lacks style ❌", summary)
	}
	const want = `could remove embedded field "Cfg" from selector in:` + "\n  a.go:6:35, 7:35; b.go:3:35"
	if !strings.Contains(console, want) {
		t.Fatalf("want\n%s\ngot:\n%s", want, console)
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

func TestStyle_UnusedFails(t *testing.T) {
	src := `package main

type srv struct{}

func (s *srv) dead() {}

func main() { _ = &srv{} }
`
	summary, console := runStyle(t, src)
	if !strings.Contains(summary, "style ❌") || !strings.Contains(console, "is unused") {
		t.Fatalf("summary %q console %q", summary, console)
	}
}

func TestStyle_UsedOnlyInTestPasses(t *testing.T) {
	summary, console := runStyleFiles(t, map[string]string{
		"main.go":     "package main\n\nfunc helper() int { return 42 }\n\nfunc main() {}\n",
		"lib_test.go": "package main\n\nimport \"testing\"\n\nfunc TestHelper(t *testing.T) { _ = helper() }\n",
	})
	if !strings.Contains(summary, "style ✅") {
		t.Fatalf("summary %q console %q", summary, console)
	}
}

func TestStyle_WriteStringConcatFails(t *testing.T) {
	src := `package main

import (
	"bytes"
	"strings"
)

func Build(s string) string {
	var b strings.Builder
	b.WriteString("\t" + s + "\n")
	var buf bytes.Buffer
	buf.WriteString(s + "\n")
	return b.String() + buf.String()
}

func main() {}
`
	summary, console := runStyle(t, src)
	if !strings.Contains(summary, "style ❌") {
		t.Fatalf("summary %q lacks style ❌", summary)
	}
	const want = "inefficient string concatenation in call to WriteString"
	if !strings.Contains(console, want) {
		t.Fatalf("console %q lacks %q", console, want)
	}
}

func TestStyle_WriteStringConstPasses(t *testing.T) {
	src := `package main

import "strings"

const prefix = "a"

func Build() string {
	var b strings.Builder
	b.WriteString(prefix + "b")
	return b.String()
}

func main() {}
`
	summary, console := runStyle(t, src)
	if !strings.Contains(summary, "style ✅") {
		t.Fatalf("summary %q console %q", summary, console)
	}
}
