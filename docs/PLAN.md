---
PLAN: "fix(cascade): a dependent's test submodule (tests/go.mod) is tidied, reverted and committed together with its root go.mod"
EXECUTOR: jules
REVIEWER: none
---

> Read AGENTS.md at the repo root before any change.

# PLAN — the cascade forgets the test submodule

## Why (bug, traced)

Many repos keep their tests in a separate module, `tests/go.mod`, with `replace <root> => ..`
(e.g. `webtyp/auth/tests`, `webtyp/dbsync/tests`). When `gopush` cascades a new version of a
dependency into such a repo, `UpdateDependentModule` (`go_handler.go:442`) runs `go get` and
`go mod tidy` **only in the root**. The test submodule keeps the old requirements, so its
`go test` (the `dev tests` and `wasm tests` passes of the gate) fails with
`go: updates to go.mod needed; to update it: go mod tidy`, and the dependent is never published.
Seen on 2026-10-29 in `auth`, `dbsync`, `rpc`, `idempotency`: in each, `go mod tidy` inside
`tests/` was the whole fix. The revert (`git checkout -- go.mod go.sum`) and the deps-only
commit (`git.CommitPaths(..., "go.mod", "go.sum")`) also see only the root files.

## Design gate

No exported symbol changes. One existing helper is reused: `findSubModuleDirs(rootDir)`
(`gotest.go:798`, direct child directories with a `go.mod`), the same list `gotest` already runs.

## Stage 1 — failing test first: `tests/dependents_guard_test.go`

Add `TestUpdateDependentModule_TidiesTestSubmodule`, built like
`TestUpdateDependentModule_DirtyTreeCommitsOnlyGoModAndSum` in the same file (same fake library
`github.com/test/mylib`, same bump to `v0.0.1`), plus: the dependent has `tests/go.mod` with
`module <dep>/tests`, `require <dep> v0.0.0`, `replace <dep> => ..`, and a `tests/x_test.go` that
imports the library through the dependent, so `tests/go.mod` is out of date after the root bump.
Commit it first so the tree starts clean.

Assert after `UpdateDependentModule`:
1. running `go mod tidy` again in `tests/` changes nothing (`git diff --quiet tests/go.mod tests/go.sum` succeeds after it);
2. the outcome is not a failure;
3. for the deps-only variant (dirty tree: an untracked `notes.txt` as in the existing test), the
   commit contains `go.mod`, `go.sum`, `tests/go.mod`, `tests/go.sum` and nothing else.

Commit this stage with the test **failing** (red), as **bug-fix-flow** requires.

## Stage 2 — `go_handler.go` (`UpdateDependentModule`)

- Right after the root `go mod tidy` (step 5), for each `sub := range findSubModuleDirs(depDir)`
  run `go mod tidy` in `sub`; a failure is reported exactly like the root one
  (`g.reportFail(depName, fmt.Errorf("go mod tidy failed in %s: %s", rel, extractFirstFailure(output)))`
  with `rel` the subdirectory name).
- Build the list of files once: `modFiles := []string{"go.mod", "go.sum"}` plus, per submodule,
  `<rel>/go.mod` and `<rel>/go.sum` (only those that exist).
- The deferred revert runs `git checkout -- <modFiles...>`; the deps-only commit calls
  `git.CommitPaths(commitMsg, modFiles...)`. Nothing else changes.

## Stage 3 — docs

`README.md`, the cascade section of `gopush`: one sentence — "test submodules (`tests/go.mod`)
are tidied and committed with the root `go.mod`."

## Delivery check

```
go install ./cmd/gotest && gotest
```

- A PR that does not compile, or whose tests could not run, is a failed delivery.
- Never pause to ask permission: when the check passes, commit, push and open the PR. Stuck 30
  minutes: open the PR listing what fails.
- `git diff --stat main` lists only: `go_handler.go`, `tests/dependents_guard_test.go`,
  `README.md`, this plan.

## Stages

| # | Files | Done when |
|---|---|---|
| 1 | `tests/dependents_guard_test.go` | new test fails for the traced reason (commit red) |
| 2 | `go_handler.go` | new test green, suite green |
| 3 | `README.md` | sentence present |
