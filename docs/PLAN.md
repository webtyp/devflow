---
PLAN: "feat(gotest): one pass per build destination — browser tests in headless Chrome with -tags browser, Worker and browser mains vetted for js/wasm"
EXECUTOR: jules
REVIEWER: none
---

> Read AGENTS.md at the repo root before any change.

# gotest: one pass per build destination

## Why

webtyp apps now have three binaries, named by file (ecosystem decision, already in the skills):

| File | Build tag | Binary |
|---|---|---|
| `web/browser.go` | `//go:build browser` | `web/public/browser.wasm` (compiled with `-tags browser`) |
| `web/edge.go` | `//go:build wasm && !browser` | `web/edge.wasm` (Cloudflare Worker, no destination tag) |
| `web/server.go` | `//go:build !wasm` | `web/server` (native) |

Modules follow the same axis: `server.go` is `//go:build !browser`, `browser.go` is `//go:build browser`.
A file tagged `browser` never compiles in today's runs: not natively (no tag), not in the
current wasm pass (no tag). Its tests (the view tests, `tests/<m>_view_test.go`) would silently
stop running. Each destination must be tested **on its own platform**:

- native: unchanged (today's vet + race + cover run);
- Worker and shared wasm: unchanged wasm pass (`GOOS=js GOARCH=wasm`, no tag) **plus** `go vet ./web`
  for js/wasm when `web/edge.go` exists, so the Worker main and everything it imports compiles;
- browser: a new pass `GOOS=js GOARCH=wasm go test -exec wasmbrowsertest -tags browser` when the
  module has any file that only builds with the `browser` tag, plus `go vet -tags browser ./web`
  for js/wasm when `web/browser.go` exists.

This is the same mechanism as the existing **dev pass** (`devtag.go`: `hasDevTaggedFiles`,
`fileNeedsDevTag`, `runDevPass`), generalized to a second tag. Copy its shape.

## Tools

This repo builds its own runner: `go install ./cmd/gotest && gotest` (AGENTS.md).

## Environment

```bash
export GOPRIVATE=webtyp.com/*,github.com/veltylabs/*
export GOFLAGS=-mod=mod
```

A PR that does not compile, or whose tests could not run, is a failed delivery.
Never pause to ask permission or report progress: when the Delivery check passes, commit, push and
open the PR. Stuck for 30 minutes: open the PR anyway and list what fails.

## APIs used (all in this repo, read them before editing)

| Symbol | Exact signature | File |
|---|---|---|
| `GoTestCmdFn` | `var GoTestCmdFn = testCommand` with type `func(ctx context.Context, dir string, name string, args ...string) *exec.Cmd` | `gotest.go:23` |
| `findSubModuleDirs` | `func findSubModuleDirs(rootDir string) []string` | `gotest.go:743` |
| `(*Go).wasmExecArg` | `func (g *Go) wasmExecArg() string` (`"wasmbrowsertest"` or the tinygo exec) | `gotest_tinygo.go:24` |
| `(*Go).wasmTimeout` | `func (g *Go) wasmTimeout(timeoutSec int) time.Duration` | `gotest_tinygo.go:31` |
| `(*Go).installWasmBrowserTest` | `func (g *Go) installWasmBrowserTest() error` | `gotest.go:971` |
| `(*Go).wasmTestPackages` | `func (g *Go) wasmTestPackages(dir string, runAll bool) []string` (changes in Stage 1) | `gotest.go:1086` |
| `ParseWasmTestPackages` | `func ParseWasmTestPackages(goListOut string) []string` (unchanged) | `gotest.go:1109` |
| `hasDevTaggedFiles`, `fileNeedsDevTag`, `runDevPass` | see `devtag.go` (renamed/generalized in Stage 1) | `devtag.go` |
| `NewConsoleFilter`, `paramWriter` | as used in `runWasmIn` | `gotest.go:1002-1014` |

If anything differs from this table, stop and name it in the PR; never adapt it by guessing.

## Stage 1 — generalize the tag detection (`devtag.go` → `buildtag.go`)

1. Rename the file `devtag.go` → `buildtag.go` (`git mv`).
2. Keep `const devTag = "dev"`; add, next to it:

   ```go
   // browserTag is the destination tag of the browser binary: only the client
   // compiler passes it (-tags browser). Files gated on it run only in js/wasm,
   // in headless Chrome.
   const browserTag = "browser"
   ```
3. Replace `hasDevTaggedFiles(root string) bool` with `hasTaggedFiles(root, tag string) bool` (same
   walk, same skipped dirs) and `fileNeedsDevTag(path string) bool` with
   `fileNeedsTag(path, tag string) bool` (same logic, the literal `devTag` inside the eval becomes the
   `tag` parameter). Both stay unexported. Callers: `hasTaggedFiles(g.rootDir, devTag)`.
4. `runDevPass` is unchanged except for living in `buildtag.go`.
5. Deletions: `grep -rn "hasDevTaggedFiles\|fileNeedsDevTag" --include=*.go .` → empty.

Example table for `fileNeedsTag(path, "browser")`:

| First build line of the file | Result |
|---|---|
| `//go:build browser` | true |
| `//go:build wasm && browser` | true |
| `//go:build !browser` | false (default build includes it) |
| `//go:build wasm` | false |
| no build line | false |

## Stage 2 — `wasmTestPackages` takes the extra tag

Change the signature to `func (g *Go) wasmTestPackages(dir string, runAll bool, extraTag string) []string`.
The `-tags=` argument it builds is the comma join of the non-empty values of
`"integration"` (only when `runAll`) and `extraTag`; no `-tags` argument when both are empty.
Example: `runAll=true, extraTag="browser"` → `-tags=integration,browser`;
`runAll=false, extraTag=""` → no `-tags`. Existing callers pass `""`.

## Stage 3 — new file `gotest_destinations.go`

```go
package devflow

const (
	webDir         = "web"
	edgeMainFile   = "edge.go"    // web/edge.go: the Worker main
	browserMainFile = "browser.go" // web/browser.go: the browser main
	browserPassLabel = "browser"
	workerPassLabel  = "worker"
)
```

Functions (all unexported, methods on `*Go`):

1. `func (g *Go) runBrowserTests(dir string, runAll bool, timeoutSec int, extraArgs []string) (output string, failed bool)`
   - args: `test`, `-exec`, `g.wasmExecArg()`, `-count=1`, then `extraArgs...`, then
     `g.wasmTestPackages(dir, runAll, browserTag)...`; the `-tags=` value is the same as in Stage 2
     (`browser`, or `integration,browser`) and goes before the packages.
   - command: `GoTestCmdFn(ctx, dir, "go", args...)` with `ctx` from
     `context.WithTimeout(context.Background(), g.wasmTimeout(timeoutSec))`, `cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm")`,
     output piped through `NewConsoleFilter(g.consoleOutput)` exactly as `runWasmIn` does.
   - `failed` = the command returned an error or the context deadline was exceeded.
2. `func (g *Go) vetWebMain(tag string) (output string, failed bool)`
   - runs `GoTestCmdFn(context.Background(), g.rootDir, "go", args...)` with
     `args = ["vet", "./web"]` when `tag == ""`, `["vet", "-tags=" + tag, "./web"]` otherwise,
     env `GOOS=js GOARCH=wasm`. Output captured in a `bytes.Buffer`.

## Stage 4 — wire the passes into `runFullTestSuite` (`gotest.go`)

Right **after** the existing `// WASM Tests` block (after its `if len(wasmDirs) > 0 { … }`) and
**before** `// Report consolidated coverage`, insert, in this order:

```go
// Worker pass: web/edge.go is the Cloudflare Worker main (wasm && !browser).
// No test runs it, so vet it for js/wasm: that compiles the Worker and all it imports.
if checkFileExists(filepath.Join(g.rootDir, webDir, edgeMainFile)) {
	out, failed := g.vetWebMain("")
	addMsg(!failed, workerPassLabel)
	if failed {
		testStatus = "Failed"
		g.consoleOutput(out)
	}
}

// Browser pass: files tagged `browser` only exist in browser.wasm. Their tests
// run in headless Chrome with -tags browser; the browser main is vetted too.
if hasTaggedFiles(g.rootDir, browserTag) {
	failed := false
	if err := g.installWasmBrowserTest(); err != nil {
		failed = true
		g.consoleOutput("browser tests: wasmbrowsertest setup failed: " + err.Error())
	}
	if !failed && checkFileExists(filepath.Join(g.rootDir, webDir, browserMainFile)) {
		if out, f := g.vetWebMain(browserTag); f {
			failed = true
			g.consoleOutput(out)
		}
	}
	if !failed {
		for _, dir := range append([]string{g.rootDir}, findSubModuleDirs(g.rootDir)...) {
			if out, f := g.runBrowserTests(dir, runAll, timeoutSec, nil); f {
				failed = true
				g.consoleOutput(out)
			}
		}
	}
	addMsg(!failed, browserPassLabel)
	if failed {
		testStatus = "Failed"
	}
}
```

`checkFileExists(path string) bool` (`badges.go:404`) takes any path and is `os.Stat` based.
Setup failure is a **failure** (❌ browser), never a silent skip.

## Stage 5 — the fast path (`runCustomTests`, `gotest -run …`)

After its `if len(wasmCustomDirs) > 0 { … }` block, when `hasTaggedFiles(g.rootDir, browserTag)`:
for each dir of `append([]string{g.rootDir}, findSubModuleDirs(g.rootDir)...)` call
`g.runBrowserTests(dir, runAll, timeoutSec, wasmArgs)` where `wasmArgs` is the filtered custom-arg
slice that block already builds (move its declaration above the `if` so it is in scope).
`addMsg(!failed, browserPassLabel)`. No vet in the fast path (same as today).

## Stage 6 — tests (`tests/gotest_destinations_test.go`, package `devflow_test`)

Use the existing helpers `testCreateGoModule` and `captureGoTest` (`tests/gotest_options_test.go`):
they record the `args` of every `GoTestCmdFn` call. Helper in the new file:

```go
func hasArgs(want ...string) func([]string) bool {
	return func(a []string) bool {
		for _, w := range want {
			found := false
			for _, s := range a {
				if s == w {
					found = true
				}
			}
			if !found {
				return false
			}
		}
		return true
	}
}
```

| # | Setup (files written in the temp module) | Call | Expected |
|---|---|---|---|
| D1 | `view.go` = `//go:build browser\n\npackage main\n` | `captureGoTest(t, dir, devflow.TestOptions{NoCache: true})` | some run has `hasArgs("test", "-tags=browser")` |
| D2 | no file with `browser` | same | no run contains `-tags=browser` |
| D3 | `srv.go` = `//go:build !browser\n\npackage main\n` | same | no run contains `-tags=browser` |
| D4 | `web/edge.go` = `//go:build wasm && !browser\n\npackage main\n` | same | some run has `hasArgs("vet", "./web")` and no `-tags=` in that run |
| D5 | `web/browser.go` = `//go:build browser\n\npackage main\n` | same | some run has `hasArgs("vet", "-tags=browser", "./web")` |
| D6 | no `web/edge.go` | same | no run has `args[0] == "vet"` |
| D7 | D1 setup | `captureGoTest(t, dir, devflow.TestOptions{NoCache: true, RunAll: true})` | some run has `hasArgs("-tags=integration,browser")` |
| D8 | D1 setup | `captureGoTest(t, dir, devflow.TestOptions{Args: []string{"-run", "TestX"}})` | some run has `hasArgs("-tags=browser", "-run", "TestX")` |

The existing `tests/gotest_devtag_test.go` must stay green unchanged (it proves the rename kept
behaviour).

## Stage 7 — docs

- `README.md`, section of `gotest`: one table with the passes (native, dev, wasm, worker, browser),
  when each runs and the status label it prints (`dev tests`, `wasm`, `worker`, `browser`).
- `docs/` file that documents gotest (find it: `grep -rln "dev tests\|wasmbrowsertest" docs/`): same table.

## Delivery check

```bash
go install ./cmd/gotest
go build ./...
gotest
grep -rn "hasDevTaggedFiles\|fileNeedsDevTag" --include=*.go .   # → empty
test -f buildtag.go && test ! -f devtag.go
git diff --stat main   # lists every file below, deletes only devtag.go (renamed)
```

## Stages

| # | Files | Done when |
|---|---|---|
| 1 | `devtag.go` → `buildtag.go` | renamed, generalized, greps empty |
| 2 | `gotest.go` (`wasmTestPackages` + callers) | builds |
| 3 | `gotest_destinations.go` (new) | builds |
| 4 | `gotest.go` (`runFullTestSuite`) | builds |
| 5 | `gotest.go` (`runCustomTests`) | builds |
| 6 | `tests/gotest_destinations_test.go` (new) | D1–D8 green, existing suite green |
| 7 | `README.md`, the gotest doc in `docs/` | table present |
