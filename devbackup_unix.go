//go:build !windows

package devflow

import (
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// backupStopGrace is how long a stopped backup gets to exit on SIGTERM before
// its process group is killed.
const backupStopGrace = 2 * time.Second

// startBackup runs commandLine through sh in a process group of its own, so the
// whole tree (the shell and whatever it launched) can be stopped at once. When
// the command ends, the shell removes pidPath — only if it still names this
// run, so a backup ending late never erases the next one's record.
func startBackup(commandLine, pidPath string) (int, error) {
	p := shellQuote(pidPath)
	script := commandLine + "\n" + `[ "$(cat ` + p + ` 2>/dev/null)" = "$$" ] && rm -f ` + p
	cmd := exec.Command("sh", "-c", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	pid := cmd.Process.Pid
	// Reap the shell when it exits so a long-lived caller keeps no zombie.
	go func() { _ = cmd.Wait() }()
	return pid, nil
}

// killBackupTree stops the process group led by pid: SIGTERM, then SIGKILL if
// it is still there after backupStopGrace. Reports whether it was running.
func killBackupTree(pid int) bool {
	if syscall.Kill(-pid, 0) != nil {
		return false
	}
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	deadline := time.Now().Add(backupStopGrace)
	for time.Now().Before(deadline) {
		if syscall.Kill(-pid, 0) != nil {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	return true
}

// shellQuote single-quotes s for sh.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
