package devflow_test

import (
	"context"
	"webtyp.com/command"
	gitmod "webtyp.com/git"
	"os/exec"
	"testing"

	"webtyp.com/devflow"
	"webtyp.com/mcp"
)

func TestGoTestProvider(t *testing.T) {
	dir, cleanup := testCreateGoModule("example.com/mcptest")
	defer cleanup()

	git, _ := gitmod.NewGit()
	g, _ := devflow.NewGo(git)
	g.SetRootDir(dir)

	provider := devflow.NewGoTestProvider(g)

	var capturedDir string
	var capturedArgs []string

	originalGoTestCmdFn := devflow.GoTestCmdFn
	defer func() { devflow.GoTestCmdFn = originalGoTestCmdFn }()

	devflow.GoTestCmdFn = func(ctx context.Context, dir string, name string, args ...string) *exec.Cmd {
		capturedDir = dir
		capturedArgs = args
		return exec.Command("true")
	}

	// Mock ExecCommand to make go vet instant — avoids non-deterministic slowness
	// under load that can push the test past the 30s binary timeout.
	originalExec := command.Exec
	defer func() { command.Exec = originalExec }()
	command.Exec = func(name string, args ...string) *exec.Cmd {
		if name == "go" && len(args) > 0 && args[0] == "vet" {
			return exec.Command("true")
		}
		return originalExec(name, args...)
	}

	tools := provider.Tools()
	if len(tools) != 1 || tools[0].Name != "run_tests" {
		t.Fatalf("Expected 1 tool named run_tests")
	}

	call := func(args string) (*mcp.Result, error) {
		return tools[0].Execute(nil, mcp.Request{
			Params: mcp.CallToolParams{Arguments: args},
		})
	}

	t.Run("Full Suite (no args)", func(t *testing.T) {
		capturedDir = ""
		capturedArgs = nil

		result, err := call("")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if result == nil {
			t.Fatal("Expected non-nil result")
		}
		if capturedDir != dir {
			t.Errorf("Expected dir %s, got %q", dir, capturedDir)
		}
		for _, arg := range capturedArgs {
			if arg == "-run" {
				t.Error("Full suite should not have -run arg")
			}
		}
	})

	t.Run("Single Test (run arg)", func(t *testing.T) {
		capturedDir = ""
		capturedArgs = nil

		result, err := call(`{"run":"TestFoo"}`)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if result == nil {
			t.Fatal("Expected non-nil result")
		}
		if capturedDir != dir {
			t.Errorf("Expected dir %s, got %q", dir, capturedDir)
		}
		foundRun := false
		for i, arg := range capturedArgs {
			if arg == "-run" && i+1 < len(capturedArgs) && capturedArgs[i+1] == "TestFoo" {
				foundRun = true
				break
			}
		}
		if !foundRun {
			t.Errorf("Missing -run TestFoo in args: %v", capturedArgs)
		}
	})

}
