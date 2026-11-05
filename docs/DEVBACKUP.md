# devbackup

Automated backup execution integrated into devflow workflow.

## Installation

```bash
go install webtyp.com/devflow/cmd/devbackup@latest
```

## Usage

```bash
# Set backup command
devbackup -s "freefilesync /path/to/config.ffs_batch"

# Set the backup disk (the folder that only exists while the disk is connected)
devbackup -t /media/$USER/backup

# Get current command and disk
devbackup -g

# Execute backup manually
devbackup

# Clear backup command / disk
devbackup -s ""
devbackup -t ""
```

### FreeFileSync Examples

**Linux/Debian:**
```bash
devbackup -s '$(command -v FreeFileSync || command -v freefilesync) $HOME/Own/Sync/SyncSettings.ffs_batch'
devbackup -t /media/$USER/backup
```

**macOS:**
```bash
devbackup -s '/Applications/FreeFileSync.app/Contents/MacOS/FreeFileSync $HOME/SyncMac/SyncSettings.ffs_batch'
devbackup -t /Volumes/backup
```

**Windows:**
```bash
devbackup -s '"/c/Program Files/FreeFileSync/FreeFileSync.exe" /c/Users/$(whoami)/SyncWin/SyncSettings.ffs_batch'
devbackup -t 'E:\backup'
```

Set `<ErrorDialog>Cancel</ErrorDialog>` in the `.ffs_batch`: with `Show`, a failed run waits
for someone to close a dialog and never exits.

## Configuration

Both settings are stored in `~/.bashrc` with markers and escaped quotes:

```bash
# START_DEVFLOW:DEV_BACKUP
export DEV_BACKUP="$(command -v FreeFileSync || command -v freefilesync) $HOME/Own/Sync/SyncSettings.ffs_batch"
# END_DEVFLOW:DEV_BACKUP
# START_DEVFLOW:DEV_BACKUP_TARGET
export DEV_BACKUP_TARGET="/media/cesar/backup"
# END_DEVFLOW:DEV_BACKUP_TARGET
```

Internal quotes are automatically escaped when saving and unescaped when reading.
Variables are set immediately in current session and persist in `.bashrc` for future sessions.

## Integration

`gopush` automatically executes backup at the end of workflow (asynchronous, non-blocking).

## Behaviour

- **One backup at a time.** Each run records the backup's process in
  `<temp dir>/devbackup.pid`. A new run first stops the one in flight (the whole process tree:
  the shell and everything it started) and then starts again, so consecutive `gopush` calls never
  pile backups up. The file is removed when the backup ends.
- **Disk not connected.** With `DEV_BACKUP_TARGET` set, a run whose target does not exist starts
  nothing and reports a warning. Without it the command always runs.

## Output

```bash
✅ Backup started
🔁 Backup restarted
⚠️ Backup disk not connected: /media/cesar/backup
```

## Exit Codes

- `0` - Success (a missing disk is a warning, not an error)
- `1` - Error (set/get failed, or the backup could not start)

## Notes

- Execution is asynchronous (doesn't block terminal)
- Errors are shown but don't stop workflow
- Command runs via `sh -c` (Linux/macOS, in its own process group) or `cmd.exe /S /C` (Windows);
  stopping uses the process group (Unix) or `taskkill /T` (Windows)
- User is responsible for proper command formatting
- Configuration persists across sessions and is available immediately
- FreeFileSync may show GTK warnings in background execution (this is normal)
- GUI applications run in background mode - check process/logs if uncertain about execution
