---
PLAN: "feat(gotest): docs/TESTS.md index check — every test has a row, every row has a test"
EXECUTOR: jules
REVIEWER: none
STATUS: review
SESSION: 11756451562153705644
PR: https://github.com/webtyp/devflow/pull/52
REVIEWED_COMMIT: e4f31f7e58fbf04d74d3302586be1d3b5aa2a3be
---

> Read AGENTS.md at the repo root before any change.
> **Finish by yourself:** when the delivery check passes, commit, push and open the pull request. Never ask whether to
> proceed or finalize — nobody answers, and a session that waits ends without your work.

# gotest — domain tests index check (`docs/TESTS.md`)

## Why

Every repo documents its domain tests in `docs/TESTS.md`: one section per area, each row «rule protected → link to
the test». Example of the format (this is the real format, copy nothing else):

```markdown
## Ingreso (LAN)
| Regla que protege | Prueba |
|---|---|
| 3 intentos fallidos bloquean la IP 15 min | [TestLogin_RateLimited](../tests/login_test.go) |
```

The full `gotest` suite must fail when a test has no row, or a row points to a test that does not exist.

## Tools

```bash
go install webtyp.com/devflow/cmd/gotest@latest
# after your change, to test your own build:
go install ./cmd/gotest
```

Environment: `GOPRIVATE=webtyp.com/*,github.com/veltylabs/*`, `GOFLAGS=-mod=mod`.

## Decisions (no judgment needed)

| # | Rule |
|---|---|
| 1 | The check runs **only in the full suite** (`gotest` with no args, `runFullTestSuite` in `gotest.go`). Never in `runCustomTests`. |
| 2 | `docs/TESTS.md` **absent** → the check does not fail; it prints one console line `⚠️ docs/TESTS.md missing: domain tests are not indexed` (repos adopt the index one by one). No summary message is added. |
| 3 | `docs/TESTS.md` **present** → summary gets `tests index ✅` or `tests index ❌`; on ❌ `testStatus = "Failed"` and each problem is printed with `g.consoleOutput`, one per line. |
| 4 | A **test** is a top-level `func` (no receiver) in a `*_test.go` file whose name starts with `Test`, is not `TestMain`, and has exactly one parameter. Benchmarks, examples, fuzz are ignored. |
| 5 | Files scanned: every `*_test.go` under the repo root, recursively, **skipping** directories whose name starts with `.` or `_`, plus `vendor`, `testdata`, `node_modules`, and any subdirectory that contains its own `go.mod` (submodule). |
| 6 | A **row** is any markdown link in `docs/TESTS.md` matching the regexp `\[(Test[A-Za-z0-9_]*)\]\(([^)#\s]+_test\.go)(#[^)]*)?\)`. The link target is resolved **relative to `docs/`** (so `../tests/x_test.go` → `tests/x_test.go`). |
| 7 | **Missing row**: a test with no row whose name equals the test name AND whose resolved target equals the file that declares it. Message (verbatim): `docs/TESTS.md: missing row for <TestName> (<rel/path_test.go>)` |
| 8 | **Orphan row**: a row whose resolved file does not declare a test with that name (including file not found). Message (verbatim): `docs/TESTS.md: row <TestName> → <rel/path_test.go>: test not found` |
| 9 | Paths in messages use forward slashes, relative to the repo root. Problems are sorted: missing rows first (by path, then name), then orphans (same order). |
| 10 | This repo (devflow) does **not** get a `docs/TESTS.md` in this plan. Do not create one. |

## APIs used

Only the standard library: `go/parser` (`parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)`),
`go/ast`, `go/token`, `os`, `path/filepath`, `regexp`, `sort`, `strings`. This repo does not compile to WASM:
the stdlib is legitimate here (see AGENTS.md), do NOT replace it with `webtyp.com/*` packages.

Existing in `gotest.go` (same package `devflow`): `g.rootDir string`, `g.consoleOutput(string)`, the local closure
`addMsg(ok bool, msg string)` and variable `testStatus string` inside `runFullTestSuite`, and helper
`checkFileExists(path string) bool`.

## Stage 1 — `gotest_testsindex.go` (new, package `devflow`)

Write exactly this public surface (it is used by `gotest.go` and by the tests in `tests/`, like `FindSlowestTest`):

```go
package devflow

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// TestsIndexFile is the domain tests index every repo keeps, relative to its root.
const TestsIndexFile = "docs/TESTS.md"

const (
	testsIndexLabel   = "tests index"
	testsIndexMissing = "⚠️ docs/TESTS.md missing: domain tests are not indexed"
	missingRowFormat  = "docs/TESTS.md: missing row for %s (%s)"
	orphanRowFormat   = "docs/TESTS.md: row %s → %s: test not found"
)

var testsIndexRow = regexp.MustCompile(`\[(Test[A-Za-z0-9_]*)\]\(([^)#\s]+_test\.go)(#[^)]*)?\)`)

// TestsIndexReport is the result of checking docs/TESTS.md against the test functions of a repo.
type TestsIndexReport struct {
	Present  bool     // docs/TESTS.md exists
	Problems []string // verbatim messages, sorted; empty means the index is in sync
}

// CheckTestsIndex compares docs/TESTS.md under rootDir with every Test function of the repo.
// Example: r, err := devflow.CheckTestsIndex("."); if r.Present && len(r.Problems) > 0 { ... }
func CheckTestsIndex(rootDir string) (TestsIndexReport, error)
```

Body requirements (write them as code, these are the exact semantics):

- Read `filepath.Join(rootDir, TestsIndexFile)`; `os.IsNotExist` → `return TestsIndexReport{}, nil`. Other read error → return it.
- Collect tests with a `filepath.WalkDir` honouring Decision 5 (for a dir ≠ rootDir: skip if name has prefix `.`/`_`, is in the skip list, or `checkFileExists(filepath.Join(path, "go.mod"))`). For each `*_test.go`: parse, iterate `f.Decls`, keep `*ast.FuncDecl` with `Recv == nil`, name prefix `Test`, name ≠ `TestMain`, `len(Type.Params.List) == 1`. Key = `rel + "\x00" + name` where `rel = filepath.ToSlash(filepath.Rel(rootDir, path))`. A parse error → return it.
- Collect rows with `testsIndexRow.FindAllStringSubmatch`; resolved rel = `filepath.ToSlash(filepath.Clean(filepath.Join("docs", m[2])))`. Same key format.
- Missing = tests not in rows; orphans = rows not in tests (deduplicate rows). Use slices of `struct{ rel, name string }` sorted with `sort.Slice` by `rel` then `name`, then format with `fmt.Sprintf` (add `fmt` import).
- No `map` restriction applies here (not WASM); a `map[string]bool` for the sets is fine.

## Stage 2 — wire into `gotest.go`

In `runFullTestSuite`, immediately **after** the browser pass block (the one ending with `addMsg(!failed, browserPassLabel)`)
and **before** `// Report consolidated coverage`, insert:

```go
	// Domain tests index: docs/TESTS.md lists every Test function, and every row points to one.
	if report, err := CheckTestsIndex(g.rootDir); err != nil {
		addMsg(false, testsIndexLabel)
		testStatus = "Failed"
		g.consoleOutput(err.Error())
	} else if !report.Present {
		g.consoleOutput(testsIndexMissing)
	} else {
		addMsg(len(report.Problems) == 0, testsIndexLabel)
		if len(report.Problems) > 0 {
			testStatus = "Failed"
			g.consoleOutput(strings.Join(report.Problems, "\n"))
		}
	}
```

## Stage 3 — tests `tests/gotest_testsindex_test.go` (package `devflow_test`)

Each test builds a repo in `t.TempDir()` (write files with `os.MkdirAll` + `os.WriteFile`) and calls
`devflow.CheckTestsIndex(dir)`. Test file contents use `package x_test` + `import "testing"` + funcs.

| Test | Setup | Expected |
|---|---|---|
| `TestTestsIndex_AbsentIsNotChecked` | `tests/a_test.go` with `TestA`, no `docs/` | `Present == false`, `len(Problems) == 0`, `err == nil` |
| `TestTestsIndex_InSync` | `tests/a_test.go` with `TestA(t *testing.T)` and `TestMain(m *testing.M)` and `BenchmarkA(b *testing.B)`; `docs/TESTS.md` with row `[TestA](../tests/a_test.go)` | `Present == true`, `Problems` empty |
| `TestTestsIndex_MissingRow` | `tests/a_test.go` with `TestA`, `TestB`; TESTS.md only row TestA | `Problems == []string{"docs/TESTS.md: missing row for TestB (tests/a_test.go)"}` |
| `TestTestsIndex_OrphanRow` | `tests/a_test.go` with `TestA`; rows TestA and `[TestGone](../tests/a_test.go)` | `Problems == []string{"docs/TESTS.md: row TestGone → tests/a_test.go: test not found"}` |
| `TestTestsIndex_WrongFileIsBoth` | `TestA` in `tests/a_test.go`; row `[TestA](../tests/b_test.go)` | `Problems == []string{"docs/TESTS.md: missing row for TestA (tests/a_test.go)", "docs/TESTS.md: row TestA → tests/b_test.go: test not found"}` |
| `TestTestsIndex_SkipsTempAndSubmodules` | `TestA` indexed; plus `_temp/x_test.go` with `TestX`, and `sub/go.mod` + `sub/s_test.go` with `TestS` (not indexed) | `Problems` empty |
| `TestTestsIndex_RootLevelTest` | `root_test.go` at repo root with `TestR`; row `[TestR](../root_test.go)` | `Problems` empty |

Compare slices with `reflect.DeepEqual`; no assertion libraries.

## Stage 4 — documentation

- `docs/GOTEST.md`: add a section `## Domain tests index (docs/TESTS.md)` after the Options table: what it checks
  (Decisions 2, 3, 6, 7, 8 in prose), the example table from «Why», and the two messages verbatim.
- `README.md`: in the place that lists what the `gotest` full suite does, add «docs/TESTS.md index check» with a
  link to `docs/GOTEST.md`. Keep README ≤ 150 lines.
- If `docs/diagrams/` has a gotest full-suite flowchart, add the step between the browser pass and coverage.

## Delivery check

```bash
go install ./cmd/gotest && gotest
grep -n "CheckTestsIndex" gotest.go gotest_testsindex.go tests/gotest_testsindex_test.go   # all three non-empty
test ! -e docs/TESTS.md
```

A PR that does not compile, or whose tests could not run, is a failed delivery. Commit after every stage. Never run
`git reset`, `git restore`, `git checkout -- <file>` or `git stash`. Stuck 30 minutes → open the PR anyway listing
what fails. Do not touch the uncommitted `devbackup*` files if you see them referenced: they are out of scope.

## Stages

| # | Files |
|---|---|
| 1 | `gotest_testsindex.go` (new) |
| 2 | `gotest.go` |
| 3 | `tests/gotest_testsindex_test.go` (new) |
| 4 | `docs/GOTEST.md`, `README.md`, `docs/diagrams/*` (if applicable) |
