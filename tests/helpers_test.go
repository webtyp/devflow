package devflow_test

import (
	gitmod "webtyp.com/git"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"webtyp.com/devflow"
)

// testChdir changes to the specified directory and returns a cleanup function.
// If chdir fails, it calls t.Fatal.
func testChdir(t *testing.T, dir string) func() {
	t.Helper()
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current dir: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to chdir to %s: %v", dir, err)
	}
	return func() {
		os.Chdir(oldDir)
	}
}

// testCreateGitRepo creates a temporary Git repo for tests
// For internal use in tests only
func testCreateGitRepo() (dir string, cleanup func()) {
	dir, _ = os.MkdirTemp("", "gitgo-test-")

	// Init git
	exec.Command("git", "-C", dir, "init").Run()
	exec.Command("git", "-C", dir, "config", "user.name", "Test").Run()
	exec.Command("git", "-C", dir, "config", "user.email", "test@test.com").Run()

	cleanup = func() {
		os.RemoveAll(dir)
	}

	return dir, cleanup
}

// testCreateGoModule creates a temporary Go module
func testCreateGoModule(moduleName string) (dir string, cleanup func()) {
	dir, _ = os.MkdirTemp("", "gitgo-gomod-")

	// Create go.mod
	gomod := "module " + moduleName + "\n\ngo 1.20\n"
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0644)

	// Create main.go
	main := "package main\n\nfunc main() {}\n"
	os.WriteFile(filepath.Join(dir, "main.go"), []byte(main), 0644)

	cleanup = func() {
		os.RemoveAll(dir)
	}

	return dir, cleanup
}

// MockPublisher for testing
type MockPublisher struct {
	PublishFn func(message, tag string, skipTests, skipRace, skipDependents, skipBackup, skipTag, skipVerify bool) (gitmod.PushResult, error)
}

func (m *MockPublisher) Publish(message, tag string, skipTests, skipRace, skipDependents, skipBackup, skipTag, skipVerify bool) (gitmod.PushResult, error) {
	if m.PublishFn != nil {
		return m.PublishFn(message, tag, skipTests, skipRace, skipDependents, skipBackup, skipTag, skipVerify)
	}
	return gitmod.PushResult{Summary: "Mock published"}, nil
}

// newGoHandlerWithMockBackup creates a Go handler with a no-op backup mock.
func newGoHandlerWithMockBackup(t *testing.T, git gitmod.GitClient) *devflow.Go {
	t.Helper()
	h, err := devflow.NewGo(git)
	if err != nil {
		t.Fatalf("NewGo: %v", err)
	}
	h.SetBackup(&devflow.MockDevBackup{})
	return h
}
