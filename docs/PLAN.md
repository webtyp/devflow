---
PLAN: "feat(gotest): fail when a source file needs a build tag no gotest pass sets"
EXECUTOR: jules
REVIEWER: none
STATUS: running
SESSION: 16940990928475209428
---

> Read AGENTS.md at the repo root before any change.

# PLAN — the "build tags" check in `gotest`

## Why

`veltylabs/mjosefa-cms` had `config/prod.go` behind `//go:build prod`. No `gotest` pass ever sets
`prod` (the passes are: default, `-tags dev`, `GOOS=js GOARCH=wasm`, `-tags browser`, and
`-tags integration` with `-a`), so that file was **never compiled** and broke without anyone
seeing it: the production binary did not build. The ecosystem convention (`buildtag.go`: the
normal build is production, `dev` exists only in development) was only a comment; nothing
enforced it. After this plan, `gotest` fails on such a file in every repo.

## Design gate (api-design)

1. **Prior art.** `go vet` reports `buildtag` misuse (malformed lines), not tags nobody sets.
   `golangci-lint` has no such rule. Bazel/Please fail when a target is never built because the
   build graph is explicit. Here the "graph" is the fixed set of `gotest` passes, so the check
   belongs in `gotest`, next to vet, with no configuration.
2. **Names.** The step prints as `build tags` in the summary (`vet ✅, build tags ❌, …`). No
   exported symbol: the function is unexported (`unreachableFiles`).
3. **Ledger.** Concepts +0 (the convention already exists), files to touch to do X: 0, ways to do
   the same thing: −1 (`prod` as a second way to say "production" stops compiling).
4. **Where.** `devflow`, owner of `gotest` and of `buildtag.go`.
5. **Deletes.** Nothing in this repo; it makes every other repo delete its private tags.

## Rule

A **non-test** `.go` file (name not ending in `_test.go`) fails the check when its
`//go:build` expression is **false for every assignment** where:

- these identifiers may be true or false freely: the platform ones (every `GOOS` and `GOARCH`
  value, `unix`, `cgo`, `gc`, `gccgo`, `race`, `msan`, `asan`, `purego`, `tinygo`, and any
  `go1.N`), the `gotest` pass tags `dev`, `browser`, `integration`;
- these conventional tags make the file **exempt** (never reported): `ignore` (`go run`
  scripts), `tools` (tool pinning), `generate` (generators), `deploy` (`config/deploy.go`, read
  by `webtyp/deploy` from its source);
- **every other identifier is false.**

So `//go:build prod` fails, `//go:build !prod` passes (true when `prod` is false),
`//go:build windows` passes, `//go:build wasm && browser` passes, `//go:build dev` passes.
Test files are skipped: opt-in suites (`e2e`, `eval`, `transbank_integration`) are allowed.
Directories skipped exactly as `hasTaggedFiles` does (`.`-prefixed, `vendor`, `node_modules`,
`testdata`); submodules included.

## Stage 1 — `buildtag.go`

Add, below `fileNeedsTag`:

```go
// Identifiers a gotest pass (or another machine) can set. Everything else is
// false when gotest builds, so a file that needs it is never compiled.
var freeTags = []string{
	devTag, browserTag, integrationTag,
	"aix", "android", "darwin", "dragonfly", "freebsd", "hurd", "illumos", "ios", "js",
	"linux", "netbsd", "openbsd", "plan9", "solaris", "wasip1", "windows", "zos", "unix",
	"386", "amd64", "arm", "arm64", "loong64", "mips", "mipsle", "mips64", "mips64le",
	"ppc64", "ppc64le", "riscv64", "s390x", "sparc64", "wasm",
	"cgo", "gc", "gccgo", "race", "msan", "asan", "purego", "tinygo",
}

// exemptTags mark files that are not part of any build on purpose.
var exemptTags = []string{"ignore", "tools", "generate", "deploy"}

// unreachableFiles lists the non-test .go files under root whose build
// constraint no gotest pass can satisfy. Paths are relative to root.
func unreachableFiles(root string) []string
```

- `integrationTag`: add `const integrationTag = "integration"` next to `devTag`, and replace the
  `"integration"` literals in `gotest_destinations.go:21` and `gotest.go` (`-tags=integration`
  becomes `"-tags=" + integrationTag`) with it.
- Implementation: walk like `hasTaggedFiles`; read the constraint like `fileNeedsTag` (reuse its
  scanning loop by extracting `func readConstraint(path string) (constraint.Expr, bool)` and make
  `fileNeedsTag` call it). Collect the identifiers of the expression (walk `constraint.AndExpr`,
  `OrExpr`, `NotExpr`, `TagExpr`). If any is in `exemptTags`, skip the file. Free identifiers =
  those in `freeTags` or with prefix `go1.`; try every true/false combination of the free
  identifiers that appear in the expression (they are few: at most 2^k, k ≤ 10 — if k > 10 treat
  the file as reachable), with every other identifier false. No combination true → report it.

## Stage 2 — `gotest.go` (`runFullTestSuite`)

Right after the vet result is processed (after the `if vetErr != nil { … }` block):

```go
	if files := unreachableFiles(g.rootDir); len(files) > 0 {
		addMsg(false, "build tags")
		testStatus = "Failed"
		g.consoleOutput(buildTagsReport(files))
	}
```

Add to `buildtag.go`:

```go
// buildTagsReport explains each file and how to fix it.
func buildTagsReport(files []string) string
```

Output, one line per file, exactly:
`build tags: <file> is never compiled: its //go:build needs a tag no gotest pass sets. Production is the normal build and development is -tags dev (webtyp convention): use !dev / dev, or delete the file.`

`testStatus = "Failed"` must survive the later `EvaluateTestResults` call: set a local
`buildTagsFailed := true` and, just before the `// Return error if tests or vet failed` block,
`if buildTagsFailed { testStatus = "Failed" }`. Nothing is printed when the check passes (no
`build tags ✅`), so existing summaries do not change.

## Stage 3 — `tests/gotest_buildtags_test.go` (new)

Same helpers as `tests/gotest_devtag_test.go` (`testCreateGoModule`, `captureGoTest` or a direct
`g.Test(devflow.TestOptions{NoCache: true})` with `GoTestCmdFn`/`command.Exec` faked as in
`captureGoTest`). Write the file into the module, run, assert on the returned summary string:

| Test | File written | Expected |
|---|---|---|
| `TestBuildTags_PrivateTagFails` | `prod.go`: `//go:build prod` | summary contains `build tags ❌` and error is non-nil |
| `TestBuildTags_NegatedPrivateTagPasses` | `prod.go`: `//go:build !prod` | summary does not contain `build tags` |
| `TestBuildTags_PlatformAndPassTagsPass` | `a.go` `//go:build windows`, `b.go` `//go:build wasm && browser`, `c.go` `//go:build dev`, `d.go` `//go:build go1.21` | no `build tags` |
| `TestBuildTags_ExemptTagsPass` | `gen.go` `//go:build ignore`, `deploy.go` `//go:build deploy`, `tools.go` `//go:build tools` | no `build tags` |
| `TestBuildTags_TestFilesSkipped` | `x_test.go`: `//go:build e2e` | no `build tags` |
| `TestBuildTags_TestdataSkipped` | `testdata/p.go`: `//go:build prod` | no `build tags` |
| `TestBuildTags_PrivateAndPlatformFails` | `p.go`: `//go:build prod && linux` | `build tags ❌` |

## Stage 4 — docs

- `README.md` (the `gotest` section): one bullet: "Fails (`build tags ❌`) when a non-test file
  needs a build tag no pass sets — production is the normal build, development `-tags dev`."
- `docs/` file that documents gotest steps, if one lists them (grep `dev tests` under `docs/`):
  add the same line.

## Delivery check

```
go install ./cmd/gotest && gotest
```

- A PR that does not compile, or whose tests could not run, is a failed delivery.
- Never pause to ask permission: when the check passes, commit, push and open the PR. Stuck 30
  minutes: open the PR listing what fails.
- `git diff --stat main` lists only: `buildtag.go`, `gotest.go`, `gotest_destinations.go`,
  `tests/gotest_buildtags_test.go`, `README.md`, the docs file of Stage 4 if it exists, this plan.

## Stages

| # | Files | Done when |
|---|---|---|
| 1 | `buildtag.go`, `gotest_destinations.go` | compiles |
| 2 | `gotest.go` | compiles |
| 3 | `tests/gotest_buildtags_test.go` | 7 tests pass |
| 4 | `README.md`, docs | line present |
