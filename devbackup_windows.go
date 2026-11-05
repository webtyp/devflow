//go:build windows

package devflow

import (
	"os/exec"
	"strconv"
	"syscall"
)

// createNewProcessGroup is CREATE_NEW_PROCESS_GROUP: the backup does not share
// the caller's console group, so a Ctrl+C there does not reach it.
const createNewProcessGroup = 0x00000200

// startBackup runs commandLine through cmd.exe. When the command ends, cmd
// deletes pidPath. /S keeps cmd from second-guessing the outer quotes, so the
// line is passed through verbatim (CmdLine bypasses Go's argument escaping,
// which cmd.exe does not follow).
func startBackup(commandLine, pidPath string) (int, error) {
	cmd := exec.Command("cmd.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CmdLine:       `cmd.exe /S /C "` + commandLine + ` & del /Q "` + pidPath + `" 2>nul"`,
		CreationFlags: createNewProcessGroup,
	}
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	pid := cmd.Process.Pid
	go func() { _ = cmd.Wait() }()
	return pid, nil
}

// killBackupTree stops pid and every process it started (taskkill /T). Reports
// whether it was running: taskkill fails when there is no such process.
func killBackupTree(pid int) bool {
	return exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(pid)).Run() == nil
}
