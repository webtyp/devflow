---
PLAN: "feat(gopush,gotest): a dependent that fails the cascade is recorded and shown until it upgrades"
EXECUTOR: local
REVIEWER: none
---

> Parked: `docs/PLAN.md` (Install, DEV_INSTANCES) is running. Execute **locally** once it closes (owner's order,
> 2026-11-05): `mv docs/PLAN_CASCADE_FAILURES.md docs/PLAN.md`, implement, `gotest`, `gopush`.

# devflow — a failed cascade is never forgotten

## The hole

`webtyp.com/storage` added the NULL rule. Its `gopush` cascade tried `indexdb`, `sqlt`, `agentmemory`: each failed the
gate, `UpdateDependentModule` reverted their `go.mod` (`go_handler.go`, «Smart Revert Logic») and `reportFail` printed
one console line (`go_handler.go:413`). Nothing kept it: `indexdb` and `sqlt` stayed on `storage v0.0.7`,
`agentmemory` on v0.1.4, with real bugs the new contract exposed (found by hand on 2026-11-05).

## Design (owner chose A)

A cascade failure is written to a ledger and shown on every run until the dependent reaches that version.

- **Ledger**: `$XDG_CACHE_HOME/devflow/cascade_failures.json` (fallback `~/.cache/...`), a JSON array (backend tool:
  stdlib `encoding/json` is legitimate here). One entry per (dependent dir, dependency module):
  ```go
  type CascadeFailure struct {
  	Dir        string // absolute path of the dependent module (where its go.mod is)
  	Module     string // the dependent's module path
  	Dependency string // the module that was published, e.g. webtyp.com/storage
  	Version    string // the version it failed to reach, e.g. v0.1.18
  	Reason     string // first failure line, as reportFail printed it
  	Date       string // YYYY-MM-DD
  }
  ```
  Writes replace an entry with the same (Dir, Dependency); a newer Version overwrites an older one.
- **Recorded** in `RunCascade` (`cascade.go`), in the `err != nil` branch after `processor(...)`: one entry per bump of
  that node. **Cleared** when the same node later publishes or is `already up-to-date` for that dependency.
- **Shown**:
  - `gotest` in a repo whose `go.mod` dir has entries: before the summary, one line each —
    `⚠️ behind: webtyp.com/storage v0.1.4 → v0.1.18 failed the cascade on 2026-10-12: <reason>`; the entry is dropped
    when `go.mod` already requires ≥ Version. Not a failing gate (the repo may be fine today); always printed.
  - `gopush`, at the end of every run (any repo): `Behind after failed cascades:` + one line per open entry,
    `<module> needs <dependency>@<version>`, so the list is seen from wherever one publishes.
- **Seam**: the ledger path is injectable (`Go.SetCascadeLedger(path string)`) so tests use `t.TempDir()`.

## Tests (`tests/cascade_failures_test.go`, rows in `docs/TESTS.md`)

| Test | Setup → call → expected |
|---|---|
| `TestCascadeFailures_FailedDependentIsRecorded` | processor returns error for node B on bump storage@v0.1.18 → ledger has 1 entry {B dir, storage, v0.1.18, reason} |
| `TestCascadeFailures_LaterSuccessClearsIt` | entry exists; cascade publishes B with storage@v0.1.18 → ledger empty |
| `TestCascadeFailures_NewerVersionReplaces` | entry v0.1.18; fails again at v0.1.19 → 1 entry, Version v0.1.19 |
| `TestCascadeFailures_GotestShowsBehind` | entry for dir; go.mod requires v0.1.4 → output contains `behind: webtyp.com/storage v0.1.4 → v0.1.18` |
| `TestCascadeFailures_GotestDropsWhenUpgraded` | entry for dir; go.mod requires v0.1.18 → no line, entry removed |

## Docs

`docs/GOPUSH.md` (cascade section: failures are recorded, where, how they clear) and `docs/GOTEST.md` (the `behind`
line); diagram of the cascade, if one exists under `docs/diagrams/`, gains the «record failure» branch.
