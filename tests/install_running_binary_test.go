package devflow_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
)

func TestInstall_DoesNotKillRunningBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows because starting and killing a process requires different setup for exe replacement")
	}

	tmpDir := t.TempDir()

	// temp module example.com/sleeper with cmd/sleeper/main.go
	os.MkdirAll(filepath.Join(tmpDir, "cmd", "sleeper"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "cmd", "sleeper", "main.go"), []byte(`package main; import "time"; func main(){ time.Sleep(time.Minute) }`), 0644)
	os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(`module example.com/sleeper

go 1.21`), 0644)

	// GOBIN=t.TempDir() via t.Setenv
	gobin := t.TempDir()
	t.Setenv("GOBIN", gobin)

	// run go install ./cmd/sleeper once
	installCmd := exec.Command("go", "install", "./cmd/sleeper")
	installCmd.Dir = tmpDir
	installCmd.Env = append(os.Environ(), "GOBIN="+gobin)
	if out, err := installCmd.CombinedOutput(); err != nil {
		t.Fatalf("First go install failed: %v\nOutput: %s", err, string(out))
	}

	// start $GOBIN/sleeper
	sleeperPath := filepath.Join(gobin, "sleeper")
	if runtime.GOOS == "windows" {
		sleeperPath += ".exe"
	}
	procCmd := exec.Command(sleeperPath)
	if err := procCmd.Start(); err != nil {
		t.Fatalf("Failed to start sleeper process: %v", err)
	}

	// cleanup proc.Kill()
	t.Cleanup(func() {
		procCmd.Process.Kill()
	})

	// build a *devflow.Go for that root
	mockGit := &MockGitClient{}
	g := newGoHandlerWithMockBackup(t, mockGit)
	g.SetRootDir(tmpDir)

	// call Install("")
	if err := g.Install(""); err != nil {
		t.Fatalf("Install returned error: %v", err)
	}

	// Expected: returns nil; the started process is still alive
	if err := procCmd.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("Started process is not alive after Install: %v", err)
	}
}
