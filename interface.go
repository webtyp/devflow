package devflow

import "webtyp.com/git"

// FolderWatcher defines interface for adding/removing directories to watch
type FolderWatcher interface {
	AddDirectoriesToWatch(paths ...string) error
	RemoveDirectoriesFromWatcher(paths ...string) error
}

// Publisher defines the interface for publishing code changes.
type Publisher interface {
	Publish(message, tag string, skipTests, skipRace, skipDependents, skipBackup, skipTag, skipVerify bool) (git.PushResult, error)
}

// CodeJobDriver defines the contract for an external AI coding agent.
// Implementations: JulesDriver, (future: OllamaDriver, etc.)
// title is the human-readable job name (e.g. "owner/repo"), derived by CodeJob.
type CodeJobDriver interface {
	Name() string
	SetLog(fn func(...any))
	Send(prompt, title string) (string, error)
}

// SessionProvider is implemented by CodeJobDrivers that return a session ID
// after a successful Send(). CodeJob uses this to persist to .env.
type SessionProvider interface {
	SessionID() string
}

// GoModInterface defines interface for go.mod handling
type GoModInterface interface {
	NewFileEvent(fileName, extension, filePath, event string) error
	SetFolderWatcher(watcher FolderWatcher)
	Name() string
	SupportedExtensions() []string
	MainInputFileRelativePath() string
	UnobservedFiles() []string
	SetLog(fn func(...any))
	SetRootDir(path string)
	GetReplacePaths() ([]ReplaceEntry, error)
}

// BackupRunner defines the interface for backup operations.
// Allows mocking in tests to prevent real backup execution.
type BackupRunner interface {
	SetLog(fn func(...any))
	SetCommand(command string) error
	GetCommand() (string, error)
	Run() (string, error)
}
