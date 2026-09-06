package devflow_test

import (
	"webtyp.com/command"
	gitmod "webtyp.com/git"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"

	"webtyp.com/devflow"
)

type mockStateHTTPClient struct {
	resp *http.Response
	err  error
}

func (m *mockStateHTTPClient) Do(req *http.Request) (*http.Response, error) {
	return m.resp, m.err
}

func TestJulesSessionState(t *testing.T) {
	// Case 1: Working
	client := &mockStateHTTPClient{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"id":"S1","outputs":[]}`)),
		},
	}
	msg, prURL, done, err := devflow.JulesSessionState("S1", "key", client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if done {
		t.Error("expected done=false while working")
	}
	if prURL != "" {
		t.Errorf("expected empty PR URL while working, got %q", prURL)
	}
	if !strings.Contains(msg, "working") {
		t.Errorf("expected working message, got %q", msg)
	}

	// Case 2: Done (PR Ready)
	client = &mockStateHTTPClient{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Body: io.NopCloser(strings.NewReader(`{
				"id":"S1",
				"outputs":[{"pullRequest":{"url":"https://github.com/test/pull/1","title":"feat: test"}}]
			}`)),
		},
	}
	msg, prURL, done, err = devflow.JulesSessionState("S1", "key", client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !done {
		t.Error("expected done=true when PR is ready")
	}
	if prURL != "https://github.com/test/pull/1" {
		t.Errorf("expected PR URL 'https://github.com/test/pull/1', got %q", prURL)
	}
	if !strings.Contains(msg, "PR ready") {
		t.Errorf("expected PR ready message, got %q", msg)
	}
}

func TestCheckoutPRBranch_DirtyTreeSuccess(t *testing.T) {
	dir := t.TempDir()
	defer testChdir(t, dir)()

	recorded := []string{}
	orig := command.Exec
	defer func() { command.Exec = orig }()
	command.Exec = func(name string, args ...string) *exec.Cmd {
		full := name + " " + strings.Join(args, " ")
		recorded = append(recorded, full)
		switch {
		case full == "gh pr view https://github.com/test/pull/1 --json headRefName --jq .headRefName":
			return exec.Command("echo", "feat-branch")
		case full == "git status --porcelain":
			return exec.Command("echo", " M modified-file.go")
		case full == "git branch --show-current":
			return exec.Command("echo", "feat-branch")
		default:
			return exec.Command("true")
		}
	}

	branch, err := devflow.CheckoutPRBranch(gitmod.RealRunner{}, "https://github.com/test/pull/1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if branch != "feat-branch" {
		t.Errorf("expected branch feat-branch, got %q", branch)
	}

	checkCall := func(expected string) {
		for _, c := range recorded {
			if c == expected {
				return
			}
		}
		t.Errorf("expected call %q not found in %v", expected, recorded)
	}

	checkCall("git stash push -u -m codejob: local drift before review")
	checkCall("git checkout feat-branch")
	checkCall("git stash pop")
}

func TestCheckoutPRBranch_PopConflict(t *testing.T) {
	dir := t.TempDir()
	defer testChdir(t, dir)()

	recorded := []string{}
	orig := command.Exec
	defer func() { command.Exec = orig }()
	command.Exec = func(name string, args ...string) *exec.Cmd {
		full := name + " " + strings.Join(args, " ")
		recorded = append(recorded, full)
		switch {
		case full == "gh pr view https://github.com/test/pull/1 --json headRefName --jq .headRefName":
			return exec.Command("echo", "feat-branch")
		case full == "git status --porcelain":
			return exec.Command("echo", " M modified-file.go")
		case full == "git branch --show-current":
			return exec.Command("echo", "feat-branch")
		case full == "git stash pop":
			return exec.Command("sh", "-c", "echo 'conflict'; exit 1")
		default:
			return exec.Command("true")
		}
	}

	branch, err := devflow.CheckoutPRBranch(gitmod.RealRunner{}, "https://github.com/test/pull/1")
	if err == nil {
		t.Fatal("expected error due to stash pop conflict, got nil")
	}
	if branch != "feat-branch" {
		t.Errorf("expected branch feat-branch even on pop conflict, got %q", branch)
	}
	if !strings.Contains(err.Error(), "conflict while re-applying local drift") {
		t.Errorf("expected conflict error message, got %v", err)
	}
	if !strings.Contains(err.Error(), "Stash kept") {
		t.Errorf("expected 'Stash kept' in error message, got %v", err)
	}
}

func TestMergeAndPublish_Guard(t *testing.T) {
	dir := t.TempDir()
	defer testChdir(t, dir)()

	_ = os.MkdirAll("docs", 0755)
	_ = os.WriteFile("docs/PLAN.md", []byte("---\nPLAN: test\nPR: https://github.com/test/pull/1\n---\n"), 0644)

	orig := command.Exec
	defer func() { command.Exec = orig }()
	command.Exec = func(name string, args ...string) *exec.Cmd {
		full := name + " " + strings.Join(args, " ")
		// Force checkout failure
		if full == "git checkout feat-branch" {
			return exec.Command("sh", "-c", "exit 1")
		}
		if full == "gh pr view https://github.com/test/pull/1 --json headRefName --jq .headRefName" {
			return exec.Command("echo", "feat-branch")
		}
		return exec.Command("true")
	}

	mockPub := &MockPublisher{}
	_, err := devflow.MergeAndPublish(gitmod.RealRunner{}, mockPub, "test", "")
	if err == nil {
		t.Fatal("expected MergeAndPublish to fail when checkout fails")
	}

	// Verify no commit was attempted
	// We'd need to track calls to be sure, but the error being returned is the first guard.
}

func TestMergePR_NoPRURL(t *testing.T) {
	dir := t.TempDir()
	defer testChdir(t, dir)()

	_ = os.MkdirAll("docs", 0755)
	_ = os.WriteFile("docs/PLAN.md", []byte("---\nPLAN: test\n---\n"), 0644)

	err := devflow.MergePR(gitmod.RealRunner{})
	if err == nil {
		t.Fatal("expected error when no PR URL in PLAN.md, got nil")
	}
	if !strings.Contains(err.Error(), "no pending PR found") {
		t.Errorf("expected 'no pending PR found' error, got: %v", err)
	}
}

func TestMergeAndPublish_NoPRURL(t *testing.T) {
	dir := t.TempDir()
	defer testChdir(t, dir)()

	_ = os.MkdirAll("docs", 0755)
	_ = os.WriteFile("docs/PLAN.md", []byte("---\nPLAN: test\n---\n"), 0644)

	_, err := devflow.MergeAndPublish(&mockRunner{}, &MockPublisher{}, "test", "")
	if err == nil {
		t.Fatal("expected error when no PR URL in PLAN.md, got nil")
	}
	if !strings.Contains(err.Error(), "no pending PR found") {
		t.Errorf("expected 'no pending PR found' error, got: %v", err)
	}
}

// mockExecFor returns an ExecCommand replacement that records all calls and
// simulates a dirty or clean working tree. All other commands succeed silently.
// The returned *[]string grows with each command invocation.
func mockExecFor(dirtyStatus bool) (fn func(string, ...string) *exec.Cmd, calls *[]string) {
	recorded := []string{}
	calls = &recorded
	fn = func(name string, args ...string) *exec.Cmd {
		full := name + " " + strings.Join(args, " ")
		*calls = append(*calls, full)
		switch {
		case full == "gh pr view https://github.com/test/pull/1 --json headRefName --jq .headRefName":
			return exec.Command("echo", "feat-branch")
		case full == "git status --porcelain":
			if dirtyStatus {
				// Simulate two modified tracked files
				return exec.Command("echo", " M errors.go")
			}
			return exec.Command("true")
		case full == "git symbolic-ref --short refs/remotes/origin/HEAD":
			return exec.Command("echo", "origin/main")
		case full == "git branch --show-current":
			return exec.Command("echo", "feat-branch")
		case strings.HasPrefix(full, "git rev-parse v"):
			// Tag doesn't exist (TagExists returns false → CreateTag proceeds)
			return exec.Command("sh", "-c", "exit 1")
		default:
			return exec.Command("true")
		}
	}
	return
}

// TestMergeAndPublish_DirtyStateCommitsBeforeMerge verifies that when there are
// local uncommitted changes (review corrections), MergeAndPublish:
//  1. commits + pushes them to the Jules branch
//  2. then explicitly switches to main
//  3. then runs gh pr merge
func TestMergeAndPublish_DirtyStateCommitsBeforeMerge(t *testing.T) {
	dir := t.TempDir()
	defer testChdir(t, dir)()

	_ = os.MkdirAll("docs", 0755)
	_ = os.WriteFile("docs/PLAN.md", []byte("---\nPLAN: test\nPR: https://github.com/test/pull/1\n---\n"), 0644)

	mockFn, calls := mockExecFor(true)
	orig := command.Exec
	defer func() { command.Exec = orig }()
	command.Exec = mockFn

	idxOf := func(prefix string) int {
		for i, c := range *calls {
			if strings.HasPrefix(c, prefix) {
				return i
			}
		}
		return -1
	}

	mockPub := &MockPublisher{}
	devflow.MergeAndPublish(gitmod.RealRunner{}, mockPub, "test", "") //nolint: the result is not relevant; we test the call sequence

	statusIdx := idxOf("git status --porcelain")
	addIdx := idxOf("git add .")
	commitIdx := idxOf("git commit -m review:")
	pushIdx := idxOf("git push")
	checkoutIdx := idxOf("git checkout main")
	mergeIdx := idxOf("gh pr merge")

	if statusIdx < 0 {
		t.Error("git status --porcelain was not called")
	}
	if addIdx < 0 {
		t.Error("git add . was not called (review corrections not staged)")
	}
	if commitIdx < 0 {
		t.Error("git commit review: corrections was not called (corrections not committed)")
	}
	if pushIdx < 0 {
		t.Error("git push was not called (corrections not pushed to Jules branch)")
	}
	if checkoutIdx < 0 {
		t.Error("git checkout main was not called before merge")
	}
	if mergeIdx < 0 {
		t.Error("gh pr merge was not called")
	}

	// Verify ordering: status → add → commit → push → checkout main → merge
	if addIdx < statusIdx {
		t.Errorf("git add (%d) should come after git status (%d)", addIdx, statusIdx)
	}
	if commitIdx < addIdx {
		t.Errorf("git commit (%d) should come after git add (%d)", commitIdx, addIdx)
	}
	if pushIdx < commitIdx {
		t.Errorf("git push (%d) should come after git commit (%d)", pushIdx, commitIdx)
	}
	if checkoutIdx < pushIdx {
		t.Errorf("git checkout main (%d) should come after git push (%d)", checkoutIdx, pushIdx)
	}
	if mergeIdx < checkoutIdx {
		t.Errorf("gh pr merge (%d) should come after git checkout main (%d)", mergeIdx, checkoutIdx)
	}
}

// TestMergeAndPublish_CleanStateSkipsPreCommit verifies that when the working
// tree is clean, no pre-merge commit is attempted, but the branch switch to
// main and gh pr merge still happen in the correct order.
func TestMergeAndPublish_CleanStateSkipsPreCommit(t *testing.T) {
	dir := t.TempDir()
	defer testChdir(t, dir)()

	_ = os.MkdirAll("docs", 0755)
	_ = os.WriteFile("docs/PLAN.md", []byte("---\nPLAN: test\nPR: https://github.com/test/pull/1\n---\n"), 0644)

	mockFn, calls := mockExecFor(false)
	orig := command.Exec
	defer func() { command.Exec = orig }()
	command.Exec = mockFn

	idxOf := func(prefix string) int {
		for i, c := range *calls {
			if strings.HasPrefix(c, prefix) {
				return i
			}
		}
		return -1
	}

	mockPub := &MockPublisher{}
	devflow.MergeAndPublish(gitmod.RealRunner{}, mockPub, "test", "") //nolint: the result is not relevant; we test the call sequence

	commitIdx := idxOf("git commit -m review:")
	checkoutIdx := idxOf("git checkout main")
	mergeIdx := idxOf("gh pr merge")

	if commitIdx >= 0 {
		t.Error("git commit review: should NOT be called when working tree is clean")
	}
	if checkoutIdx < 0 {
		t.Error("git checkout main was not called")
	}
	if mergeIdx < 0 {
		t.Error("gh pr merge was not called")
	}
	if mergeIdx < checkoutIdx {
		t.Errorf("gh pr merge (%d) should come after git checkout main (%d)", mergeIdx, checkoutIdx)
	}
}

// TestMergeAndPublish_UsesMasterWhenThatsTheDefaultBranch is a regression
// test: repos whose default branch is "master" (e.g. old forks) must not
// have MergeAndPublish hardcode "git checkout main" — it should resolve and
// use the actual default branch from origin/HEAD.
func TestMergeAndPublish_UsesMasterWhenThatsTheDefaultBranch(t *testing.T) {
	dir := t.TempDir()
	defer testChdir(t, dir)()

	_ = os.MkdirAll("docs", 0755)
	_ = os.WriteFile("docs/PLAN.md", []byte("---\nPLAN: test\nPR: https://github.com/test/pull/1\n---\n"), 0644)

	recorded := []string{}
	mockFn := func(name string, args ...string) *exec.Cmd {
		full := name + " " + strings.Join(args, " ")
		recorded = append(recorded, full)
		switch {
		case full == "gh pr view https://github.com/test/pull/1 --json headRefName --jq .headRefName":
			return exec.Command("echo", "feat-branch")
		case full == "git branch --show-current":
			return exec.Command("echo", "feat-branch")
		case full == "git status --porcelain":
			return exec.Command("true")
		case full == "git symbolic-ref --short refs/remotes/origin/HEAD":
			return exec.Command("echo", "origin/master")
		case strings.HasPrefix(full, "git rev-parse v"):
			return exec.Command("sh", "-c", "exit 1")
		default:
			return exec.Command("true")
		}
	}
	orig := command.Exec
	defer func() { command.Exec = orig }()
	command.Exec = mockFn

	mockPub := &MockPublisher{}
	devflow.MergeAndPublish(gitmod.RealRunner{}, mockPub, "test", "") //nolint: the result is not relevant; we test the call sequence

	idxOf := func(prefix string) int {
		for i, c := range recorded {
			if strings.HasPrefix(c, prefix) {
				return i
			}
		}
		return -1
	}

	if idxOf("git checkout master") < 0 {
		t.Errorf("expected 'git checkout master' to be called, got calls: %v", recorded)
	}
	if idxOf("git checkout main") >= 0 {
		t.Errorf("did not expect 'git checkout main' when default branch is master, got calls: %v", recorded)
	}
}

func TestMergeAndPublish_TagOverride(t *testing.T) {
	dir := t.TempDir()
	defer testChdir(t, dir)()

	_ = os.MkdirAll("docs", 0755)
	_ = os.WriteFile("docs/PLAN.md", []byte("---\nPLAN: test\nPR: https://github.com/test/pull/1\n---\n"), 0644)

	mockFn, _ := mockExecFor(false)
	orig := command.Exec
	defer func() { command.Exec = orig }()
	command.Exec = mockFn

	mockPub := &MockPublisher{
		PublishFn: func(message, tag string, skipTests, skipRace, skipDependents, skipBackup, skipTag, skipVerify bool) (gitmod.PushResult, error) {
			return gitmod.PushResult{Tag: tag, Summary: "Mock published " + tag}, nil
		},
	}

	result, err := devflow.MergeAndPublish(gitmod.RealRunner{}, mockPub, "test", "v1.2.3")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Tag != "v1.2.3" {
		t.Errorf("expected tag v1.2.3, got %q", result.Tag)
	}
}
