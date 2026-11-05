---
PLAN: "fix: Install no longer kills running instances of the binary it installs"
EXECUTOR: jules
REVIEWER: none
---

> Read AGENTS.md at the repo root before any change.
> **Finish by yourself:** when the delivery check passes, commit, push and open the pull request. Never ask whether to
> proceed or finalize — nobody answers, and a session that waits ends without your work.

# devflow — `Install` never kills the running binary

## Why

`(*Go).Install` (`go_handler.go:704`) calls `gorun.StopApp(cmd)` before `go install ./cmd/<cmd>`. `StopApp` kills
every process **named** `<cmd>`. Publishing `webtyp/app` installs `cmd/webtyp`, so it killed the running
`webtyp` daemon: the agent's MCP tools answered `ECONNREFUSED` and it had no way back (2026-11-05). The daemon now
watches its own executable and relaunches itself when it changes, so the install must leave it running.

Why the kill existed: on Windows a running `.exe` cannot be overwritten, so `go install` fails. Windows does allow
**renaming** a running `.exe`, so the old file is moved aside instead of killing the process. On Linux and macOS
`go install` replaces the file with a rename and a running process is unaffected.

## Tools

```bash
go install webtyp.com/devflow/cmd/gotest@latest
```

## Decisions (no judgment needed)

- Delete the `gorun.StopApp(cmd)` line and the `webtyp.com/gorun` import from `go_handler.go` if nothing else in the
  file uses it (`grep -n "gorun\." go_handler.go` → empty), then `go mod tidy`.
- New file `install_target.go` with `moveRunningBinaryAside(installDir, pkg string) error`, called in the loop **in
  place of** the deleted line, after `installDir` and `pkg` are computed (move the call below the `if _, err :=
  os.Stat(filepath.Join(cmdDir, "go.mod"))` block). An error is returned from `Install` wrapped as
  `fmt.Errorf("failed to install %s: %w", cmd, err)`.
- Only on Windows it does anything; on other OS it returns nil immediately.

## Stage 1 — `install_target.go` (package `devflow`)

```go
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
	windowsGOOS  = "windows"
	asideSuffix  = ".old"
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
```

## APIs used

| Symbol | Signature | File |
|---|---|---|
| `command.RunInDir` | `func RunInDir(dir, name string, args ...string) (string, error)` | `webtyp.com/command` (already imported by `go_handler.go`) |

If a signature in the published tag differs from this table, stop and name the symbol in the PR; never adapt it by guessing.

## Stage 2 — test `tests/install_running_binary_test.go`

| Test | Setup | Call | Expected |
|---|---|---|---|
| `TestInstall_DoesNotKillRunningBinary` | temp module `example.com/sleeper` with `cmd/sleeper/main.go` = `package main; import "time"; func main(){ time.Sleep(time.Minute) }`; `GOBIN=t.TempDir()` via `t.Setenv`; run `go install ./cmd/sleeper` once; start `$GOBIN/sleeper` with `exec.Command(...).Start()` | build a `*devflow.Go` for that root the way the existing Install tests do (find them with `grep -rn "Install(" tests/`) and call `Install("")` | returns nil; the started process is still alive (`proc.Signal(syscall.Signal(0)) == nil`); cleanup `proc.Kill()`. Skip on windows (`t.Skip`) |

## Stage 3 — documentation

In the doc that describes `Install` (find it with `grep -rln "Install" README.md docs/`), replace any sentence that
says running instances are stopped with: «`Install` never stops running instances: on Windows the running `.exe` is
renamed to `<name>.old` so the new one can be written; a program that must pick up the new binary watches its own
executable (the webtyp daemon does).»

## Delivery check

```bash
gotest
grep -rn "StopApp" --include=*.go .   # → empty
```

A PR that does not compile, or whose tests could not run, is a failed delivery. Environment:
`GOPRIVATE=webtyp.com/*,github.com/veltylabs/*`, `GOFLAGS=-mod=mod`. Commit after every stage; never run
`git reset`, `git restore`, `git checkout -- <file>` or `git stash` on your own work.

## Stages

| # | Files | Done when |
|---|---|---|
| 1 | `install_target.go`, `go_handler.go`, `go.mod`, `go.sum` | builds; `StopApp` gone |
| 2 | `tests/install_running_binary_test.go` | passes |
| 3 | docs | sentence updated |
