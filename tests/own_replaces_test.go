package devflow_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"webtyp.com/command"
)

// A published module must not carry local replaces of other ecosystem modules: its go.mod would
// tag a state that only builds on this machine. gopush reconciles them before it tags.

// ownReplacesWorkspace builds lib (a git repo with a bare remote, tagged v1.2.3 at HEAD) and app, whose go.mod
// requires lib through a local replace. It returns the app dir and the lib dir.
func ownReplacesWorkspace(t *testing.T) (app, lib string) {
	t.Helper()
	root := t.TempDir()
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	remote := filepath.Join(root, "lib.git")
	lib = filepath.Join(root, "lib")
	app = filepath.Join(root, "app")
	for _, d := range []string{remote, lib, app} {
		os.MkdirAll(d, 0o755)
	}
	run(remote, "init", "--bare", "-b", "main")
	run(lib, "init", "-b", "main")
	run(lib, "config", "user.name", "Test")
	run(lib, "config", "user.email", "t@t.com")
	os.WriteFile(filepath.Join(lib, "go.mod"), []byte("module github.com/test/lib\n\ngo 1.20\n"), 0o644)
	os.WriteFile(filepath.Join(lib, "lib.go"), []byte("package lib\n"), 0o644)
	run(lib, "add", ".")
	run(lib, "commit", "-m", "init")
	run(lib, "tag", "v1.2.3")
	run(lib, "remote", "add", "origin", remote)
	run(lib, "push", "origin", "main", "--tags")

	os.WriteFile(filepath.Join(app, "go.mod"), []byte("module github.com/test/app\n\ngo 1.20\n\n"+
		"require github.com/test/lib v0.0.0-00010101000000-000000000000\n\n"+
		"replace github.com/test/lib => ../lib\n"), 0o644)
	os.WriteFile(filepath.Join(app, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644)
	return app, lib
}

// goCommandsMocked stubs the go toolchain steps gopush runs (verify, tidy, vet): the test is about go.mod, not the compiler.
// `go mod edit` stays real because that is how go.mod is rewritten.
func goCommandsMocked(t *testing.T) (tidyCalls *int) {
	t.Helper()
	orig := command.Exec
	t.Cleanup(func() { command.Exec = orig })
	n := 0
	command.Exec = func(name string, args ...string) *exec.Cmd {
		joined := strings.Join(args, " ")
		if name == "go" {
			switch {
			case strings.HasPrefix(joined, "mod edit"):
				return orig(name, args...)
			case joined == "version":
				return exec.Command("echo", "go version go1.20 linux/amd64")
			case joined == "mod tidy":
				n++
				return exec.Command("true")
			}
			return exec.Command("true")
		}
		return orig(name, args...)
	}
	return &n
}

func TestOwnReplaces_PublishedModuleIsPinnedAndReplaceDropped(t *testing.T) {
	app, _ := ownReplacesWorkspace(t)
	tidy := goCommandsMocked(t)
	defer testChdir(t, app)()
	g := newGoHandlerWithMockBackup(t, &MockGitClient{latestTag: "v0.0.0"})

	if _, err := g.Push("fix", "v0.0.1", true, true, true, true, false, false, ""); err != nil {
		t.Fatalf("Push: %v", err)
	}
	mod, _ := os.ReadFile("go.mod")
	if strings.Contains(string(mod), "replace github.com/test/lib") {
		t.Errorf("the replace of a published module must be dropped before tagging:\n%s", mod)
	}
	if !strings.Contains(string(mod), "github.com/test/lib v1.2.3") {
		t.Errorf("the module must be required at its published tag v1.2.3:\n%s", mod)
	}
	if *tidy == 0 {
		t.Error("go mod tidy must run so go.sum matches the new requirement")
	}
}

func TestOwnReplaces_UnpublishedModuleRefusesToTag(t *testing.T) {
	app, lib := ownReplacesWorkspace(t)
	os.WriteFile(filepath.Join(lib, "wip.go"), []byte("package lib\n"), 0o644) // uncommitted work in lib
	goCommandsMocked(t)
	defer testChdir(t, app)()
	g := newGoHandlerWithMockBackup(t, &MockGitClient{latestTag: "v0.0.0"})

	_, err := g.Push("feat", "v0.0.1", true, true, true, true, false, false, "")
	if err == nil || !strings.Contains(err.Error(), "github.com/test/lib") {
		t.Fatalf("a replace of a module with unpublished work must refuse to tag, naming it; got %v", err)
	}
	mod, _ := os.ReadFile("go.mod")
	if !strings.Contains(string(mod), "replace github.com/test/lib") {
		t.Error("a refused publish must leave go.mod untouched")
	}
}

func TestOwnReplaces_NoTagMeansNoCheck(t *testing.T) {
	app, lib := ownReplacesWorkspace(t)
	os.WriteFile(filepath.Join(lib, "wip.go"), []byte("package lib\n"), 0o644)
	goCommandsMocked(t)
	defer testChdir(t, app)()
	g := newGoHandlerWithMockBackup(t, &MockGitClient{latestTag: "v0.0.0"})

	// skipTag = deps-only commits: they may carry replaces until their module is published (skill codejob)
	if _, err := g.Push("wip", "", true, true, true, true, true, false, ""); err != nil {
		t.Fatalf("a push that creates no tag may carry replaces: %v", err)
	}
}

func TestOwnReplaces_KeepReplacesPublishesKnowingIt(t *testing.T) {
	app, lib := ownReplacesWorkspace(t)
	os.WriteFile(filepath.Join(lib, "wip.go"), []byte("package lib\n"), 0o644)
	goCommandsMocked(t)
	defer testChdir(t, app)()
	g := newGoHandlerWithMockBackup(t, &MockGitClient{latestTag: "v0.0.0"})
	g.SetKeepReplaces(true)

	if _, err := g.Push("feat", "v0.0.1", true, true, true, true, false, false, ""); err != nil {
		t.Fatalf("with KeepReplaces the publish proceeds: %v", err)
	}
	mod, _ := os.ReadFile("go.mod")
	if !strings.Contains(string(mod), "replace github.com/test/lib") {
		t.Error("KeepReplaces must leave the replace in place")
	}
}

func TestOwnReplaces_SelfReferenceIsKept(t *testing.T) {
	app, _ := ownReplacesWorkspace(t)
	goCommandsMocked(t)
	// the subpackage case: a go.mod that points at its own parent module with "=> ./"
	os.WriteFile(filepath.Join(app, "go.mod"), []byte("module github.com/test/app\n\ngo 1.20\n\nreplace github.com/test/app => ./\n"), 0o644)
	defer testChdir(t, app)()
	g := newGoHandlerWithMockBackup(t, &MockGitClient{latestTag: "v0.0.0"})

	if _, err := g.Push("fix", "v0.0.1", true, true, true, true, false, false, ""); err != nil {
		t.Fatalf("a self-referencing replace is not a replace of another module: %v", err)
	}
	mod, _ := os.ReadFile("go.mod")
	if !strings.Contains(string(mod), "replace github.com/test/app => ./") {
		t.Errorf("a self-referencing replace must survive:\n%s", mod)
	}
}
