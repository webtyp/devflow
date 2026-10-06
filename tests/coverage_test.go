package devflow_test

import "webtyp.com/devflow"

import (
	"context"
	"webtyp.com/command"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestGoPushFlags(t *testing.T) {
	dir, cleanup := testCreateGoModule("github.com/test/repo")
	defer cleanup()
	defer testChdir(t, dir)()

	// Mock ExecCommand (go vet/go tool cover) and GoTestCmdFn (go test) so this
	// test exercises Push()'s flag dispatch without paying for 3 real compiles —
	// one of them with -race, which was the dominant cost (~5-6s of this test's time).
	originalExec := command.Exec
	defer func() { command.Exec = originalExec }()
	command.Exec = func(name string, args ...string) *exec.Cmd {
		if name == "go" && len(args) > 0 && (args[0] == "vet" || args[0] == "tool") {
			return exec.Command("true")
		}
		return originalExec(name, args...)
	}
	originalGoTestCmdFn := devflow.GoTestCmdFn
	defer func() { devflow.GoTestCmdFn = originalGoTestCmdFn }()
	devflow.GoTestCmdFn = func(ctx context.Context, dir, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "true")
	}

	// Use MockGitClient
	mockGit := &MockGitClient{
		latestTag: "v0.0.0",
		log:       func(args ...any) {},
	}

	goHandler := newGoHandlerWithMockBackup(t, mockGit)

	// 1. Skip Tests and Skip Race
	// Mock returns "Mock push ok"
	result, err := goHandler.Push("msg", "v0.0.1", true, true, false, false, false, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary == "" {
		t.Error("Empty summary")
	}

	// 2. Run Tests, Skip Race
	// Create dummy test
	testContent := `package main
import "testing"
func TestExample(t *testing.T) {}
`
	os.WriteFile("main_test.go", []byte(testContent), 0644)

	_, err = goHandler.Push("msg", "v0.0.2", false, true, false, false, false, false, "")
	if err != nil {
		t.Fatal(err)
	}

	// 3. Run Tests, Run Race
	_, err = goHandler.Push("msg", "v0.0.3", false, false, false, false, false, false, "")
	if err != nil {
		t.Fatal(err)
	}
}

func TestGoUpdateDependentsNoSearchPath(t *testing.T) {
	// Test that default search path ".." works (or at least is used)
	// We can just call it in a dir structure where .. has nothing relevant
	dir, cleanup := testCreateGoModule("github.com/test/repo")
	defer cleanup()

	defer testChdir(t, dir)()

	// Use MockGitClient
	mockGit := &MockGitClient{
		log: func(args ...any) {},
	}

	goHandler, err := devflow.NewGo(mockGit)
	if err != nil {
		t.Fatal(err)
	}

	// It should not fail, just find nothing
	if err := goHandler.UpdateDependents("github.com/test/repo", "v0.0.1", ""); err != nil {
		t.Fatal(err)
	}
}

func TestGoFailures(t *testing.T) {
	dir, cleanup := testCreateGoModule("github.com/test/repo")
	defer cleanup()
	defer testChdir(t, dir)()

	mockGit := &MockGitClient{
		log: func(args ...any) {},
	}
	goHandler := newGoHandlerWithMockBackup(t, mockGit)

	// Test Verify Failure (delete go.mod)
	os.Remove("go.mod")
	err := goHandler.Verify()
	if err == nil {
		t.Error("Expected verify to fail when go.mod is missing")
	}

	// Restore go.mod for next steps
	os.WriteFile("go.mod", []byte("module github.com/test/repo\n\ngo 1.20\n"), 0644)

	// Test GetModulePath Failure (corrupt go.mod)
	os.WriteFile("go.mod", []byte("invalid content"), 0644)
	_, err = goHandler.GetModulePath()
	if err == nil {
		t.Error("Expected getModulePath to fail with invalid content")
	}
}

func TestGoUpdateModuleFail(t *testing.T) {
	dir, cleanup := testCreateGoModule("github.com/test/repo")
	defer cleanup()
	defer testChdir(t, dir)()

	mockGit := &MockGitClient{
		log: func(args ...any) {},
	}
	goHandler := newGoHandlerWithMockBackup(t, mockGit)
	goHandler.SetRetryConfig(10*time.Millisecond, 2)

	// Try to update a module in current dir (which is not a valid dependent in this context, or just fails `go get`)
	// We try to run updateModule on the current directory for a non-existent dependency

	err := goHandler.UpdateModule(".", "github.com/nonexistent/dep", "v1.0.0")
	if err == nil {
		t.Error("Expected updateModule to fail")
	}
}

func TestGoPushFailTest(t *testing.T) {
	dir, cleanup := testCreateGoModule("github.com/test/repo")
	defer cleanup()
	defer testChdir(t, dir)()

	mockGit := &MockGitClient{
		log: func(args ...any) {},
	}
	goHandler := newGoHandlerWithMockBackup(t, mockGit)
	goHandler.SetConsoleOutput(func(string) {}) // suppress subprocess output

	// Create failing test
	testContent := `package main
import "testing"
func TestFail(t *testing.T) { t.Fatal("fail") }
`
	os.WriteFile("main_test.go", []byte(testContent), 0644)

	_, err := goHandler.Push("msg", "", false, false, false, false, false, false, "")
	if err == nil {
		t.Error("Expected Push to fail due to failed tests")
	}
}

// Add one more test case for edge cases in executor
func TestExecutorErrors(t *testing.T) {
	// Run invalid command
	_, err := command.Run("invalid_command_xyz")
	if err == nil {
		t.Error("Expected error for invalid command")
	}
}
