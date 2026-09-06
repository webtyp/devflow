package devflow_test

// Tests for the dependent-update guards (PLAN.md Fase 1):
//   - A dirty working tree (changes beyond go.mod/go.sum) must NEVER be swept
//     by `git add .`: only go.mod+go.sum are committed, no tag is created.
//     This is the intuitive, zero-config protection: a repo with WIP is dirty
//     by definition, so it is protected without the developer doing anything.
//   - New Git primitives: StatusPorcelain, CommitPaths, DiffShortStat.
//   - New helper: WorkTreeDirtyBeyond.
// These tests define the target contract; the implementation must make them
// pass WITHOUT modifying the expectations.

import (
	"webtyp.com/command"
	gitmod "webtyp.com/git"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"webtyp.com/devflow"
)

// testInitRepoWithCommit creates a real git repo with an initial commit
// containing go.mod, go.sum and app.go.
func testInitRepoWithCommit(t *testing.T) (dir string, git *gitmod.Git) {
	t.Helper()
	dir, cleanup := testCreateGitRepo()
	t.Cleanup(cleanup)

	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module github.com/test/app\n\ngo 1.20\n"), 0644)
	os.WriteFile(filepath.Join(dir, "go.sum"), []byte(""), 0644)
	os.WriteFile(filepath.Join(dir, "app.go"), []byte("package app\n"), 0644)
	exec.Command("git", "-C", dir, "add", ".").Run()
	exec.Command("git", "-C", dir, "commit", "-m", "initial").Run()

	git, err := gitmod.NewGit()
	if err != nil {
		t.Fatalf("NewGit: %v", err)
	}
	git.SetRootDir(dir)
	return dir, git
}

func TestGitStatusPorcelain(t *testing.T) {
	dir, git := testInitRepoWithCommit(t)

	out, err := git.StatusPorcelain()
	if err != nil {
		t.Fatalf("StatusPorcelain: %v", err)
	}
	if strings.TrimSpace(out) != "" {
		t.Errorf("expected empty porcelain on clean tree, got: %q", out)
	}

	os.WriteFile(filepath.Join(dir, "wip.go"), []byte("package app\n"), 0644)
	out, err = git.StatusPorcelain()
	if err != nil {
		t.Fatalf("StatusPorcelain: %v", err)
	}
	if !strings.Contains(out, "wip.go") {
		t.Errorf("expected porcelain to list wip.go, got: %q", out)
	}
}

func TestWorkTreeDirtyBeyond(t *testing.T) {
	dir, git := testInitRepoWithCommit(t)

	// Clean tree → not dirty
	dirty, err := gitmod.WorkTreeDirtyBeyond(git, "go.mod", "go.sum")
	if err != nil {
		t.Fatalf("WorkTreeDirtyBeyond: %v", err)
	}
	if dirty {
		t.Error("clean tree must not be dirty")
	}

	// Only go.mod/go.sum modified → NOT dirty beyond allowed
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module github.com/test/app\n\ngo 1.21\n"), 0644)
	os.WriteFile(filepath.Join(dir, "go.sum"), []byte("x\n"), 0644)
	dirty, err = gitmod.WorkTreeDirtyBeyond(git, "go.mod", "go.sum")
	if err != nil {
		t.Fatalf("WorkTreeDirtyBeyond: %v", err)
	}
	if dirty {
		t.Error("changes limited to go.mod/go.sum must not count as dirty")
	}

	// .env and .gitignore are always ignored (same rule as HasPendingChanges)
	os.WriteFile(filepath.Join(dir, ".env"), []byte("KEY=1\n"), 0644)
	os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(".env\n"), 0644)
	dirty, err = gitmod.WorkTreeDirtyBeyond(git, "go.mod", "go.sum")
	if err != nil {
		t.Fatalf("WorkTreeDirtyBeyond: %v", err)
	}
	if dirty {
		t.Error(".env/.gitignore changes must not count as dirty")
	}

	// An unrelated WIP file → dirty
	os.WriteFile(filepath.Join(dir, "wip.go"), []byte("package app\n"), 0644)
	dirty, err = gitmod.WorkTreeDirtyBeyond(git, "go.mod", "go.sum")
	if err != nil {
		t.Fatalf("WorkTreeDirtyBeyond: %v", err)
	}
	if !dirty {
		t.Error("untracked wip.go must count as dirty beyond go.mod/go.sum")
	}
}

func TestGitCommitPaths_LeavesWIPUntouched(t *testing.T) {
	dir, git := testInitRepoWithCommit(t)

	// Modify go.mod, go.sum AND app.go (app.go = the developer's WIP)
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module github.com/test/app\n\ngo 1.21\n"), 0644)
	os.WriteFile(filepath.Join(dir, "go.sum"), []byte("y\n"), 0644)
	os.WriteFile(filepath.Join(dir, "app.go"), []byte("package app\n\n// WIP\n"), 0644)

	committed, err := git.CommitPaths("deps: update mylib to v0.0.2", "go.mod", "go.sum")
	if err != nil {
		t.Fatalf("CommitPaths: %v", err)
	}
	if !committed {
		t.Fatal("expected a commit to be created")
	}

	// The WIP file must remain uncommitted
	status, _ := exec.Command("git", "-C", dir, "status", "--porcelain").Output()
	if !strings.Contains(string(status), "app.go") {
		t.Errorf("app.go (WIP) must remain uncommitted, status: %q", string(status))
	}

	// The commit must contain ONLY go.mod and go.sum
	show, _ := exec.Command("git", "-C", dir, "show", "--stat", "--name-only", "--format=", "HEAD").Output()
	files := strings.TrimSpace(string(show))
	if !strings.Contains(files, "go.mod") || !strings.Contains(files, "go.sum") {
		t.Errorf("commit must contain go.mod and go.sum, got: %q", files)
	}
	if strings.Contains(files, "app.go") {
		t.Errorf("commit must NOT contain app.go (WIP swept!), got: %q", files)
	}

	// Nothing staged to commit → committed=false, no error
	committed, err = git.CommitPaths("deps: noop", "go.mod", "go.sum")
	if err != nil {
		t.Fatalf("CommitPaths noop: %v", err)
	}
	if committed {
		t.Error("expected committed=false when paths have no changes")
	}
}

func TestGitDiffShortStat(t *testing.T) {
	dir, git := testInitRepoWithCommit(t)

	// Clean tree → empty
	out, err := git.DiffShortStat()
	if err != nil {
		t.Fatalf("DiffShortStat: %v", err)
	}
	if strings.TrimSpace(out) != "" {
		t.Errorf("expected empty shortstat on clean tree, got: %q", out)
	}

	// UNSTAGED modification must be counted. The shortstat body is computed
	// BEFORE the workflow stages anything (`git add` happens later, inside
	// the push), so the contract is `git diff HEAD --shortstat` — changes vs
	// HEAD whether staged or not. A `--cached`-only implementation would
	// always return empty at message-build time and is therefore wrong.
	os.WriteFile(filepath.Join(dir, "app.go"), []byte("package app\n\nvar X = 1\n"), 0644)
	out, err = git.DiffShortStat()
	if err != nil {
		t.Fatalf("DiffShortStat: %v", err)
	}
	if !strings.Contains(out, "1 file changed") {
		t.Errorf("expected shortstat to report '1 file changed' for an unstaged change, got: %q", out)
	}

	// Staging the same change must not alter the result
	exec.Command("git", "-C", dir, "add", "app.go").Run()
	out, err = git.DiffShortStat()
	if err != nil {
		t.Fatalf("DiffShortStat: %v", err)
	}
	if !strings.Contains(out, "1 file changed") {
		t.Errorf("expected shortstat to report '1 file changed' after staging, got: %q", out)
	}
}

// TestUpdateDependentModule_DirtyTreeCommitsOnlyGoModAndSum is THE core guard:
// a dependent with unrelated WIP must never have its whole tree committed.
// Expected behavior: bump + tests, then commit ONLY go.mod+go.sum with the
// deps message (including the propagated root cause), push WITHOUT tag,
// and never run `git add .`.
func TestUpdateDependentModule_DirtyTreeCommitsOnlyGoModAndSum(t *testing.T) {
	tmp := t.TempDir()
	depDir := filepath.Join(tmp, "myapp")
	os.MkdirAll(depDir, 0755)
	gomod := "module github.com/test/myapp\n\ngo 1.20\n\nrequire github.com/test/mylib v0.0.0\n"
	os.WriteFile(filepath.Join(depDir, "go.mod"), []byte(gomod), 0644)
	os.WriteFile(filepath.Join(depDir, "wip.go"), []byte("package myapp // WIP\n"), 0644)

	var mu sync.Mutex
	var gitCalls [][]string
	originalExec := command.Exec
	defer func() { command.Exec = originalExec }()
	command.Exec = func(name string, args ...string) *exec.Cmd {
		joined := strings.Join(args, " ")
		switch name {
		case "git":
			mu.Lock()
			gitCalls = append(gitCalls, args)
			mu.Unlock()
			switch {
			case strings.HasPrefix(joined, "status --porcelain"):
				// Simulates the developer's WIP in the dependent
				return exec.Command("echo", "?? wip.go")
			case strings.HasPrefix(joined, "diff"):
				// Any staged-changes probe (diff-index, diff --cached --quiet…)
				// must report "there ARE changes to commit" (exit 1)
				return exec.Command("false")
			default:
				return exec.Command("true")
			}
		case "go":
			if joined == "version" {
				return exec.Command("echo", "go version go1.20 linux/amd64")
			}
			return exec.Command("true") // get / tidy / generate / list
		case "gotest":
			return exec.Command("echo", "tests ok")
		}
		return originalExec(name, args...)
	}

	mockGit := &MockGitClient{}
	g := newGoHandlerWithMockBackup(t, mockGit)
	g.SetConsoleOutput(func(string) {})
	g.SetRetryConfig(time.Millisecond, 1)

	outcome, err := g.UpdateDependentModule(depDir, []gitmod.DepBump{{ModulePath: "github.com/test/mylib", NewVersion: "v0.0.1"}}, "feat: nueva API de rutas")
	if err != nil {
		t.Fatalf("dirty-tree path must succeed as deps-only, got error: %v", err)
	}
	if outcome.Status != devflow.CascadeStatusDepsOnly {
		t.Errorf("result must report 'deps only', got: %+v", outcome)
	}

	mu.Lock()
	defer mu.Unlock()

	var sawAddPathspec, sawTagCreation, sawPush bool
	var commitMsg string
	for _, args := range gitCalls {
		if len(args) == 0 {
			continue
		}
		switch args[0] {
		case "add":
			// ANY add argument beyond go.mod/go.sum sweeps WIP into the deps
			// commit — that covers `git add .`, `git add -A`, `git add wip.go`…
			for _, a := range args[1:] {
				if a != "go.mod" && a != "go.sum" && a != "--" {
					t.Errorf("git add on a dirty dependent must be limited to go.mod/go.sum, got: git %v", args)
				}
			}
			joined := strings.Join(args, " ")
			if strings.Contains(joined, "go.mod") && strings.Contains(joined, "go.sum") {
				sawAddPathspec = true
			}
		case "commit":
			for i, a := range args {
				if a == "-m" && i+1 < len(args) {
					commitMsg = args[i+1]
				}
			}
		case "tag":
			// `git tag <name>` = creation; `git tag -l ...` = listing (allowed)
			if len(args) >= 2 && !strings.HasPrefix(args[1], "-") {
				sawTagCreation = true
			}
		case "push":
			sawPush = true
		}
	}

	if !sawAddPathspec {
		t.Errorf("expected a pathspec-limited `git add go.mod go.sum`, git calls: %v", gitCalls)
	}
	if sawTagCreation {
		t.Error("a deps-only commit on a dirty tree must NOT create a tag")
	}
	if !sawPush {
		t.Errorf("expected a push of the deps-only commit, git calls: %v", gitCalls)
	}
	if !strings.HasPrefix(commitMsg, gitmod.DepsCommitPrefix) {
		t.Errorf("commit message must start with %q, got: %q", gitmod.DepsCommitPrefix, commitMsg)
	}
	if !strings.Contains(commitMsg, gitmod.CauseLinePrefix+"feat: nueva API de rutas") {
		t.Errorf("commit message must propagate the root cause line, got: %q", commitMsg)
	}

	// The WIP file must still exist untouched
	if _, err := os.Stat(filepath.Join(depDir, "wip.go")); err != nil {
		t.Error("wip.go must remain in the working tree")
	}
}

func TestUpdateDependentModule_ActiveSessionLeavesRepoUntouched(t *testing.T) {
	tmp := t.TempDir()
	depDir := filepath.Join(tmp, "myapp")
	os.MkdirAll(depDir, 0755)
	gomodContent := "module github.com/test/myapp\n\ngo 1.20\n\nrequire github.com/test/mylib v0.0.0\nreplace github.com/test/mylib => ../mylib\n"
	os.WriteFile(filepath.Join(depDir, "go.mod"), []byte(gomodContent), 0644)
	planDir := filepath.Join(depDir, "docs")
	_ = os.MkdirAll(planDir, 0755)
	_ = os.WriteFile(filepath.Join(planDir, "PLAN.md"), []byte("---\nPLAN: test\nSTATUS: running\n---\n"), 0644)

	g, _ := devflow.NewGo(&MockGitClient{})
	g.SetConsoleOutput(func(string) {})

	outcome, err := g.UpdateDependentModule(depDir, []gitmod.DepBump{{ModulePath: "github.com/test/mylib", NewVersion: "v0.0.1"}}, "feat: test")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if outcome.Status != devflow.CascadeStatusSkipped {
		t.Errorf("expected status %s, got %s", devflow.CascadeStatusSkipped, outcome.Status)
	}
	if outcome.Reason != gitmod.ObjectionCodejobSession {
		t.Errorf("expected reason %s, got %s", gitmod.ObjectionCodejobSession, outcome.Reason)
	}

	// Verify go.mod remains untouched
	newGomod, _ := os.ReadFile(filepath.Join(depDir, "go.mod"))
	if string(newGomod) != gomodContent {
		t.Error("go.mod was mutated despite active session")
	}
}

func TestUpdateDependentModule_OtherReplacesGoesDepsOnly(t *testing.T) {
	tmp := t.TempDir()
	depDir := filepath.Join(tmp, "myapp")
	os.MkdirAll(depDir, 0755)
	gomodContent := "module github.com/test/myapp\n\ngo 1.20\n\nrequire github.com/test/mylib v0.0.0\nrequire github.com/test/other v0.0.0\nreplace github.com/test/other => ../other\n"
	os.WriteFile(filepath.Join(depDir, "go.mod"), []byte(gomodContent), 0644)

	var mu sync.Mutex
	var gitCalls [][]string
	originalExec := command.Exec
	defer func() { command.Exec = originalExec }()
	command.Exec = func(name string, args ...string) *exec.Cmd {
		joined := strings.Join(args, " ")
		switch name {
		case "git":
			mu.Lock()
			gitCalls = append(gitCalls, args)
			mu.Unlock()
			switch {
			case strings.HasPrefix(joined, "status --porcelain"):
				// Clean tree: the only objection in play must be the
				// unrelated replace, not a dirty working tree.
				return exec.Command("echo", "")
			case strings.HasPrefix(joined, "diff"):
				// go.mod will change once bumped -> "there ARE changes to commit"
				return exec.Command("false")
			default:
				return exec.Command("true")
			}
		case "go":
			if joined == "version" {
				return exec.Command("echo", "go version go1.20 linux/amd64")
			}
			return exec.Command("true") // get / tidy / generate / list
		case "gotest":
			return exec.Command("echo", "tests ok")
		}
		return originalExec(name, args...)
	}

	mockGit := &MockGitClient{}
	g := newGoHandlerWithMockBackup(t, mockGit)
	g.SetConsoleOutput(func(string) {})
	g.SetRetryConfig(time.Millisecond, 1)

	outcome, err := g.UpdateDependentModule(depDir, []gitmod.DepBump{{ModulePath: "github.com/test/mylib", NewVersion: "v0.0.1"}}, "feat: test")
	if err != nil {
		t.Fatalf("other-replaces path must succeed as deps-only, got error: %v", err)
	}
	if outcome.Status != devflow.CascadeStatusDepsOnly {
		t.Errorf("expected status %s, got %+v", devflow.CascadeStatusDepsOnly, outcome)
	}
	if outcome.Reason != gitmod.ObjectionOtherReplaces {
		t.Errorf("expected reason %s, got %s", gitmod.ObjectionOtherReplaces, outcome.Reason)
	}

	mu.Lock()
	defer mu.Unlock()
	var sawTagCreation, sawPush bool
	for _, args := range gitCalls {
		if len(args) == 0 {
			continue
		}
		if args[0] == "tag" {
			sawTagCreation = true
		}
		if args[0] == "push" {
			sawPush = true
			for _, a := range args[1:] {
				if a == "--tags" {
					sawTagCreation = true
				}
			}
		}
	}
	if sawTagCreation {
		t.Error("deps-only outcome must never create/push a tag")
	}
	if !sawPush {
		t.Errorf("expected a push of the deps-only commit, git calls: %v", gitCalls)
	}
}

func TestUpdateDependentModule_DisplayNameIncludesParentRepo(t *testing.T) {
	tmp := t.TempDir()
	repoDir := filepath.Join(tmp, "ssr")
	depDir := filepath.Join(repoDir, "tests")
	os.MkdirAll(depDir, 0755)
	os.MkdirAll(filepath.Join(repoDir, ".git"), 0755) // marks repoDir as the repo root
	gomodContent := "module github.com/test/ssr/tests\n\ngo 1.20\n\nrequire github.com/test/mylib v0.0.0\n"
	os.WriteFile(filepath.Join(depDir, "go.mod"), []byte(gomodContent), 0644)
	planDir := filepath.Join(depDir, "docs")
	_ = os.MkdirAll(planDir, 0755)
	_ = os.WriteFile(filepath.Join(planDir, "PLAN.md"), []byte("---\nPLAN: test\nSTATUS: running\n---\n"), 0644)

	var lines []string
	g, _ := devflow.NewGo(&MockGitClient{})
	g.SetConsoleOutput(func(s string) { lines = append(lines, s) })

	outcome, err := g.UpdateDependentModule(depDir, []gitmod.DepBump{{ModulePath: "github.com/test/mylib", NewVersion: "v0.0.1"}}, "feat: test")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if outcome.Status != devflow.CascadeStatusSkipped {
		t.Fatalf("expected status %s, got %s", devflow.CascadeStatusSkipped, outcome.Status)
	}

	found := false
	for _, l := range lines {
		if strings.Contains(l, "ssr/tests") {
			found = true
		}
		if strings.Contains(l, "📦 tests →") {
			t.Errorf("display name lost the parent repo, printed the ambiguous form: %q", l)
		}
	}
	if !found {
		t.Errorf("expected a console line naming %q, got: %v", "ssr/tests", lines)
	}
}

func TestUpdateDependentModule_UpToDateLeavesRepoUntouched(t *testing.T) {
	tmp := t.TempDir()
	depDir := filepath.Join(tmp, "myapp")
	os.MkdirAll(depDir, 0755)
	gomodContent := "module github.com/test/myapp\n\ngo 1.20\n\nrequire github.com/test/mylib v0.0.1\n"
	os.WriteFile(filepath.Join(depDir, "go.mod"), []byte(gomodContent), 0644)

	mockGit := &MockGitClient{}
	g, _ := devflow.NewGo(mockGit)
	g.SetConsoleOutput(func(string) {})

	// We need to mock GetCurrentVersion via RunCommandInDir "go list -m -json"
	originalExec := command.Exec
	defer func() { command.Exec = originalExec }()
	command.Exec = func(name string, args ...string) *exec.Cmd {
		if name == "go" && args[0] == "list" && args[1] == "-m" && args[2] == "-json" {
			return exec.Command("echo", `{"Version": "v0.0.1"}`)
		}
		if name == "git" && args[0] == "status" {
			return exec.Command("echo", "")
		}
		return exec.Command("true")
	}

	outcome, err := g.UpdateDependentModule(depDir, []gitmod.DepBump{{ModulePath: "github.com/test/mylib", NewVersion: "v0.0.1"}}, "feat: test")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if outcome.Status != devflow.CascadeStatusSkipped {
		t.Errorf("expected status %s, got %s", devflow.CascadeStatusSkipped, outcome.Status)
	}
	if outcome.Reason != "already up-to-date" {
		t.Errorf("expected reason 'already up-to-date', got %s", outcome.Reason)
	}

	// Verify go.mod remains untouched (by reading the file, not the mock)
	newGomod, _ := os.ReadFile(filepath.Join(depDir, "go.mod"))
	if string(newGomod) != gomodContent {
		t.Errorf("go.mod was mutated despite being up-to-date. Original:\n%q\nNew:\n%q", gomodContent, string(newGomod))
	}
}

func TestUpdateDependentModule_MultiDependencyUnblocked(t *testing.T) {
	tmp := t.TempDir()
	depDir := filepath.Join(tmp, "myapp")
	os.MkdirAll(depDir, 0755)
	gomodContent := "module github.com/test/myapp\n\ngo 1.20\n\nrequire github.com/test/mylib v0.0.0\nrequire github.com/test/other v0.0.0\nreplace github.com/test/mylib => ../mylib\nreplace github.com/test/other => ../other\n"
	os.WriteFile(filepath.Join(depDir, "go.mod"), []byte(gomodContent), 0644)

	mockGit := &MockGitClient{}
	g, _ := devflow.NewGo(mockGit)
	g.SetConsoleOutput(func(string) {})
	g.SetRetryConfig(time.Millisecond, 1)

	// Mock commands to avoid real go get/gotest
	originalExec := command.Exec
	defer func() { command.Exec = originalExec }()
	command.Exec = func(name string, args ...string) *exec.Cmd {
		return exec.Command("true")
	}

	bumps := []gitmod.DepBump{
		{ModulePath: "github.com/test/mylib", NewVersion: "v0.0.1"},
		{ModulePath: "github.com/test/other", NewVersion: "v0.0.1"},
	}

	// Should NOT be blocked because both replaces are in the bump list
	outcome, err := g.UpdateDependentModule(depDir, bumps, "feat: test")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if outcome.Status == devflow.CascadeStatusSkipped && outcome.Reason == gitmod.ObjectionOtherReplaces {
		t.Fatal("should not be blocked by replaces that are part of the wave")
	}
}
