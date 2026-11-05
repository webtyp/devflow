package devflow

import (
	"os"
	"runtime"
	"strings"

	"webtyp.com/command"
)

// goListTarget prints the path `go install` writes for a package.
var goListTarget = []string{"list", "-f", "{{.Target}}"}

const (
	windowsGOOS = "windows"
	asideSuffix = ".old"
)

// moveRunningBinaryAside lets `go install` replace a binary that is running.
// Windows refuses to overwrite a running .exe but allows renaming it, so the
// current file is moved to <target>.old (a previous .old is removed first).
// Linux and macOS replace the file by rename and need nothing.
func moveRunningBinaryAside(installDir, pkg string) error {
	if runtime.GOOS != windowsGOOS {
		return nil
	}
	out, err := command.RunInDir(installDir, "go", append(goListTarget, pkg)...)
	if err != nil {
		return err
	}
	target := strings.TrimSpace(out)
	if target == "" {
		return nil // nothing installed yet
	}
	if _, err := os.Stat(target); os.IsNotExist(err) {
		return nil
	}
	_ = os.Remove(target + asideSuffix) // a leftover from the previous install; absent is fine
	return os.Rename(target, target+asideSuffix)
}
