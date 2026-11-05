//go:build !windows

// Unix only: these tests start real `sleep`/`true` processes through sh and
// check their process groups; the Windows path (cmd.exe + taskkill) has no
// equivalent here.

package devflow_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"webtyp.com/devflow"
)

// isolateBackup points HOME (the .bashrc fallback) and TMPDIR (the pid file)
// at temp dirs, so a test never reads the real ~/.bashrc nor sees a real backup.
func isolateBackup(t *testing.T) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	t.Setenv("DEV_BACKUP", "")
	t.Setenv("DEV_BACKUP_TARGET", "")
	return filepath.Join(tmp, "devbackup.pid")
}

func readBackupPid(t *testing.T, pidPath string) int {
	t.Helper()
	data, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatalf("expected a pid file for the backup in flight: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatalf("pid file holds %q: %v", data, err)
	}
	return pid
}

func groupAlive(pid int) bool { return syscall.Kill(-pid, 0) == nil }

func TestDevBackup_DiskNotConnected_WarnsAndStartsNothing(t *testing.T) {
	pidPath := isolateBackup(t)
	missing := filepath.Join(t.TempDir(), "backup-disk")
	t.Setenv("DEV_BACKUP", "sleep 30")
	t.Setenv("DEV_BACKUP_TARGET", missing)

	msg, err := devflow.NewDevBackup().Run()
	if err != nil {
		t.Fatalf("a missing disk is a warning, not an error: %v", err)
	}
	if !strings.Contains(msg, "Backup disk not connected") || !strings.Contains(msg, missing) {
		t.Errorf("expected the disk warning naming %s, got %q", missing, msg)
	}
	if _, err := os.Stat(pidPath); err == nil {
		t.Errorf("no backup must start while the disk is missing")
	}
}

// Each gopush starts a backup; one still running must be stopped (its whole
// tree) and started again instead of piling up.
func TestDevBackup_RunStopsTheBackupInFlightAndStartsAgain(t *testing.T) {
	pidPath := isolateBackup(t)
	t.Setenv("DEV_BACKUP", "sleep 30")
	t.Setenv("DEV_BACKUP_TARGET", t.TempDir())
	b := devflow.NewDevBackup()

	if msg, err := b.Run(); err != nil || !strings.Contains(msg, "Backup started") {
		t.Fatalf("first run: msg=%q err=%v", msg, err)
	}
	first := readBackupPid(t, pidPath)
	t.Cleanup(func() { _ = syscall.Kill(-first, syscall.SIGKILL) })

	msg, err := b.Run()
	if err != nil || !strings.Contains(msg, "Backup restarted") {
		t.Fatalf("second run: msg=%q err=%v", msg, err)
	}
	second := readBackupPid(t, pidPath)
	t.Cleanup(func() { _ = syscall.Kill(-second, syscall.SIGKILL) })

	if second == first {
		t.Fatalf("expected a new backup process, got the same pid %d", first)
	}
	if groupAlive(first) {
		t.Errorf("the first backup (group %d) is still running after the restart", first)
	}
	if !groupAlive(second) {
		t.Errorf("the new backup (group %d) is not running", second)
	}
}

func TestDevBackup_FinishedBackupRemovesItsPidFile(t *testing.T) {
	pidPath := isolateBackup(t)
	t.Setenv("DEV_BACKUP", "true")

	if _, err := devflow.NewDevBackup().Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(pidPath); os.IsNotExist(err) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("the pid file of a finished backup is still there: %s", pidPath)
}
