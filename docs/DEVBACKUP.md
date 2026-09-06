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

# Get current command
devbackup -g

# Execute backup manually
devbackup

# Clear backup command
devbackup -s ""
```

### FreeFileSync Examples

**Linux/Debian:**
```bash
devbackup -s '$(command -v FreeFileSync || command -v freefilesync) $HOME/Own/Sync/SyncSettings.ffs_batch'
```

**Windows:**
```bash
devbackup -s '"/c/Program Files/FreeFileSync/FreeFileSync.exe" /c/Users/$(whoami)/SyncWin/SyncSettings.ffs_batch'
```

## Configuration

The backup command is stored in `~/.bashrc` with markers and escaped quotes:

```bash
# START_DEVFLOW:DEV_BACKUP
export DEV_BACKUP="$(command -v FreeFileSync || command -v freefilesync) $HOME/Own/Sync/SyncSettings.ffs_batch"
# END_DEVFLOW:DEV_BACKUP
```

Internal quotes are automatically escaped when saving and unescaped when reading.
Variable is set immediately in current session and persists in `.bashrc` for future sessions.

## Integration

`gopush` automatically executes backup at the end of workflow (asynchronous, non-blocking).

## Output

```bash
✅ Backup started
```

If backup fails (async error):
```bash
❌ Backup failed: exit status 1
Output: error details
```

## Exit Codes

- `0` - Success
- `1` - Error (set/get failed)

## Notes

- Execution is asynchronous (doesn't block terminal)
- Errors are shown but don't stop workflow
- Command runs via `sh -c` (Linux/macOS) or `cmd.exe /C` (Windows)
- User is responsible for proper command formatting
- Configuration persists across sessions and is available immediately
- FreeFileSync may show GTK warnings in background execution (this is normal)
- GUI applications run in background mode - check process/logs if uncertain about execution
