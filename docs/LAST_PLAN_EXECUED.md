---
PLAN: "refactor!: Go.Test takes TestOptions instead of five positional parameters"
EXECUTOR: jules
REVIEWER: none
---

> This plan is dispatched via the CodeJob workflow. See skill: agents-workflow.

## Prerequisite — install the test runner

External agents run in isolated environments where `gotest` is not installed.
Run this **before anything else**; the acceptance criteria depend on it:

```bash
go install webtyp.com/devflow/cmd/gotest@latest
```

Then use `gotest` for the whole suite and `gotest -run TestName` for one test.
Never call `go test` directly: `gotest` handles `-vet`, `-race`, `-cover`, the
WASM suite and the README badges.

# Plan — `Go.Test` options struct

## The defect

`gotest.go:27`:

```go
func (g *Go) Test(customArgs []string, skipRace bool, timeoutSec int, noCache bool, runAll bool) (string, error)
```

At every call site it reads like this (`gotest_mcp.go:84`):

```go
summary, err = p.g.Test(nil, false, 0, false, false)
```

Nothing about that line says what it does. The proof is in the repository
itself — `go_handler.go:284` carries a comment whose only job is to decode the
arguments:

```go
testSummary, err := g.Test([]string{}, skipRace, 0, false, false) // Empty slice = full test suite, 0 = default timeout, false = allow cache, false = runAll
```

A comment that exists to explain a signature is the signature admitting it
failed. Three of the five parameters are booleans of the same type, so swapping
any two compiles silently and changes behaviour.

## Design gate

Required by skill **api-design**.

**1. Prior art.** Go's own standard library uses this shape for exactly this
situation: `http.Server{}`, `tls.Config{}`, `json.Encoder` — a struct whose zero
value is the default configuration. `exec.Cmd` likewise. The functional-options
pattern (`WithRace()`, `WithTimeout(n)`) is the other Go convention; it is
heavier and is worth its cost only for a public API with many optional knobs
that must stay backward compatible. This one has five, is internal, and has no
external users.

**2. Novice-name test.** `Test(TestOptions{SkipRace: true})` reads as a
sentence. `Test(nil, false, 0, false, false)` does not.

**3. Ledger.**

```
Concepts to learn            +1  / −0   (one struct)
Lines at the call site        0
Comments needed to read it   −1         (go_handler.go:284 is deleted)
Ways to call it               0         (the old signature is removed, not kept)
Silently swappable arguments −3         (three same-typed booleans stop being positional)
```

**4. Where it belongs.** `TestOptions` belongs beside `Test`, in `gotest.go`.
No new package.

**5. What it deletes.** The five-parameter signature and the explanatory comment
at `go_handler.go:284`.

**Why now.** Three call sites, all inside this repository, no external users. The
cost of this change never gets lower than it is today, and a published tag
freezes the signature.

## Stage 1 — the type and the signature

In `gotest.go`, above `Test`:

```go
// TestOptions configures a test run. The zero value runs the full suite with
// the race detector, the default timeout, and the build cache enabled.
type TestOptions struct {
    Args     []string // extra `go test` arguments; nil or empty = full suite
    SkipRace bool     // omit -race
    Timeout  int      // seconds; 0 = the package default
    NoCache  bool     // add -count=1
    RunAll   bool     // include the packages normally skipped
}

func (g *Go) Test(opts TestOptions) (string, error)
```

The body is unchanged except that it reads the fields instead of the parameters.
The unexported helpers (`runFullTestSuite`, `runCustomTests`) keep their current
signatures — they are internal and out of scope.

Do **not** keep the old signature under another name, and do not add a
`TestLegacy` wrapper.

## Stage 2 — the three call sites

| File | Was | Becomes |
|---|---|---|
| `go_handler.go:284` | `g.Test([]string{}, skipRace, 0, false, false)` plus its decoder comment | `g.Test(TestOptions{SkipRace: skipRace})` — **delete the comment** |
| `gotest_mcp.go:84` | `p.g.Test(nil, false, 0, false, false)` | `p.g.Test(TestOptions{})` |
| `gotest_mcp.go:86` | `p.g.Test([]string{"-run", args.Run}, false, 0, false, false)` | `p.g.Test(TestOptions{Args: []string{"-run", args.Run}})` |

Also update `cmd/gotest/main.go` if it builds the arguments itself: it must
construct a `TestOptions` and pass it, keeping `cmd/` thin.

## Constraints

- **Thin `cmd/`.** `cmd/gotest/main.go` parses flags into a `TestOptions` and
  calls the library. No conditional logic beyond that mapping.
- **No hardcoded strings.** Any repeated flag string (`-run`, `-count=1`) is a
  named constant in the package.
- This repository is backend tooling: the standard library is legitimate. Do not
  "fix" stdlib imports.

## Tests

Extend the existing `gotest` tests:

1. `TestOptions{}` → the command built includes `-race` and no `-count=1`.
2. `TestOptions{SkipRace: true}` → no `-race`.
3. `TestOptions{NoCache: true}` → includes `-count=1`.
4. `TestOptions{Timeout: 90}` → the timeout reaches the built command.
5. `TestOptions{Args: []string{"-run", "TestX"}}` → takes the custom path.

If the command construction is not currently reachable without executing the
toolchain, extract it into an unexported pure function that returns the argument
slice, and test that. Do not add a test that shells out to `go test`.

## Acceptance criteria

1. `grep -rn "Test(\[\]string{}\|Test(nil, false" --include='*.go' .` → empty.
2. `grep -n "Empty slice = full test suite" go_handler.go` → empty.
3. `go build ./... && go vet ./... && go test ./...` → clean.

## Stages

| # | Stage | File(s) | Gate |
|---|---|---|---|
| 1 | `TestOptions` + signature | `gotest.go` | compiles |
| 2 | call sites + comment deletion | `go_handler.go`, `gotest_mcp.go`, `cmd/gotest/main.go` | criteria 1, 2 |
| 3 | tests | `gotest_*_test.go` | criterion 3 |

Sequential.

## Out of scope

This repository holds seven unrelated concerns in one package — badges,
codejob, devbackup, goinstall, gonew, gopush, gotest — which is why the
`webtyp` binary links GitHub badge rendering and Jules dispatch. Splitting it is
a separate decision and a separate plan. Do not start it here.
