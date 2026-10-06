package devflow_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"webtyp.com/command"
	"webtyp.com/devflow"
)

func submoduleWithWasmSuite(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	// Root go.mod and source
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/fx\n\ngo 1.25.2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fx.go"), []byte("package fx\n\nfunc F() int { return 1 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Submodule tests/
	subDir := filepath.Join(dir, "tests")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}

	subMod := "module example.com/fx/tests\n\ngo 1.25.2\n\nrequire example.com/fx v0.0.0\nreplace example.com/fx => ..\n"
	if err := os.WriteFile(filepath.Join(subDir, "go.mod"), []byte(subMod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "go.sum"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	nativeTest := "package tests\n\nimport (\n\t\"testing\"\n\t\"example.com/fx\"\n)\n\nfunc TestNative(t *testing.T) {\n\tif fx.F() != 1 {\n\t\tt.Fatal(\"fail\")\n\t}\n}\n"
	if err := os.WriteFile(filepath.Join(subDir, "native_test.go"), []byte(nativeTest), 0o644); err != nil {
		t.Fatal(err)
	}

	wasmTest := "//go:build wasm\n\npackage tests\n\nimport \"testing\"\n\nfunc TestFront(t *testing.T) {}\n"
	if err := os.WriteFile(filepath.Join(subDir, "front_test.go"), []byte(wasmTest), 0o644); err != nil {
		t.Fatal(err)
	}

	return dir
}

func TestSubmoduleWasmSuiteIsRun(t *testing.T) {
	origPath := os.Getenv("PATH")
	t.Setenv("PATH", "/usr/local/go/bin:"+origPath)
	fixture := submoduleWithWasmSuite(t)

	g, err := devflow.NewGo(nil)
	if err != nil {
		t.Fatalf("devflow.NewGo failed: %v", err)
	}
	g.SetRootDir(fixture)

	type recordedRun struct {
		dir  string
		args []string
	}
	var runs []recordedRun

	origCmd := devflow.GoTestCmdFn
	origExec := command.Exec
	t.Cleanup(func() {
		devflow.GoTestCmdFn = origCmd
		command.Exec = origExec
	})

	devflow.GoTestCmdFn = func(ctx context.Context, dir, name string, args ...string) *exec.Cmd {
		runs = append(runs, recordedRun{dir: dir, args: append([]string(nil), args...)})
		return exec.Command("true")
	}

	command.Exec = func(name string, args ...string) *exec.Cmd {
		if name == "go" {
			if len(args) > 0 && args[0] == "list" {
				return origExec(name, args...)
			}
			return exec.Command("true")
		}
		return origExec(name, args...)
	}

	_, _ = g.Test(devflow.TestOptions{NoCache: true})

	subDir := filepath.Join(fixture, "tests")
	found := false
	for _, r := range runs {
		if r.dir == subDir {
			for _, arg := range r.args {
				if arg == "-exec" {
					found = true
					break
				}
			}
		}
	}

	if !found {
		t.Errorf("expected WASM run (with -exec) inside submodule directory %s, but none was recorded in runs: %+v", subDir, runs)
	}
}
