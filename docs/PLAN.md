---
PLAN: "refactor: workspace root and go.mod walk come from modfind"
EXECUTOR: jules
REVIEWER: none
STATUS: running
SESSION: 7906523861212882946
---

> This plan is dispatched via the CodeJob workflow. See skill: agents-workflow.

# Plan — devflow: consume modfind for workspace discovery

Phase **B** of the master plan
`SOURCE_SELECTION_MASTER_PLAN.md` (orchestration only — everything this plan needs is inline).
**Depends on phase A2:** `webtyp.com/modfind` at the tag that exports `WorkspaceRoot`,
`WorkspaceModules` (see
`https://github.com/webtyp/modfind/blob/main/docs/PLAN.md`). Do not start before that tag exists.

Read [AGENTS.md](../AGENTS.md) first. This repo is backend tooling: the standard library is
legitimate, do NOT replace it with `webtyp.com/*` browser packages.

## Why

`workspace.go` (`WorkspaceRoot`, `WorkspaceRootFrom`, `WorkspaceFileExt`, `skipWalkDir`) and two
`filepath.Walk` loops over `go.mod` files (`findAllModules` in `cascade.go`,
`FindDependentModules` in `go_mod.go`) now live in `webtyp.com/modfind`, which `webtyp/app` and
`webtyp/devbrowser` also use. Keeping a second copy here means two definitions of "a module of the
workspace" that will drift. No public behaviour of `gopush`/`codejob` changes except the one listed
under *Behaviour change*.

This plan changes no exported signature of devflow except deleting the exported symbols that
moved, so the design gate lives in modfind's plan (same five answers). It deletes
`workspace.go` entirely.

## Stage 1 — dependency

`go get webtyp.com/modfind@latest` (must be the phase-A2 tag or newer: it exports
`modfind.WorkspaceModules`). Run `go mod tidy`.

## Stage 2 — replace the workspace root

1. In `go_handler.go` (around line 234) replace `WorkspaceRoot(g.rootDir)` with
   `modfind.WorkspaceRoot(g.rootDir)`. Update the doc comment at line ~218 to say
   "see modfind.WorkspaceRoot".
2. `WorkspaceRootFrom` and `WorkspaceFileExt` have NO successor: modfind keeps them private,
   because their only users were devflow's own `workspace.go` and its test.
   `grep -rn "WorkspaceRootFrom\|WorkspaceFileExt" --include='*.go' .` must only hit
   `workspace.go` and `test/workspace_test.go`, which both get deleted. Any other hit means a
   non-test user exists: stop and report it in the PR instead of re-creating the symbol.
3. Delete `workspace.go`.
4. Delete `test/workspace_test.go` (the same test now lives in modfind as
   `workspace_test.go`).

## Stage 3 — replace the two go.mod walks

1. `cascade.go`, `findAllModules(searchPath)`: keep its signature and its return value
   (`map[string]string`, dir → module path) and its rule "skip the module whose dir equals
   `g.rootDir`", but build it from `modfind.WorkspaceModules(searchPath)` instead of walking:
   ```go
   mods, err := modfind.WorkspaceModules(searchPath)
   if err != nil {
   	return nil, err
   }
   absRoot, _ := filepath.Abs(g.rootDir)
   modules := make(map[string]string, len(mods))
   for _, m := range mods {
   	if absDir, _ := filepath.Abs(m.Dir); absDir == absRoot {
   		continue
   	}
   	modules[m.Dir] = m.Path
   }
   return modules, nil
   ```
2. `go_mod.go`, `FindDependentModules(modulePath, searchPath)`: same treatment. Iterate
   `modfind.WorkspaceModules(searchPath)`; keep the existing exclusion of `g.rootDir` and anything
   under it; keep calling `g.HasDependency(filepath.Join(m.Dir, "go.mod"), modulePath)`; return
   the dirs in the same order WorkspaceModules yields them.
3. Delete `skipWalkDir` and `walkSkipDirs` (they were in the deleted `workspace.go`; make sure no
   reference remains).

### Behaviour change (intended — write it in the PR description)

modfind's walk does not enter directories named `testdata` or `_temp`. devflow's walk did. A module
whose `go.mod` sits under a nested `testdata/` or `_temp/` will no longer be bumped by `gopush` as a
dependent. Fixture modules must not be bumped, and `_temp/` is deleted at the end of every plan, so
skipping them is the correct behaviour. If an existing test in `test/` breaks **only** because
its fixture tree places modules under a nested `testdata/` directory below `searchPath`, move the
fixture to a directory not named `testdata` and say so in the PR. Do not change modfind.

## Stage 4 — tests live in `tests/`

Ecosystem rule: every repo keeps its tests in `tests/` (plural), never in `test/`. A test stays at
the root only when it needs an unexported identifier, and then it carries a written justification.
1. `git mv test tests` (the directory, including `testdata/` and `README.md` inside it).
2. Fix every reference to the old path: `grep -rn "test/" --include='*.go' --include='*.md' --include='*.yml' . | grep -v "_test\|go test"`.
   That covers relative `testdata` paths built from `"test"`, docs links and CI workflows.
3. Root-level tests: `cover_merge_test.go` and `extract_failure_test.go` are `package devflow` and
   exercise unexported functions. Apply this mechanical criterion to each:
   - if, after reading it, the test only uses exported identifiers → `git mv` it to `tests/`,
     as `package devflow_test`;
   - otherwise it STAYS at the root, and gets this comment as its first lines:
     `// Root-level test (justified): exercises <unexported identifiers> — <why the behaviour is not
     observable through the exported API>.`
   **Never export a function so a test can reach it.** List the outcome per file in the PR
   description.

## Acceptance

- `gotest` passes.
- `grep -rn "func WorkspaceRoot\|func skipWalkDir\|filepath.Walk(searchPath" --include='*.go' .`
  → empty.
- `grep -rn "webtyp.com/modfind" go.mod` → one `require` line.

## Stages

| # | Stage | Files |
|---|---|---|
| 1 | Dependency | `go.mod`, `go.sum` |
| 2 | Workspace root | `go_handler.go`, `workspace.go` (deleted), `test/workspace_test.go` (deleted) |
| 3 | go.mod walks | `cascade.go`, `go_mod.go` |
| 4 | `test/` → `tests/` | `tests/**`, any file referencing `test/` |
