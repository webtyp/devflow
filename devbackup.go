package devflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	backupEnvVar       = "DEV_BACKUP"
	backupTargetEnvVar = "DEV_BACKUP_TARGET"
	// backupPidFile, in the OS temp dir, holds the process of the backup in
	// flight so the next run can stop it before starting again.
	backupPidFile = "devbackup.pid"

	backupStartedMsg      = "✅ Backup started"
	backupRestartedMsg    = "🔁 Backup restarted"
	backupDiskMissingWarn = "⚠️ Backup disk not connected: "
)

// DevBackup handles backup operations
type DevBackup struct {
	bashrc *Bashrc
	log    func(...any)
}

// NewDevBackup creates a new DevBackup instance
func NewDevBackup() *DevBackup {
	return &DevBackup{
		bashrc: NewBashrc(),
		log:    func(...any) {},
	}
}

// SetLog sets the logger function
func (d *DevBackup) SetLog(fn func(...any)) {
	if fn != nil {
		d.log = fn
	}
}

// SetCommand sets the backup command in .bashrc and current environment
func (d *DevBackup) SetCommand(command string) error {
	return d.setVar(backupEnvVar, command)
}

// GetCommand retrieves the backup command
// First checks environment variable, then falls back to .bashrc
func (d *DevBackup) GetCommand() (string, error) {
	return d.getVar(backupEnvVar)
}

// SetTarget sets the backup disk: a folder that only exists while the disk is
// connected (/media/<user>/backup, /Volumes/backup, E:\backup). Empty clears it.
func (d *DevBackup) SetTarget(path string) error {
	return d.setVar(backupTargetEnvVar, path)
}

// GetTarget retrieves the backup disk, environment first, then .bashrc.
func (d *DevBackup) GetTarget() (string, error) {
	return d.getVar(backupTargetEnvVar)
}

// setVar saves key to .bashrc for persistence and updates the current
// process environment for immediate use; an empty value removes it.
func (d *DevBackup) setVar(key, value string) error {
	if err := d.bashrc.Set(key, value); err != nil {
		return err
	}
	if value == "" {
		return os.Unsetenv(key)
	}
	return os.Setenv(key, value)
}

func (d *DevBackup) getVar(key string) (string, error) {
	if v := os.Getenv(key); v != "" {
		return v, nil
	}
	return d.bashrc.Get(key)
}

// Run executes the backup command asynchronously and returns a message for the
// summary, or "" when no command is configured.
//
// Only one backup runs at a time: the one still in flight from a previous run
// is stopped (its whole process tree) before the new one starts. When a
// backup disk is configured and not connected, nothing starts and the message
// is a warning, not an error.
func (d *DevBackup) Run() (string, error) {
	backupCmd, err := d.GetCommand()
	if err != nil || backupCmd == "" {
		// Not configured, silent skip
		return "", nil
	}

	if target, err := d.GetTarget(); err == nil && target != "" {
		if _, statErr := os.Stat(target); statErr != nil {
			return backupDiskMissingWarn + target, nil
		}
	}

	pidPath := filepath.Join(os.TempDir(), backupPidFile)
	restarted := stopBackupInFlight(pidPath)

	pid, err := startBackup(backupCmd, pidPath)
	if err != nil {
		return "", fmt.Errorf("failed to start backup: %w", err)
	}
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(pid)), 0o600); err != nil {
		return "", fmt.Errorf("backup started but its pid file could not be written: %w", err)
	}

	if restarted {
		return backupRestartedMsg, nil
	}
	return backupStartedMsg, nil
}

// stopBackupInFlight stops the backup recorded in pidPath, if it is still
// running, and reports whether it was.
func stopBackupInFlight(pidPath string) bool {
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return false
	}
	_ = os.Remove(pidPath)
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return false
	}
	return killBackupTree(pid)
}
