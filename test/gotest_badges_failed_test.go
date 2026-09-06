package devflow_test

import (
	"context"
	"webtyp.com/command"
	gitmod "webtyp.com/git"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"webtyp.com/devflow"
)

func TestGotest_BadgesOnlyOnSuccess(t *testing.T) {
	// 1. Setup a temporary Go module
	tmpDir, cleanup := testCreateGoModule("testbadges")
	defer cleanup()

	// Mock ExecCommand to make go vet/go tool cover instant — this test only
	// cares about the pass/fail gating of badge updates, not real vet/coverage output.
	originalExec := command.Exec
	defer func() { command.Exec = originalExec }()
	command.Exec = func(name string, args ...string) *exec.Cmd {
		if name == "go" && len(args) > 0 && (args[0] == "vet" || args[0] == "tool") {
			return exec.Command("true")
		}
		return originalExec(name, args...)
	}

	// Mock GoTestCmdFn to avoid a real `go test -cover -coverpkg=./...` compile:
	// that real subprocess took ~1-9s per call (2 calls in this test) and was the
	// main source of slowness/flakiness under load. We only need to simulate the
	// pass/fail exit status that runFullTestSuite reacts to.
	shouldFail := true
	originalGoTestCmdFn := devflow.GoTestCmdFn
	defer func() { devflow.GoTestCmdFn = originalGoTestCmdFn }()
	devflow.GoTestCmdFn = func(ctx context.Context, dir, name string, args ...string) *exec.Cmd {
		if shouldFail {
			return exec.CommandContext(ctx, "false")
		}
		return exec.CommandContext(ctx, "true")
	}

	// Add a README with badge markers
	readmePath := filepath.Join(tmpDir, "README.md")
	readmeContent := "# Test Module\n\n<!-- START_SECTION:BADGES_SECTION -->\n<!-- END_SECTION:BADGES_SECTION -->\n"
	if err := os.WriteFile(readmePath, []byte(readmeContent), 0644); err != nil {
		t.Fatalf("Failed to write README: %v", err)
	}

	// Add a failing test
	failTest := "package main\nimport \"testing\"\nfunc TestFail(t *testing.T) { t.Fatal(\"fail\") }\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "fail_test.go"), []byte(failTest), 0644); err != nil {
		t.Fatalf("Failed to write failing test: %v", err)
	}

	git, err := gitmod.NewGit()
	if err != nil {
		t.Fatalf("Failed to create git handler: %v", err)
	}
	g, err := devflow.NewGo(git)
	if err != nil {
		t.Fatalf("Failed to create go handler: %v", err)
	}
	g.SetRootDir(tmpDir)
	g.SetLog(t.Log)

	// 2. Run tests - should FAIL
	_, err = g.Test(nil, true, 5, true, false)
	if err == nil {
		t.Fatal("Expected Test() to return error for failing tests")
	}

	// 3. Verify README and badges.svg were NOT updated
	afterFailContent, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("Failed to read README after failure: %v", err)
	}
	if string(afterFailContent) != readmeContent {
		t.Errorf("README was modified even though tests failed.\nGot:\n%s\nWant:\n%s", string(afterFailContent), readmeContent)
	}

	svgPath := filepath.Join(tmpDir, "docs/img/badges.svg")
	if _, err := os.Stat(svgPath); err == nil {
		t.Error("docs/img/badges.svg was created even though tests failed")
	}

	// 4. Fix the test to PASS
	passTest := "package main\nimport \"testing\"\nfunc TestPass(t *testing.T) { }\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "fail_test.go"), []byte(passTest), 0644); err != nil {
		t.Fatalf("Failed to fix test: %v", err)
	}
	shouldFail = false

	// 5. Run tests - should PASS
	_, err = g.Test(nil, true, 5, true, false)
	if err != nil {
		t.Fatalf("Expected Test() to succeed, got error: %v", err)
	}

	// 6. Verify README and badges.svg WERE updated
	afterPassContent, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("Failed to read README after success: %v", err)
	}
	if !strings.Contains(string(afterPassContent), "<img src=\"docs/img/badges.svg\">") {
		t.Errorf("README was not updated with badges after successful test.\nContent:\n%s", string(afterPassContent))
	}

	if _, err := os.Stat(svgPath); os.IsNotExist(err) {
		t.Error("docs/img/badges.svg was NOT created even though tests passed")
	}
}
