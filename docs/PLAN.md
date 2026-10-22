---
PLAN: "feat!: gotest finds the Worker and browser mains in web/edge/ and web/browser/"
EXECUTOR: jules
REVIEWER: none
---

> Read AGENTS.md at the repo root before any change.

# gotest destination passes: `web/<binary>/main.go`

## Tools

```bash
go install webtyp.com/devflow/cmd/gotest@latest
```

## Why

The ecosystem moves every binary main into its own folder under `web/` (the folder is the
binary): `web/browser/main.go` → `browser.wasm`, `web/server/main.go` → `server`,
`web/edge/main.go` → `edge.wasm`. A main carries no destination tag, and reusable modules' `ui/`
render files lose their `browser` tag too, so "some file is tagged `browser`" no longer proves a
browser binary exists: the presence of `web/browser/main.go` must trigger the browser pass as
well. No backwards compatibility: `web/edge.go` / `web/browser.go` stop being detected.

Scope: `gotest_destinations.go`, the two call sites in `gotest.go`, their tests and
`docs/GOTEST.md`. Do NOT touch `docs/PLAN_NOREFLECT_GUARD.md` (another wave's parked plan).

## Stage 1 — `gotest_destinations.go`

1. Constants: replace `webDir`, `edgeMainFile`, `browserMainFile` with

```go
const (
	edgeMainDir    = "web/edge"    // web/edge/main.go: the Worker main
	browserMainDir = "web/browser" // web/browser/main.go: the browser main
	mainFile       = "main.go"
	browserPassLabel = "browser"
	workerPassLabel  = "worker"
)
```

   (if `mainFile` collides with an existing identifier in the package, name it `binaryMainFile`).
2. `vetWebMain(tag string)` → `vetWebMain(pkgDir, tag string)`: it vets `"./" + pkgDir`
   (`go vet ./web/edge`, `go vet -tags=browser ./web/browser`), env `GOOS=js GOARCH=wasm` as today.

## Stage 2 — `gotest.go` call sites

- Worker pass: condition `checkFileExists(filepath.Join(g.rootDir, filepath.FromSlash(edgeMainDir), mainFile))`;
  call `g.vetWebMain(edgeMainDir, "")`. Comment: "web/edge/main.go is the Cloudflare Worker main."
- Browser pass: condition becomes
  `hasTaggedFiles(g.rootDir, browserTag) || browserMainExists` where
  `browserMainExists := checkFileExists(filepath.Join(g.rootDir, filepath.FromSlash(browserMainDir), mainFile))`;
  the inner vet runs when `browserMainExists`: `g.vetWebMain(browserMainDir, browserTag)`.
  Comment: "Browser pass: the browser main exists, or some file is tagged `browser`."

## Stage 3 — tests `tests/gotest_destinations_test.go`

Update the subtests of `TestBrowserAndWorkerPasses` (same helpers):

| Subtest | Setup | Expected |
|---|---|---|
| D4 | `web/edge/main.go` = `package main` (no tag) | a run with `vet` and `./web/edge`; that run has no `-tags=` |
| D5 | `web/browser/main.go` = `package main` (no tag), no other tagged file | a run with `vet`, `-tags=browser`, `./web/browser`; and a run with `test`, `-tags=browser` |
| D6 | no `web/edge/main.go` | no `vet` run containing `./web/edge` |
| D9 (new) | legacy `web/edge.go` and `web/browser.go` only (tagged as before) | no `vet` run containing `./web/edge` nor `./web/browser` |

D1–D3, D7, D8 stay as they are.

## Stage 4 — `docs/GOTEST.md`

In the «Destination passes» table:
- worker: triggers when `web/edge/main.go` exists; command `GOOS=js GOARCH=wasm go vet ./web/edge`.
- browser: triggers when `web/browser/main.go` exists **or** any `.go` file has
  `//go:build browser`; commands `GOOS=js GOARCH=wasm go vet -tags browser ./web/browser` and
  `go test -exec wasmbrowsertest -tags browser ./...`.

`AGENTS.md`: never edit its `<!-- webtyp:base -->` block; outside it, update any
`web/edge.go`/`web/browser.go` to the folder form.

## Delivery check

```bash
gotest
grep -rn 'edgeMainFile\|browserMainFile\|"web", "edge.go"\|web/edge\.go\|web/browser\.go' --include=*.go --include=GOTEST.md . | grep -v 'D9'   # empty
```

A PR whose tests could not run is a failed delivery. Environment:
`GOPRIVATE=webtyp.com/*,github.com/veltylabs/*`, `GOFLAGS=-mod=mod`.

Never pause to ask permission or report progress: when the delivery check passes, commit, push
and open the PR. Stuck for 30 minutes → open the PR anyway listing what fails.

## Stages

| # | Files | Result |
|---|---|---|
| 1 | `gotest_destinations.go` | folder constants, `vetWebMain(pkgDir, tag)` |
| 2 | `gotest.go` | worker and browser passes find the folder mains |
| 3 | `tests/gotest_destinations_test.go` | D4–D6 updated, D9 new |
| 4 | `docs/GOTEST.md`, `AGENTS.md` | docs |
