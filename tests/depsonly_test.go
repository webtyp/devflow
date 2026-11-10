package devflow_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"webtyp.com/command"
	"webtyp.com/devflow"
	gitmod "webtyp.com/git"
)

// depsOnlyExec mocks go get/tidy/gotest and a dirty git status; vet decides what `go vet ./...` does, and real
// sends every other git command to the real git.
func depsOnlyExec(t *testing.T, depDir string, vet func() *exec.Cmd, real bool) *[][]string {
	t.Helper()
	var mu sync.Mutex
	var gitCalls [][]string
	orig := command.Exec
	t.Cleanup(func() { command.Exec = orig })
	command.Exec = func(name string, args ...string) *exec.Cmd {
		joined := strings.Join(args, " ")
		switch name {
		case "git":
			mu.Lock()
			gitCalls = append(gitCalls, args)
			mu.Unlock()
			switch {
			case joined == "rev-parse --show-toplevel":
				return exec.Command("echo", depDir)
			case strings.HasPrefix(joined, "status --porcelain"):
				return exec.Command("echo", "?? wip.go")
			case strings.HasPrefix(joined, "diff"):
				return exec.Command("false")
			case real && (args[0] == "worktree" || args[0] == "checkout"):
				return orig(name, args...)
			default:
				return exec.Command("true")
			}
		case "go":
			switch joined {
			case "version":
				return exec.Command("echo", "go version go1.20 linux/amd64")
			case "vet ./...":
				return vet()
			case "mod tidy":
				return exec.Command("bash", "-c", "echo '// tidy' >> go.mod")
			}
			return exec.Command("true")
		case "gotest":
			return exec.Command("echo", "tests ok")
		}
		return orig(name, args...)
	}
	return &gitCalls
}

func saw(calls [][]string, verb string) bool {
	for _, c := range calls {
		if len(c) > 0 && c[0] == verb {
			return true
		}
	}
	return false
}

func TestDepsOnly_RefusedWhenCleanTreeDoesNotCompile(t *testing.T) {
	depDir := filepath.Join(t.TempDir(), "myapp")
	os.MkdirAll(depDir, 0o755)
	os.WriteFile(filepath.Join(depDir, "go.mod"), []byte("module github.com/test/myapp\n\ngo 1.20\n\nrequire github.com/test/mylib v0.0.0\n"), 0o644)
	calls := depsOnlyExec(t, depDir, func() *exec.Cmd { return exec.Command("bash", "-c", "printf '# github.com/test/myapp\nmigrate/main.go:21:28: undefined: migrate.FlagPlan\n'; exit 1") }, false)

	g := newGoHandlerWithMockBackup(t, &MockGitClient{})
	var out []string
	g.SetConsoleOutput(func(s string) { out = append(out, s) })
	g.SetRetryConfig(time.Millisecond, 1)

	goModBefore, _ := os.ReadFile(filepath.Join(depDir, "go.mod"))
	_, err := g.UpdateDependentModule(depDir, []gitmod.DepBump{{ModulePath: "github.com/test/mylib", NewVersion: "v0.0.1"}}, "")
	if err == nil || !strings.Contains(err.Error(), "deps only would not compile without the uncommitted changes") {
		t.Fatalf("want the deps-only refusal, got %v", err)
	}
	if saw(*calls, "commit") || saw(*calls, "push") {
		t.Fatalf("a refused deps-only must not commit or push, git calls: %v", *calls)
	}
	// The restore is by content, never `git checkout --`: that would also discard the developer's own
	// uncommitted edits of go.mod (2026-11-10, webtyp/app lost its dependency bumps).
	for _, c := range *calls {
		if len(c) > 1 && c[0] == "checkout" {
			t.Errorf("go.mod/go.sum must be restored from a snapshot, not git checkout: %v", c)
		}
	}
	if goModAfter, _ := os.ReadFile(filepath.Join(depDir, "go.mod")); string(goModAfter) != string(goModBefore) {
		t.Errorf("go.mod must be byte-for-byte what it was before the attempt.\nbefore:\n%s\nafter:\n%s", goModBefore, goModAfter)
	}
	if len(out) == 0 || !strings.Contains(out[len(out)-1], "undefined: migrate.FlagPlan") {
		t.Errorf("the console line must carry the compile error, got %q", out)
	}
}

// TestDepsOnly_UncommittedGoModIsLeftUntouched: a dependent whose go.mod has the developer's own uncommitted
// edits (a bump, a replace) is skipped before anything is written. Mutating it would mix those edits into the
// automatic deps-only commit, and a failed gate would throw them away (2026-11-10, webtyp/app).
func TestDepsOnly_UncommittedGoModIsLeftUntouched(t *testing.T) {
	depDir := filepath.Join(t.TempDir(), "myapp")
	os.MkdirAll(depDir, 0o755)
	committed := "module github.com/test/myapp\n\ngo 1.20\n\nrequire github.com/test/mylib v0.0.0\n"
	os.WriteFile(filepath.Join(depDir, "go.mod"), []byte(committed), 0o644)
	for _, args := range [][]string{{"init", "-q"}, {"add", "go.mod"}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "a"}} {
		if out, err := exec.Command("git", append([]string{"-C", depDir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	developerEdit := committed + "\nrequire github.com/test/other v0.2.0 // the developer's own bump\n"
	os.WriteFile(filepath.Join(depDir, "go.mod"), []byte(developerEdit), 0o644)

	orig := command.Exec
	t.Cleanup(func() { command.Exec = orig })
	command.Exec = func(name string, args ...string) *exec.Cmd {
		joined := strings.Join(args, " ")
		switch name {
		case "go":
			switch joined {
			case "version":
				return exec.Command("echo", "go version go1.20 linux/amd64")
			case "mod tidy":
				return exec.Command("bash", "-c", "echo '// tidy' >> go.mod")
			}
			return exec.Command("true")
		case "gotest":
			return exec.Command("echo", "tests ok")
		}
		return orig(name, args...) // real git
	}

	g := newGoHandlerWithMockBackup(t, &MockGitClient{})
	var out []string
	g.SetConsoleOutput(func(s string) { out = append(out, s) })
	g.SetRetryConfig(time.Millisecond, 1)

	outcome, err := g.UpdateDependentModule(depDir, []gitmod.DepBump{{ModulePath: "github.com/test/mylib", NewVersion: "v0.0.1"}}, "")
	if err != nil {
		t.Fatalf("an uncommitted go.mod is a skip, not a failure: %v", err)
	}
	if outcome.Status != devflow.CascadeStatusSkipped || !strings.Contains(outcome.Reason, "go.mod") {
		t.Errorf("want a skip that names go.mod, got %+v", outcome)
	}
	if after, _ := os.ReadFile(filepath.Join(depDir, "go.mod")); string(after) != developerEdit {
		t.Errorf("the developer's go.mod must be untouched, got:\n%s", after)
	}
}

// Real git and real go vet: HEAD calls B, which exists only in an uncommitted file.
func TestDepsOnly_CleanTreeIsTheLastCommit(t *testing.T) {
	depDir := filepath.Join(t.TempDir(), "myapp")
	os.MkdirAll(depDir, 0o755)
	os.WriteFile(filepath.Join(depDir, "go.mod"), []byte("module github.com/test/myapp\n\ngo 1.20\n"), 0o644)
	os.WriteFile(filepath.Join(depDir, "a.go"), []byte("package myapp\n\nfunc A() int { return B() }\n"), 0o644)
	for _, args := range [][]string{{"init", "-q"}, {"add", "go.mod", "a.go"}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "a"}} {
		if out, err := exec.Command("git", append([]string{"-C", depDir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	os.WriteFile(filepath.Join(depDir, "b.go"), []byte("package myapp\n\nfunc B() int { return 1 }\n"), 0o644)

	orig := command.Exec
	calls := depsOnlyExec(t, depDir, func() *exec.Cmd { return nil }, true)
	command.Exec = func(prev func(string, ...string) *exec.Cmd) func(string, ...string) *exec.Cmd {
		return func(name string, args ...string) *exec.Cmd {
			if name == "go" && strings.Join(args, " ") == "vet ./..." {
				return orig(name, args...)
			}
			return prev(name, args...)
		}
	}(command.Exec)

	g := newGoHandlerWithMockBackup(t, &MockGitClient{})
	g.SetConsoleOutput(func(string) {})
	g.SetRetryConfig(time.Millisecond, 1)

	_, err := g.UpdateDependentModule(depDir, []gitmod.DepBump{{ModulePath: "github.com/test/mylib", NewVersion: "v0.0.1"}}, "")
	if err == nil || !strings.Contains(err.Error(), "deps only would not compile") {
		t.Fatalf("HEAD needs the uncommitted b.go: want the refusal, got %v", err)
	}
	if saw(*calls, "commit") {
		t.Fatalf("must not commit, git calls: %v", *calls)
	}
	if out, _ := exec.Command("git", "-C", depDir, "worktree", "list").Output(); strings.Count(string(out), "\n") != 1 {
		t.Errorf("the temporary worktree must be removed, got:\n%s", out)
	}
}

// TestDepsOnly_RelativeReplaceResolvesBesideTheRepo: a dependent whose go.mod has `replace … => ../lib` compiles in
// the temporary tree too — it sits next to the repository, so ../lib is the same sibling (2026-11-06: app and
// dbsync/tests were refused because the tree lived in /tmp).
func TestDepsOnly_RelativeReplaceResolvesBesideTheRepo(t *testing.T) {
	root := t.TempDir()
	lib := filepath.Join(root, "lib")
	os.MkdirAll(lib, 0o755)
	os.WriteFile(filepath.Join(lib, "go.mod"), []byte("module github.com/test/lib\n\ngo 1.20\n"), 0o644)
	os.WriteFile(filepath.Join(lib, "lib.go"), []byte("package lib\n\nfunc B() int { return 1 }\n"), 0o644)

	depDir := filepath.Join(root, "myapp")
	os.MkdirAll(depDir, 0o755)
	os.WriteFile(filepath.Join(depDir, "go.mod"), []byte("module github.com/test/myapp\n\ngo 1.20\n\nrequire github.com/test/lib v0.0.0\n\nreplace github.com/test/lib => ../lib\n"), 0o644)
	os.WriteFile(filepath.Join(depDir, "a.go"), []byte("package myapp\n\nimport \"github.com/test/lib\"\n\nfunc A() int { return lib.B() }\n"), 0o644)
	for _, args := range [][]string{{"init", "-q"}, {"add", "go.mod", "a.go"}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "a"}} {
		if out, err := exec.Command("git", append([]string{"-C", depDir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}

	orig := command.Exec
	depsOnlyExec(t, depDir, func() *exec.Cmd { return nil }, true)
	command.Exec = func(prev func(string, ...string) *exec.Cmd) func(string, ...string) *exec.Cmd {
		return func(name string, args ...string) *exec.Cmd {
			joined := strings.Join(args, " ")
			if (name == "go" && joined == "vet ./...") || (name == "git" && joined == "rev-parse --show-toplevel") {
				return orig(name, args...)
			}
			return prev(name, args...)
		}
	}(command.Exec)

	g := newGoHandlerWithMockBackup(t, &MockGitClient{})
	g.SetConsoleOutput(func(string) {})
	g.SetRetryConfig(time.Millisecond, 1)
	if _, err := g.UpdateDependentModule(depDir, []gitmod.DepBump{{ModulePath: "github.com/test/other", NewVersion: "v0.0.1"}}, ""); err != nil {
		t.Fatalf("HEAD compiles with ../lib beside it: want deps-only to proceed, got %v", err)
	}
	if entries, _ := os.ReadDir(root); len(entries) != 2 {
		t.Errorf("the temporary tree must be removed from beside the repo, got %d entries", len(entries))
	}
}
