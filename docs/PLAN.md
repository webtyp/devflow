---
PLAN: "fix(gotest): submodules run their WASM suite too; gopush cascade reports the real failing stage and keeps the log"
EXECUTOR: jules
REVIEWER: none
STATUS: running
SESSION: 18113231280357664585
---

> This plan is dispatched via the CodeJob workflow. See skill: agents-workflow.

# Plan — `gotest` gives the same verdict wherever it is run

## Prerequisite — install the test runner

External agents run in isolated environments where `gotest` is not installed.
Run this **before anything else**; the acceptance criteria depend on it:

```bash
go install webtyp.com/devflow/cmd/gotest@latest
```

Then use `gotest` for the whole suite and `gotest -run TestName` for one test.
Never call `go test` directly.

## Problem (observed 2026-09-26)

`gopush` in `webtyp/server` cascaded to its dependents and printed:

```
📦 auth/tests → tests ❌
```

Yet `gotest` at the root of `webtyp/auth` is green. Both runs are honest; they
simply run **different suites**:

- `webtyp/auth` keeps its test suite in a nested module `tests/` (own `go.mod`).
  `FindDependentModules` (`go_mod.go`) finds `auth/tests/go.mod` directly, so
  the cascade gate (`UpdateDependentModule`, `go_handler.go`, step 6) runs
  `gotest -t 60 -no-cache` **inside `auth/tests`**. There `auth/tests` is the
  root module: WASM detection sees `user_front_test.go` (`//go:build wasm`) and
  runs the WASM suite → it fails to compile → `wasm ❌`.
- `gotest` at the root of `webtyp/auth` reaches `tests/` only through the
  submodule loop in `runFullTestSuite` (`gotest.go`, the block commented "Run
  tests in submodule directories"). That loop runs **only** a native
  `go test -race …`. WASM detection and the WASM run only ever look at
  `g.rootDir`. The submodule's WASM suite is **never run** → `auth` looks green.

Proof that the silent path hid real rot: the WASM suite of `auth/tests` no
longer compiles at all (a test file imports a build-only package without a
`!wasm` tag, and `user_front_test.go` calls `Reload`, `Save` and `Delete` with
signatures that changed long ago). That is fixed in `webtyp/auth` separately,
**not in this repo**.

Two defects in this repo made it undiagnosable:

1. **Divergent verdict** — the same code gets a different verdict depending on
   the directory `gotest` is run from.
2. **Opaque cascade failure** — `extractFirstFailure` (`go_handler.go`) maps any
   output containing `❌` to the label `tests`, although the summary line said
   `vet ✅, race ✅, tests ✅, wasm ❌`. The gate's full output is then thrown
   away, so the user has nothing to act on.

## Decision

- **D1.** A submodule found by `findSubModuleDirs` gets the **same WASM
  treatment as the root**: detection with `ShouldEnableWasm` in the submodule's
  own directory, and, if enabled, a WASM run in that directory. This applies to
  both `runFullTestSuite` and `runCustomTests`.
- **D2.** The WASM result stays **one** label when everything passes
  (`wasm ✅`). A failure names where it happened: `wasm ❌` for the root,
  `wasm <rel> ❌` for a submodule, where `<rel>` is
  `filepath.ToSlash(filepath.Rel(g.rootDir, subDir))` (e.g. `wasm tests ❌`).
- **D3.** The cascade prints **every** failing label of the gate's summary
  (`📦 auth/tests → wasm ❌`), never a generic `tests` when the summary says
  otherwise.
- **D4.** When the gate fails, the full `gotest` output is written to
  `filepath.Join(os.TempDir(), "gopush-gate-<name>.log")`, where `<name>` is the
  display name with every `/` replaced by `-` (e.g. `gopush-gate-auth-tests.log`).
  The path is printed on the same line:
  `📦 auth/tests → wasm ❌ (log: /tmp/gopush-gate-auth-tests.log)`.

**Rejected on purpose:** running the gate from the dependent's git root instead
of `depDir`. `gotest` without arguments rewrites `README.md` badges in its root
directory, which would dirty the dependent's tree between the gate and the
deps-only commit. D1 already makes both locations agree, which is the actual
requirement.

## Design gate

No exported symbol is added, removed or changed. The only observable surface
that changes is the **text of the `gotest` summary** (D2) and of the cascade
line (D3, D4):

- **Prior art:** the summary already names failing stages (`vet ❌`,
  `timeout: … ❌`); D2 extends that form to a location.
- **Novice-name test:** `wasm tests ❌` reads as "the WASM suite in tests/
  failed". No new vocabulary.
- **Complexity ledger:** +1 loop over directories, which the native path
  already has. −1 generic label that lied (`tests` for a `wasm` failure).
- **Where it belongs:** `gotest.go` (WASM detection and run) and
  `go_handler.go` (cascade gate). Nothing in `cmd/`.
- **What it deletes:** the duplicated WASM-detection block in
  `runFullTestSuite` and `runCustomTests` (it becomes one helper), and the
  `strings.Contains(output, "❌") → "tests"` shortcut in `extractFirstFailure`.

## Code quality rules (mandatory)

- **No hardcoded repeated strings.** The log file prefix is an unexported
  constant `gateLogPrefix = "gopush-gate-"` and the suffix
  `gateLogSuffix = ".log"` in `go_handler.go`.
- **This repo is backend tooling** and legitimately uses the standard library
  (`strings`, `os`, `path/filepath`, `os/exec`). Do NOT replace those imports
  with `webtyp.com/fmt` or similar — the "no stdlib" rule applies to
  WASM-compiled packages only, and devflow is never compiled to WASM.
- Tests live in `test/` (package `devflow_test`), like the existing
  `test/gotest_options_test.go`. Do not add `_test.go` files next to the source
  unless they need unexported identifiers. If one does, it goes in the root as
  `package devflow`.
- Do not change the native (stdlib) submodule run, the coverage merge, the
  watchdog or the badge logic. They are correct.

## Stage 1 — one WASM detection helper (`gotest.go`)

1. Add an unexported method:

   ```go
   // wasmEnabledIn reports whether dir has test files that only exist in the
   // js/wasm build — the same rule the root has always used.
   func (g *Go) wasmEnabledIn(dir string, runAll bool) bool
   ```

   Its body is the current anonymous detection goroutine body (native
   `go list -f "{{.ImportPath}} {{.TestGoFiles}} {{.XTestGoFiles}}" ./...` vs
   the same with `GOOS=js GOARCH=wasm`, `-tags=integration` when `runAll`), with
   `cmd.Dir = dir` instead of `g.rootDir`. It returns
   `ShouldEnableWasm(nativeOut, wasmOut)`.
2. In `runFullTestSuite`, the detection goroutine becomes
   `enableWasmTests = g.wasmEnabledIn(g.rootDir, runAll)`.
3. In `runCustomTests`, same replacement.
4. Change `wasmTestPackages(runAll bool)` to
   `wasmTestPackages(dir string, runAll bool)` and use `cmd.Dir = dir`. Update
   every call site (`grep -n "wasmTestPackages(" *.go`) to pass `g.rootDir`.

Acceptance: `grep -n '"GOOS=js", "GOARCH=wasm"' gotest.go` shows the
detection env in `wasmEnabledIn` only. The run command and `wasmTestPackages`
keep theirs. The whole suite is still green.

## Stage 2 — WASM run per directory (`gotest.go`)

1. Add an unexported type and method that run the WASM suite in **one**
   directory. Its body is the current `if enableWasmTests { … }` block of
   `runFullTestSuite`, lifted as-is, with these changes only:

   ```go
   type wasmRun struct {
       output   string   // combined go test output
       failed   bool
       timedOut []string // test names, or one "wasm tests exceeded Ns" entry
       coverage string   // calculateAverageCoverage(output), "0" if none
   }

   func (g *Go) runWasmIn(dir, coverPkg string, runAll bool, timeoutSec int) wasmRun
   ```

   - `GoTestCmdFn(wasmCtx, dir, …)` instead of `g.rootDir`.
   - `-coverpkg=` + `coverPkg` instead of the literal `-coverpkg=./...`.
   - `g.wasmTestPackages(dir, runAll)`.
   - Timeout culprit search (`findWasmTimeoutCulprit`) only when
     `dir == g.rootDir`. For a submodule, `timedOut` is
     `[]string{fmt.Sprintf("wasm tests in %s exceeded %ds", rel, timeoutSec)}`.
   - It does **not** call `addMsg` and does not touch `testStatus` /
     `coveragePercent`. The caller does.
2. In `runFullTestSuite`, replace the whole `if enableWasmTests { … }` block
   with:
   - `installWasmBrowserTest()` once, only if at least one directory needs WASM.
     On error keep the current `addMsg(false, "WASM tests skipped (setup failed)")`.
   - Directories, in order: `g.rootDir` if `enableWasmTests`, then every
     `subDir` of `findSubModuleDirs(g.rootDir)` for which
     `g.wasmEnabledIn(subDir, runAll)` is true.
   - Cover package: `"./..."` for the root, `covPkgFlag`'s value
     (`moduleName + "/..."`) for a submodule — the same target the native
     submodule run already uses.
   - For each result: append `output` to `wasmTestOutput` (with `"\n"`). For
     each `timedOut` entry emit `addMsg(false, "timeout: "+entry)`, then set
     `testStatus = "Failed"`. If `failed` without timeout, emit
     `addMsg(false, "wasm")` for the root or `addMsg(false, "wasm "+rel)` for a
     submodule, then set `testStatus = "Failed"`. Keep the existing
     "higher coverage wins" comparison against `coveragePercent` for each
     passing run.
   - After the loop: if at least one directory ran and none failed or timed out,
     emit exactly one `addMsg(true, "wasm")`, and set `testStatus = "Passing"`
     when it is not already `"Failed"` (current behavior).
3. In `runCustomTests`, apply the same per-directory loop to its WASM block.
   It passes the custom flags exactly as it does today: keep its current WASM
   argument construction and only make `dir` / `coverPkg` vary.
4. Delete the long commented-out reasoning block inside the old WASM success
   branch (the lines starting `// Try exact coverage for WASM if possible` down
   to `// The user sees 76.7% vs 89% discrepancy …`). It does not survive into
   `runWasmIn`.

Acceptance:
- `grep -n "Try exact coverage for WASM" gotest.go` → empty.
- Stage 4 tests pass.

## Stage 3 — cascade reports the real failure and keeps the log (`go_handler.go`)

1. Rewrite `extractFirstFailure(output string) string` (keep the name; it is
   also used for `go mod tidy` output):
   - Scan lines. For every line containing `❌`, strip a leading
     `"Tests failed: "`, split on `", "`, and for every segment that contains
     `" ❌"` keep the part before the first `" ❌"`
     (`"wasm tests ❌"` → `"wasm tests"`; `"wasm ❌ v0.0.56 (24.1s)"` → `"wasm"`).
   - Return the kept segments joined with `", "`, deduplicated, in order of
     appearance.
   - No segment kept → return `"failed"` (the `go mod tidy` case keeps working).
2. Add unexported constants `gateLogPrefix = "gopush-gate-"` and
   `gateLogSuffix = ".log"`, and an unexported helper:

   ```go
   // writeGateLog saves the full gate output so a failed cascade entry is
   // actionable; it returns the path, or "" if the write failed.
   func writeGateLog(depName, output string) string
   ```

   Path: `filepath.Join(os.TempDir(), gateLogPrefix+strings.ReplaceAll(depName, "/", "-")+gateLogSuffix)`,
   mode `0o644`, overwriting any previous file.
3. In `UpdateDependentModule` step 6, on gate failure:

   ```go
   cause := extractFirstFailure(output)
   line := fmt.Sprintf("📦 %s → %s ❌", depName, cause)
   if p := writeGateLog(depName, output); p != "" {
       line += fmt.Sprintf(" (log: %s)", p)
   }
   g.consoleOutput(line)
   ```

   Nothing else in the step changes. The `go.mod`/`go.sum` revert in the
   deferred block stays.

Acceptance: `grep -n 'strings.Contains(output, "❌")' go_handler.go` → empty.

## Stage 4 — tests (`test/`)

New file `test/gotest_submodule_wasm_test.go`, package `devflow_test`.

1. **Fixture** `submoduleWithWasmSuite(t) string`: a `t.TempDir()` with
   - `go.mod`: `module example.com/fx` + `go 1.25.2`
   - `fx.go`: `package fx` + `func F() int { return 1 }`
   - `tests/go.mod`: `module example.com/fx/tests`, `go 1.25.2`,
     `require example.com/fx v0.0.0`, `replace example.com/fx => ..`
   - `tests/native_test.go`: untagged, `package tests`, one passing test
     importing `example.com/fx`.
   - `tests/front_test.go`: `//go:build wasm`, `package tests`, one passing
     test.

   (Run `go mod tidy` in the fixture is NOT needed: the module has no external
   requires. If `go list` reports a missing `go.sum`, write an empty
   `tests/go.sum`.)
2. **`TestSubmoduleWasmSuiteIsRun`**: build `g` like `captureGoTest` in
   `test/gotest_options_test.go` does (`gitmod.NewGit`, `devflow.NewGo`,
   `SetRootDir(fixture)`). Swap `devflow.GoTestCmdFn` to record
   `(dir, args)` and return `exec.Command("true")`. Swap `command.Exec` so
   `go` → `exec.Command("true")` for everything EXCEPT `go list` (detection
   must see the real build tags). Restore both in `t.Cleanup`. Run the full
   suite (`g.Test(devflow.TestOptions{…})` with no custom args and `NoCache`
   set — mirror the options the existing tests use). Assert that some recorded
   call has `dir == filepath.Join(fixture, "tests")` AND `args` contains
   `"-exec"`.

   > Detection in `wasmEnabledIn` uses `exec.Command` directly (not
   > `command.Exec`), so it runs the real `go list` regardless of the swap —
   > that is intended. If after Stage 1 it goes through `command.Exec`, the
   > swap above must let `go list` through.
3. **`TestExtractFirstFailureNamesEveryFailingStage`** — this needs the
   unexported `extractFirstFailure`, so it lives in the repo root as
   `extract_failure_test.go`, `package devflow`. Table:

   | output | want |
   |---|---|
   | `"Tests failed: vet ✅, race ✅, tests ✅, wasm ❌ v0.0.56 (24.1s)"` | `"wasm"` |
   | `"Tests failed: vet ✅, race ✅, tests ✅, wasm tests ❌ (3s)"` | `"wasm tests"` |
   | `"Tests failed: vet ❌, race ✅, tests ❌, wasm ✅ (3s)"` | `"vet, tests"` |
   | `"Tests failed: timeout: TestX (exceeded 60s) ❌ (70s)"` | `"timeout: TestX (exceeded 60s)"` |
   | `"go: updates to go.mod needed"` | `"failed"` |

   This table is the contract for Stage 3.1.
4. **`TestWriteGateLog`** (root, `package devflow`): `writeGateLog("auth/tests", "x")`
   returns a path ending in `gopush-gate-auth-tests.log` whose content is `x`.
   Remove the file in `t.Cleanup`.

Run `gotest`: everything green.

## Stage 5 — docs

- `docs/GOTEST.md`, after item 5 of the numbered list (WASM auto-detection),
  add:

  > Submodules (a subdirectory with its own `go.mod`, e.g. `tests/`) get the
  > same treatment: native tests **and** their own WASM detection and run. A
  > WASM failure in a submodule is reported with its path: `wasm tests ❌`. So
  > `gotest` gives the same verdict at the repo root as inside the submodule.

- `docs/GOPUSH.md`, in the section describing dependents (search for
  "dependent"; if none exists, append a `## Dependents` section) add:

  > When a dependent's gate fails, the line names every failing stage of its
  > `gotest` summary and points to the full output:
  > `📦 auth/tests → wasm ❌ (log: /tmp/gopush-gate-auth-tests.log)`.
  > Its `go.mod`/`go.sum` are restored; reproduce with `gotest` in that
  > directory after `go get <module>@<version>`.

## Stages

| # | What | Files |
|---|---|---|
| 1 | `wasmEnabledIn(dir, runAll)`; `wasmTestPackages(dir, runAll)` | `gotest.go` |
| 2 | `runWasmIn` + per-directory WASM loop in both suites; drop the dead comment block | `gotest.go` |
| 3 | `extractFirstFailure` names every failing stage; `writeGateLog`; gate line with log path | `go_handler.go` |
| 4 | Fixture + 3 tests; `gotest` green | `test/gotest_submodule_wasm_test.go`, `extract_failure_test.go` |
| 5 | Document submodule WASM + gate log | `docs/GOTEST.md`, `docs/GOPUSH.md` |
