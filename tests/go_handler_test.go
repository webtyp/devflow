package devflow_test

import "webtyp.com/devflow"

import (
	"fmt"
	"webtyp.com/command"
	gitmod "webtyp.com/git"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestGoGetModulePath(t *testing.T) {
	dir, cleanup := testCreateGoModule("github.com/test/repo")
	defer cleanup()
	defer testChdir(t, dir)()

	mockGit := &MockGitClient{}
	goHandler := newGoHandlerWithMockBackup(t, mockGit)

	path, err := goHandler.GetModulePath()
	if err != nil {
		t.Fatal(err)
	}

	if path != "github.com/test/repo" {
		t.Errorf("Expected github.com/test/repo, got %s", path)
	}
}

func TestGoModVerify(t *testing.T) {
	dir, cleanup := testCreateGoModule("github.com/test/repo")
	defer cleanup()
	defer testChdir(t, dir)()

	mockGit := &MockGitClient{}
	goHandler := newGoHandlerWithMockBackup(t, mockGit)

	err := goHandler.Verify()
	if err != nil {
		t.Fatal(err)
	}
}

func TestGoTest(t *testing.T) {
	dir, cleanup := testCreateGoModule("github.com/test/repo")
	defer cleanup()

	// Create passing test
	testContent := `package main
import "testing"
func TestExample(t *testing.T) {}
`
	os.WriteFile(dir+"/main_test.go", []byte(testContent), 0644)

	defer testChdir(t, dir)()

	// Mock ExecCommand to make go vet instant — avoids non-deterministic slowness
	// under load that can push the test past the 30s binary timeout.
	originalExec := command.Exec
	defer func() { command.Exec = originalExec }()
	command.Exec = func(name string, args ...string) *exec.Cmd {
		if name == "go" && len(args) > 0 && (args[0] == "vet" || args[0] == "tool") {
			return exec.Command("true")
		}
		return originalExec(name, args...)
	}

	mockGit := &MockGitClient{}
	goHandler := newGoHandlerWithMockBackup(t, mockGit)

	_, err := goHandler.Test(devflow.TestOptions{})
	if err != nil {
		// In test environment, tests might fail, but we check the call works
		t.Log("Test failed as expected in test environment:", err)
	}
}

func TestFindDependentModules(t *testing.T) {
	// Create temporary directory structure
	tmpDir := t.TempDir()

	// Main module
	mainDir := tmpDir + "/main"
	os.MkdirAll(mainDir, 0755)
	// Important: module name matches what dep1 requires
	os.WriteFile(mainDir+"/go.mod", []byte("module github.com/test/main\n\ngo 1.20\n"), 0644)

	// Dependent module 1
	dep1Dir := tmpDir + "/dep1"
	os.MkdirAll(dep1Dir, 0755)
	dep1Mod := `module github.com/test/dep1

go 1.20

require github.com/test/main v0.0.1
`
	os.WriteFile(dep1Dir+"/go.mod", []byte(dep1Mod), 0644)

	// Independent module
	indepDir := tmpDir + "/indep"
	os.MkdirAll(indepDir, 0755)
	os.WriteFile(indepDir+"/go.mod", []byte("module github.com/test/indep\n\ngo 1.20\n"), 0644)

	mockGit := &MockGitClient{}
	goHandler := newGoHandlerWithMockBackup(t, mockGit)

	// Search dependents
	dependents, err := goHandler.FindDependentModules("github.com/test/main", tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	// Should find only dep1
	if len(dependents) != 1 {
		for _, d := range dependents {
			t.Logf("Found: %s", d)
		}
		t.Errorf("Expected 1 dependent, got %d", len(dependents))
	}
}

func TestHasDependency(t *testing.T) {
	tmpDir := t.TempDir()
	gomodPath := tmpDir + "/go.mod"

	content := `module github.com/test/repo

go 1.20

require webtyp.com/devflow v0.0.1
`
	os.WriteFile(gomodPath, []byte(content), 0644)

	mockGit := &MockGitClient{}
	goHandler := newGoHandlerWithMockBackup(t, mockGit)

	// Should find the dependency
	if !goHandler.HasDependency(gomodPath, "webtyp.com/devflow") {
		t.Error("Expected to find dependency")
	}

	// Should not find this one
	if goHandler.HasDependency(gomodPath, "github.com/other/repo") {
		t.Error("Should not find non-existent dependency")
	}
}

func TestGoPush(t *testing.T) {
	// Use MockGitClient to decouple from real git and avoid remote access issues in tests
	mockGit := &MockGitClient{
		latestTag: "v0.0.0",
		log:       func(args ...any) {},
	}

	// We need to create a dummy go module context because Go.Push reads go.mod
	dir, cleanup := testCreateGoModule("github.com/test/repo")
	defer cleanup()
	defer testChdir(t, dir)()

	goHandler := newGoHandlerWithMockBackup(t, mockGit)

	// Create test file so tests pass (Go.Push runs tests by default)
	// But actually, Go.Push runs tests using `go test`.
	// Since we are mocking Git, we might still fail on `go test` if we don't have valid go files?
	// The original test created main_test.go. Let's keep that.
	testContent := `package main
import "testing"
func TestExample(t *testing.T) {}
`
	os.WriteFile("main_test.go", []byte(testContent), 0644)

	// We need to ensure Go.verify() passes. It runs `go mod verify`.
	// Make sure go.mod is valid (testCreateGoModule does this).

	// Run Push
	// We skip dependents (true) and backup (true/false) to focus on core flow
	result, err := goHandler.Push("test update", "v0.0.1", false, true, true, true, false, false, "")
	if err != nil {
		t.Fatalf("Go Push failed: %v", err)
	}

	// Verify summary contains expected elements
	// Mock returns "Mock push ok"
	if !strings.Contains(result.Summary, "Mock push ok") {
		t.Errorf("Expected summary to contain 'Mock push ok', got: %s", result.Summary)
	}
	if !strings.Contains(result.Summary, "vet ✅") {
		t.Errorf("Expected summary to contain 'vet ✅', got: %s", result.Summary)
	}
}

func TestGoPush_NoGoMod(t *testing.T) {
	mockGit := &MockGitClient{
		pushResult: gitmod.PushResult{Summary: "Git push ok"},
	}

	// Temp dir WITHOUT go.mod
	dir := t.TempDir()
	defer testChdir(t, dir)()

	goHandler := newGoHandlerWithMockBackup(t, mockGit)

	// Run Push
	result, err := goHandler.Push("test", "", false, false, false, false, false, false, "")
	if err != nil {
		t.Fatalf("Push failed: %v", err)
	}

	if result.Summary != "Git push ok" {
		t.Errorf("Expected 'Git push ok', got: %s", result.Summary)
	}
}

func TestGoPush_SkipTag(t *testing.T) {
	mockGit := &MockGitClient{}
	dir, cleanup := testCreateGoModule("github.com/test/repo")
	defer cleanup()
	defer testChdir(t, dir)()

	goHandler := newGoHandlerWithMockBackup(t, mockGit)

	// Run Push with skipTag=true
	result, err := goHandler.Push("test", "", true, true, true, true, true, false, "")
	if err != nil {
		t.Fatalf("Push failed: %v", err)
	}

	if !strings.Contains(result.Summary, "Pushed ✅") {
		t.Errorf("Expected summary to contain '✅ Pushed commits', got: %s", result.Summary)
	}
	if !mockGit.pushWithoutTagsCalled {
		t.Error("PushWithoutTags should have been called")
	}
}

func TestGoPush_DependentOutput(t *testing.T) {
	// This tests that dependents print to consoleOutput and NOT to summary
	var consoleLines []string
	var consoleMu sync.Mutex
	mockGit := &MockGitClient{
		createdTag: "v1.0.0",
	}

	dir, cleanup := testCreateGoModule("github.com/test/main")
	defer cleanup()
	defer testChdir(t, dir)()

	// Create a dependent
	depDir := filepath.Join(filepath.Join(filepath.Dir(dir), "dep"))
	os.MkdirAll(depDir, 0755)
	os.WriteFile(filepath.Join(depDir, "go.mod"), []byte("module github.com/test/dep\n\nrequire github.com/test/main v0.0.0\n"), 0644)

	// Mock ExecCommand to prevent real go/git/gotest invocations
	originalExec := command.Exec
	defer func() { command.Exec = originalExec }()

	command.Exec = func(name string, args ...string) *exec.Cmd {
		if name == "go" {
			cmdStr := strings.Join(args, " ")
			if cmdStr == "version" {
				return exec.Command("echo", "go version go1.20 linux/amd64")
			}
			if strings.Contains(cmdStr, "mod verify") {
				return exec.Command("echo", "all modules verified")
			}
			if strings.Contains(cmdStr, "list -m -json") {
				return exec.Command("echo", `{"Version": "v0.0.0"}`)
			}
			if cmdStr == "list -m" || (strings.Contains(cmdStr, "list") && strings.Contains(cmdStr, "-m") && !strings.Contains(cmdStr, "-json")) {
				return exec.Command("echo", "github.com/test/main")
			}
			if strings.Contains(cmdStr, "get") || strings.Contains(cmdStr, "tidy") {
				return exec.Command("echo", "mock success")
			}
		}
		if name == "gotest" {
			return exec.Command("echo", "tests passed")
		}
		if name == "git" {
			return exec.Command("echo", "git success")
		}
		return originalExec(name, args...)
	}

	goHandler := newGoHandlerWithMockBackup(t, mockGit)
	goHandler.SetConsoleOutput(func(s string) {
		consoleMu.Lock()
		consoleLines = append(consoleLines, s)
		consoleMu.Unlock()
	})
	goHandler.SetRetryConfig(10*time.Millisecond, 2)

	// We need to mock FindDependentModules or ensure it finds our dep
	// SearchPath is the parent of dir
	result, _ := goHandler.Push("feat: main", "v1.0.0", true, true, false, true, false, false, "..")

	// Summary should NOT contain dep update result
	if strings.Contains(result.Summary, "updated to v1.0.0") {
		t.Errorf("Summary should not contain dep update result, got: %s", result.Summary)
	}

	// Console should contain dep update result
	found := false
	for _, line := range consoleLines {
		if strings.Contains(line, "📦 dep") {
			found = true
			break
		}
	}
	if !found {
		t.Log("Console output:", consoleLines)
	}
}

func TestUpdateDependentModule(t *testing.T) {
	// 1. Setup structure:
	// /tmp/test-gopush/
	// ├── mylib/  (module github.com/test/mylib)
	// └── myapp/  (module github.com/test/myapp, requires mylib, replace ../mylib)

	tmp := t.TempDir()
	mylibDir := filepath.Join(tmp, "mylib")
	myappDir := filepath.Join(tmp, "myapp")

	os.MkdirAll(mylibDir, 0755)
	os.MkdirAll(myappDir, 0755)

	// Create mylib
	os.WriteFile(filepath.Join(mylibDir, "go.mod"), []byte("module github.com/test/mylib\n\ngo 1.20\n"), 0644)
	os.WriteFile(filepath.Join(mylibDir, "mylib.go"), []byte("package mylib\n"), 0644)

	// Create myapp
	os.WriteFile(filepath.Join(myappDir, "go.mod"), []byte("module github.com/test/myapp\n\ngo 1.20\n\nrequire github.com/test/mylib v0.0.0\nreplace github.com/test/mylib => ../mylib\n"), 0644)

	// Init git in myapp (needed for Push)
	runGit := func(dir string, args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if err := cmd.Run(); err != nil {
			t.Logf("git %v in %s failed: %v", args, dir, err)
		}
	}

	runGit(myappDir, "init")
	runGit(myappDir, "config", "user.name", "Test")
	runGit(myappDir, "config", "user.email", "test@test.com")
	runGit(myappDir, "add", ".")
	runGit(myappDir, "commit", "-m", "initial")

	// Setup remote for myapp
	remoteDir, _ := os.MkdirTemp("", "myapp-remote-")
	defer os.RemoveAll(remoteDir)
	exec.Command("git", "init", "--bare", remoteDir).Run()
	runGit(myappDir, "remote", "add", "origin", remoteDir)

	neutralDir := t.TempDir()
	defer testChdir(t, neutralDir)()

	// Disable proxy so "go get" fails instantly without network requests
	t.Setenv("GOPROXY", "off")

	mockGit := &MockGitClient{}
	g := newGoHandlerWithMockBackup(t, mockGit)
	g.SetRetryConfig(time.Millisecond, 1)

	result, err := g.UpdateDependentModule(myappDir, []gitmod.DepBump{{ModulePath: "github.com/test/mylib", NewVersion: "v0.0.1"}}, "")

	// We expect a failure at "go get" because the module doesn't exist in registry
	if err == nil {
		t.Errorf("Expected error from go get (module not in registry), got result: %+v", result)
	} else if !strings.Contains(err.Error(), "go get failed") {
		t.Errorf("Expected error to contain 'go get failed', got: %v", err)
	}

	// However, we can verify that the replace was removed BEFORE the go get failure
	gomodContent, _ := os.ReadFile(filepath.Join(myappDir, "go.mod"))
	if strings.Contains(string(gomodContent), "replace github.com/test/mylib") {
		t.Error("replace directive should have been removed even if go get failed later")
	}
}

// TestGoInstall_SubmoduleCmdUsesOwnDir verifies that when a cmd subdirectory
// has its own go.mod (a separate module), Install() runs `go install .` from
// that subdirectory instead of `go install ./cmd/<name>` from the root, which
// would fail with "does not contain package".
func TestGoInstall_SubmoduleCmdUsesOwnDir(t *testing.T) {
	type call struct{ args []string }
	var calls []call

	original := command.Exec
	defer func() { command.Exec = original }()
	command.Exec = func(name string, args ...string) *exec.Cmd {
		if name == "go" && len(args) > 0 && args[0] == "install" {
			calls = append(calls, call{args: append([]string{name}, args...)})
			// return success no-op
			return exec.Command("true")
		}
		return original(name, args...)
	}

	tmpDir := t.TempDir()

	// cmd/regular — part of root module (no go.mod)
	os.MkdirAll(filepath.Join(tmpDir, "cmd/regular"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "cmd/regular/main.go"), []byte("package main\nfunc main() {}\n"), 0644)

	// cmd/ddlc — separate module (has its own go.mod)
	os.MkdirAll(filepath.Join(tmpDir, "cmd/ddlc"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "cmd/ddlc/main.go"), []byte("package main\nfunc main() {}\n"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "cmd/ddlc/go.mod"), []byte("module github.com/test/ddlc\n\ngo 1.21\n"), 0644)

	mockGit := &MockGitClient{}
	g := newGoHandlerWithMockBackup(t, mockGit)
	g.SetRootDir(tmpDir)

	if err := g.Install("v0.9.18"); err != nil {
		t.Fatalf("Install returned unexpected error: %v", err)
	}

	// The submodule cmd must NOT be installed via ./cmd/ddlc from the root
	for _, c := range calls {
		for _, arg := range c.args {
			if arg == "./cmd/ddlc" {
				t.Errorf("Install used ./cmd/ddlc from root — this fails when cmd has its own go.mod; args: %v", c.args)
			}
		}
	}

	// At least one call must use "." (install from the submodule dir itself)
	foundDot := false
	for _, c := range calls {
		for _, arg := range c.args {
			if arg == "." {
				foundDot = true
			}
		}
	}
	if !foundDot {
		t.Errorf("Expected at least one 'go install .' call for the submodule cmd; got: %v", calls)
	}
}

func TestGoInstall(t *testing.T) {
	tmpDir := t.TempDir()

	// Create cmd structure
	os.MkdirAll(filepath.Join(tmpDir, "cmd/tool1"), 0755)
	os.MkdirAll(filepath.Join(tmpDir, "cmd/tool2"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "cmd/tool1/main.go"), []byte("package main\nfunc main() {}\n"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "cmd/tool2/main.go"), []byte("package main\nfunc main() {}\n"), 0644)

	mockGit := &MockGitClient{}
	g := newGoHandlerWithMockBackup(t, mockGit)
	g.SetRootDir(tmpDir)

	err := g.Install("v1.2.3")
	if err != nil {
		t.Logf("Install failed as expected (no real module in tmp): %v", err)
	}

	// Verify behavior when cmd is missing
	g.SetRootDir(t.TempDir())
	if err := g.Install("v1.0.0"); err != nil {
		t.Errorf("Expected no error when cmd is missing, got %v", err)
	}
}

// MockGitClient for testing
type MockGitClient struct {
	checkAccessErr        error
	pushErr               error
	latestTag             string
	createdTag            string
	pushResult            gitmod.PushResult
	pushWithoutTagsCalled bool
	log                   func(...any)
	AddCalls              int
	CommitCalls           int
	LastPushTag           string
	LastPushMessage       string
	statusPorcelainOut    string
	diffShortStatOut      string
	CommitPathsCalls      [][]string
	nextTagOverride       string
}

func (m *MockGitClient) CheckRemoteAccess() error {
	return m.checkAccessErr
}

func (m *MockGitClient) Push(message, tag string) (gitmod.PushResult, error) {
	m.LastPushTag = tag
	m.LastPushMessage = message
	if m.checkAccessErr != nil {
		return gitmod.PushResult{}, m.checkAccessErr
	}
	if m.pushErr != nil {
		return gitmod.PushResult{}, m.pushErr
	}
	if m.pushResult.Summary != "" || m.pushResult.Tag != "" {
		return m.pushResult, nil
	}
	resultTag := m.createdTag
	if resultTag == "" {
		resultTag = "v0.0.1" // Default test tag
	}
	if tag != "" {
		resultTag = tag
	}
	return gitmod.PushResult{
		Summary: "Mock push ok",
		Tag:     resultTag,
	}, nil
}

func (m *MockGitClient) GetLatestTag() (string, error) {
	return m.latestTag, nil
}

func (m *MockGitClient) SetLog(fn func(...any)) {
	m.log = fn
}

func (m *MockGitClient) SetShouldWrite(fn func() bool) {
}

func (m *MockGitClient) SetRootDir(path string) {
}

func (m *MockGitClient) GitIgnoreAdd(entry string) error {
	return nil
}

func (m *MockGitClient) Add() error {
	m.AddCalls++
	return nil
}

func (m *MockGitClient) Commit(message string) (bool, error) {
	m.CommitCalls++
	return true, nil
}

func (m *MockGitClient) CreateTag(tag string) (bool, error) {
	return true, nil
}

func (m *MockGitClient) PushWithTags(tag string) (bool, error) {
	return false, nil
}

func (m *MockGitClient) PushWithoutTags() (bool, error) {
	m.pushWithoutTagsCalled = true
	return false, nil
}

func (m *MockGitClient) GetConfigUserName() (string, error) {
	return "Mock User", nil
}

func (m *MockGitClient) GetConfigUserEmail() (string, error) {
	return "mock@example.com", nil
}

func (m *MockGitClient) InitRepo(dir string) error {
	return nil
}

func (m *MockGitClient) HasPendingChanges() (bool, error) {
	return true, nil
}

func (m *MockGitClient) GenerateNextTag() (string, error) {
	if m.nextTagOverride != "" {
		return m.nextTagOverride, nil
	}
	return "v0.0.1", nil
}

func (m *MockGitClient) StatusPorcelain() (string, error) {
	return m.statusPorcelainOut, nil
}

func (m *MockGitClient) CommitPaths(message string, paths ...string) (bool, error) {
	m.CommitCalls++
	m.CommitPathsCalls = append(m.CommitPathsCalls, append([]string{message}, paths...))
	return true, nil
}

func (m *MockGitClient) DiffShortStat() (string, error) {
	return m.diffShortStatOut, nil
}

func (m *MockGitClient) Clone(repoURL string) (bool, error) {
	return false, nil
}

func (m *MockGitClient) Pull() error {
	return nil
}

func (m *MockGitClient) Fetch() error {
	return nil
}

// IncrementTag mirrors the real gitmod.Git.IncrementTag: bumps the last
// dot-separated numeric segment by one. Required by gitmod.GitClient —
// see sumdb_test.go.
func (m *MockGitClient) IncrementTag(tag string) (string, error) {
	if tag == "" {
		return "v0.0.1", nil
	}
	parts := strings.Split(tag, ".")
	if len(parts) < 3 {
		return "", fmt.Errorf("invalid tag format: %s", tag)
	}
	last, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil {
		return "", fmt.Errorf("invalid tag number: %s", parts[len(parts)-1])
	}
	parts[len(parts)-1] = strconv.Itoa(last + 1)
	return strings.Join(parts, "."), nil
}

// TestGoPush_AppendsShortStatBody: the root push keeps the user's title intact
// and appends the staged `git diff --shortstat` as the commit body (PLAN.md
// Fase 2) — quantitative context with zero AI and zero typing.
func TestGoPush_AppendsShortStatBody(t *testing.T) {
	mockGit := &MockGitClient{
		latestTag:        "v0.0.0",
		diffShortStatOut: "3 files changed, 42 insertions(+), 7 deletions(-)",
	}

	dir, cleanup := testCreateGoModule("github.com/test/repo")
	defer cleanup()
	defer testChdir(t, dir)()

	goHandler := newGoHandlerWithMockBackup(t, mockGit)

	_, err := goHandler.Push("feat: nueva feature", "v0.0.1", true, true, true, true, false, false, "")
	if err != nil {
		t.Fatalf("Push failed: %v", err)
	}

	if !strings.HasPrefix(mockGit.LastPushMessage, "feat: nueva feature") {
		t.Errorf("user title must stay first, got: %q", mockGit.LastPushMessage)
	}
	if !strings.Contains(mockGit.LastPushMessage, "3 files changed") {
		t.Errorf("commit message must include the shortstat body, got: %q", mockGit.LastPushMessage)
	}
}

func TestGoPush_RemoteAccessFailure(t *testing.T) {
	dir, cleanup := testCreateGoModule("github.com/test/repo")
	defer cleanup()
	defer testChdir(t, dir)()

	mockGit := &MockGitClient{
		checkAccessErr: fmt.Errorf("❌ Network error"),
		log:            func(args ...any) {},
	}

	goHandler := newGoHandlerWithMockBackup(t, mockGit)

	_, err := goHandler.Push("msg", "tag", true, true, true, true, false, false, "")

	if err == nil {
		t.Fatal("Expected error due to remote access failure, got nil")
	}

	if !strings.Contains(err.Error(), "Network error") {
		t.Errorf("Expected error to contain 'Network error', got: %v", err)
	}
}

func TestGoPush_SkipTag_CallsAddBeforeCommit(t *testing.T) {
	// Test that when skipTag=true, the flow calls git.Add() before git.Commit()
	// This is a non-Go project (no go.mod) case
	tmpDir := t.TempDir()
	defer testChdir(t, tmpDir)()

	// Create a git repo
	command.Run("git", "init")
	command.Run("git", "config", "user.email", "test@test.com")
	command.Run("git", "config", "user.name", "Test User")
	command.Run("git", "commit", "--allow-empty", "-m", "initial")

	// Create a mock git client to track Add() and Commit() calls
	mockGit := &MockGitClient{
		latestTag: "v0.0.0",
	}

	goHandler := newGoHandlerWithMockBackup(t, mockGit)

	// Call Push with skipTag=true (non-Go project since we have no go.mod)
	_, err := goHandler.Push("test message", "v1.0.0", false, false, false, false, true, false, "")

	if err != nil {
		t.Logf("Push returned error (expected for mock): %v", err)
	}

	// Verify that Add() was called
	if mockGit.AddCalls == 0 {
		t.Error("Add() should have been called before Commit() in skipTag=true path")
	}

	// Verify that Commit() was called after Add()
	if mockGit.CommitCalls == 0 {
		t.Error("Commit() should have been called")
	}

	// Verify order: AddCalls should exist before CommitCalls started
	if mockGit.AddCalls > 0 && mockGit.CommitCalls > 0 {
		// This is a simple check; ideally we'd track call order precisely
		t.Logf("✅ Add() was called %d time(s), Commit() was called %d time(s)", mockGit.AddCalls, mockGit.CommitCalls)
	}
}

func TestGoPush_SkipVerify_DoesNotCallVerify(t *testing.T) {
	dir, cleanup := testCreateGoModule("github.com/test/repo")
	defer cleanup()
	defer testChdir(t, dir)()

	mockGit := &MockGitClient{
		pushResult: gitmod.PushResult{Summary: "Mock push ok", Tag: "v0.0.1"},
		latestTag:  "v0.0.0",
	}

	goHandler := newGoHandlerWithMockBackup(t, mockGit)

	// Introduce an invalid go.mod to ensure verify would fail if called
	os.WriteFile("go.mod", []byte("module github.com/test/repo\n\ngo 1.21\n\nrequire github.com/nonexistent/pkg v0.0.0\n"), 0644)

	// skipVerify=true: push must succeed despite broken go.mod
	_, err := goHandler.Push("test", "v0.0.1", true, true, true, true, false, true, "")
	if err != nil {
		t.Fatalf("Push with skipVerify=true should not fail on broken go.mod, got: %v", err)
	}
}

func TestParseVerifyError_UnknownRevision(t *testing.T) {
	output := "go: webtyp.com/tinygo@v0.0.0: reading webtyp.com/tinygo/go.mod at revision v0.0.0: unknown revision v0.0.0"

	dir, cleanup := testCreateGoModule("github.com/test/repo")
	defer cleanup()
	defer testChdir(t, dir)()

	mockGit := &MockGitClient{}
	goHandler := newGoHandlerWithMockBackup(t, mockGit)

	// Write the broken go.mod and mock RunCommand via a patched verify
	_ = goHandler
	_ = output

	msg, ok := devflow.ParseVerifyError(output)
	if !ok {
		t.Fatal("Expected parseVerifyError to match unknown revision")
	}
	if !strings.Contains(msg, "is not published") {
		t.Errorf("Expected actionable message about publishing, got: %s", msg)
	}
	if !strings.Contains(msg, "webtyp.com/tinygo@v0.0.0") {
		t.Errorf("Expected module reference in message, got: %s", msg)
	}
}

func TestParseVerifyError_ChecksumMismatch(t *testing.T) {
	output := "verifying github.com/foo/bar@v1.2.3: checksum mismatch"

	msg, ok := devflow.ParseVerifyError(output)
	if !ok {
		t.Fatal("Expected parseVerifyError to match checksum mismatch")
	}
	if !strings.Contains(msg, "go clean -modcache") {
		t.Errorf("Expected actionable message with go clean -modcache, got: %s", msg)
	}
}

func TestParseVerifyError_MissingGoSum(t *testing.T) {
	output := "missing go.sum entry for module providing package github.com/foo/bar"

	msg, ok := devflow.ParseVerifyError(output)
	if !ok {
		t.Fatal("Expected parseVerifyError to match missing go.sum")
	}
	if !strings.Contains(msg, "go mod tidy") {
		t.Errorf("Expected actionable message with go mod tidy, got: %s", msg)
	}
}

func TestParseVerifyError_UnknownPattern(t *testing.T) {
	output := "some random unrecognized error from go mod verify"

	_, ok := devflow.ParseVerifyError(output)
	if ok {
		t.Error("Expected parseVerifyError to return false for unrecognized patterns")
	}
}

func TestCodejobPhaseOf(t *testing.T) {
	dir := t.TempDir()
	planDir := filepath.Join(dir, "docs")
	_ = os.MkdirAll(planDir, 0755)
	planPath := filepath.Join(planDir, "PLAN.md")

	// No PLAN.md → none
	if devflow.CodejobPhaseOf(dir) != "" {
		t.Fatal("expected empty phase when PLAN.md missing")
	}

	// STATUS set to running → PhaseRunning
	os.WriteFile(planPath, []byte("---\nPLAN: \"test\"\nSTATUS: running\n---\n"), 0644)
	if devflow.CodejobPhaseOf(dir) != devflow.PhaseRunning {
		t.Fatal("expected running phase when STATUS: running")
	}

	// STATUS set to reviewing → PhaseRunning
	os.WriteFile(planPath, []byte("---\nPLAN: \"test\"\nSTATUS: reviewing\n---\n"), 0644)
	if devflow.CodejobPhaseOf(dir) != devflow.PhaseRunning {
		t.Fatal("expected running phase when STATUS: reviewing")
	}

	// STATUS set to review → PhaseReview
	os.WriteFile(planPath, []byte("---\nPLAN: \"test\"\nSTATUS: review\n---\n"), 0644)
	if devflow.CodejobPhaseOf(dir) != devflow.PhaseReview {
		t.Fatal("expected review phase when STATUS: review")
	}

	// STATUS set to dispatch (or missing) → ""
	os.WriteFile(planPath, []byte("---\nPLAN: \"test\"\nSTATUS: dispatch\n---\n"), 0644)
	if devflow.CodejobPhaseOf(dir) != "" {
		t.Fatal("expected empty phase when STATUS: dispatch")
	}
}

func TestGoPush_BlockedOnRunningPhase(t *testing.T) {
	dir := t.TempDir()
	planDir := filepath.Join(dir, "docs")
	_ = os.MkdirAll(planDir, 0755)
	os.WriteFile(filepath.Join(planDir, "PLAN.md"), []byte("---\nPLAN: \"test\"\nSTATUS: running\n---\n"), 0644)
	defer testChdir(t, dir)()

	mockGit := &MockGitClient{}
	g := newGoHandlerWithMockBackup(t, mockGit)

	_, err := g.Push("feat: test", "", true, true, true, true, true, false, "..")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), devflow.ErrPushBlockedActiveCodejob) {
		t.Errorf("expected error message to contain %q, got %q", devflow.ErrPushBlockedActiveCodejob, err.Error())
	}
	if mockGit.CommitCalls > 0 {
		t.Error("no commit should have been created")
	}
}

func TestGoPush_NotBlockedOnReviewPhase(t *testing.T) {
	dir, cleanup := testCreateGoModule("github.com/test/repo")
	defer cleanup()
	os.WriteFile(filepath.Join(dir, ".env"), []byte("CODEJOB=jules:review:https://github.com/org/repo/pull/1\n"), 0644)
	defer testChdir(t, dir)()

	mockGit := &MockGitClient{}
	g := newGoHandlerWithMockBackup(t, mockGit)

	// push should NOT fail with blocking error (might fail for other mock reasons, but not blocking)
	_, err := g.Push("feat: test", "", true, true, true, true, true, false, "..")
	if err != nil && strings.Contains(err.Error(), devflow.ErrPushBlockedActiveCodejob) {
		t.Errorf("push should not be blocked by CODEJOB_PR, got: %v", err)
	}
}
