---
PLAN: "feat(codejob): pull lists every PR commit (new since last review, deletions, lines undone) and close refuses unreviewed commits"
EXECUTOR: jules
REVIEWER: none
STATUS: running
SESSION: 13885103654895181283
---

> This plan is dispatched via the CodeJob workflow. See skill: agents-workflow.

# Plan — codejob shows a PR commit by commit and blocks closing unreviewed commits

## 0. Context (read first)

A PR that goes through review rounds accumulates commits: the agent's first implementation,
codejob's own status commits, then one agent commit per round of corrections. Example,
`veltylabs/item_catalog` PR #10 (2026-10-12):

```
a9013a7  feat: ServiceCard                                   (agent, round 0)
74f0aaf  chore: status transition to review [pr …/pull/10]   (codejob)
034c3ea  feat: ServiceCard                                   (agent, round 1 fixes)
561d943  chore: status transition to review [pr …/pull/10]   (codejob)
```

Today `codejob pull` prints only `✅ Jules: PR ready <title> <url>` or
`✅ fast-forwarded <branch>`. Nothing tells the reviewer (a human, or an AI assistant driving
codejob) that there are two agent commits, which one is new since the last review, or what each
one did. Reviewers then look at the last commit or only at the net diff, and conclude wrongly in
both directions:

- `webtyp/layout` PR #42: the net diff showed "no code" because commit 2 deleted everything
  commit 1 added; it was first reported as "the agent did nothing".
- Reading only the last commit makes a round-1 fix look like the whole PR.

And `codejob close` merges whatever the branch holds, even commits pushed after the last review.

This plan makes codejob print the commits, mark what is new, warn about deletions and about
lines a later commit undid, remember what was reviewed, and refuse to close over unreviewed
agent commits.

## Design gate

### 1. Prior art
- GitHub PR "Commits" tab and "Changes since your last review" (a per-reviewer marker of the
  last reviewed commit).
- Gerrit patch sets: a review is attached to a specific revision; a new revision is visibly new.
- `git range-diff`: compares what changed between two versions of a series.

### 2. Novice-name test
- `PRCommitReport(runner, prURL, reviewedCommit) (CommitReport, error)` — "report the commits of
  this PR, relative to the last reviewed commit".
- `CommitReport`, `PRCommit` — the report and one commit in it.
- `CodejobCommitPrefix` — "the prefix every commit codejob makes starts with".
- `REVIEWED_COMMIT` (frontmatter, machine-only) — "the last agent commit a reviewer was shown".

### 3. Complexity ledger
+1 exported function, +2 exported types, +1 frontmatter key (`FrontmatterKeyReviewedCommit`,
`PlanMeta.ReviewedCommit`), +1 exported constant, +3 error texts. Every commit message codejob
writes changes prefix (`chore: …` → `chore(codejob): …`). No new command or flag.

### 4. Where it belongs
`webtyp/devflow`, next to `fastForwardPR` and `CheckoutPRBranch` (`codejob_state.go`), because
`Pull` and `Close` (`codejob.go`) are the only callers.

### 5. What it deletes
The bare `✅ fast-forwarded <branch>` message (replaced by the report). Every hand-written
`"chore: …"` commit message literal in codejob (replaced by constants built on the prefix).

## 1. Rules

- **This repo is backend tooling (`!wasm`): stdlib (`strings`, `fmt`, `os`, `net/http`,
  `strconv`) is legitimate here. Do NOT "fix" those imports to webtyp.com/fmt.**
- Every git/gh call goes through the injected `gitmod.Runner` (`c.runner`), never `exec`
  directly — that is what lets tests fake it (`tests/fake_runner_test.go`, `respond` func).
- **All tests in `tests/`** (package `devflow_test`, exported API only). Never export a symbol
  only for a test. Prerequisite: `go install webtyp.com/devflow/cmd/gotest@latest`; run with
  `gotest`, never `go test`.
- No string literal repeated in logic: commit messages, frontmatter keys and output markers are
  constants.

## 1.1 Method: diagram → failing tests → code (TDD), 100% coverage of new code

- Source of truth: [`docs/diagrams/CODEJOB_PR_COMMITS.md`](diagrams/CODEJOB_PR_COMMITS.md)
  (already written). Every branch has its exact test in §6.
- Per stage: write that stage's tests (new exported signatures returning zero values or
  `panic("not implemented")` so they compile) → `gotest` → **confirm they fail** → commit
  `test(codejob): …` → implement → green → commit `feat(codejob): …`. The PR history must show
  each red commit before its green one.
- Coverage of `PRCommitReport`, its helpers, the new `Pull`/`Close` branches and
  `CodejobCommitPrefix` users: 100.0% in
  `go test -coverpkg=webtyp.com/devflow -coverprofile=/tmp/c.out ./tests/... && go tool cover -func=/tmp/c.out`.

## 2. Stage 1 — one prefix for codejob's own commits

`codejob.go` (or a new `codejob_commits.go`):

```go
// CodejobCommitPrefix starts every commit codejob writes itself (status
// transitions, sync, review mark). A reviewer lists them apart from the agent's.
const CodejobCommitPrefix = "chore(codejob): "
```

Replace every codejob-authored message (`codejob.go` lines that today say
`chore: status transition to %s [pr %s]`, `chore: sync before codejob dispatch`,
`chore: status transition to running [session %s]`, `chore: no reviewer set, status to review`,
`chore: status transition to reviewing`, `chore: reviewer approved, status to review`,
`chore: round cap exceeded, handing over to human`,
`chore: changes requested, re-dispatching to corrector [round %d]`) by unexported constants or
format strings built as `CodejobCommitPrefix + "status transition to %s [pr %s]"`, etc. Update
the tests that asserted the old texts. Acceptance:
`grep -n '"chore: ' codejob*.go` → empty.

## 3. Stage 2 — `PRCommitReport` (diagram §1)

`codejob_commits.go`:

```go
// PRCommit is one commit of a PR, oldest first.
type PRCommit struct {
	SHA       string   // full sha
	Short     string   // 7 chars
	Author    string
	Subject   string
	Machine   bool     // subject starts with CodejobCommitPrefix
	New       bool     // agent commit after the reviewed commit (all agent commits when none was reviewed)
	Files     int      // files touched
	Added     int
	Removed   int
	Deleted   []string // files this commit deletes
}

// CommitReport is what a reviewer must see before judging a PR.
type CommitReport struct {
	Base, Head     string // branch names
	Commits        []PRCommit
	LastAgent      string // sha of the newest non-machine commit, "" if none
	NetFiles       int
	NetAdded       int
	NetRemoved     int
	UndoneLines    int    // sum of agent Added minus NetAdded, when positive
}

func PRCommitReport(runner gitmod.Runner, prURL, reviewedCommit string) (CommitReport, error)

// String renders the report for the terminal (format in §3.2).
func (r CommitReport) String() string
```

### 3.1 Commands (exact)

1. `gh pr view <prURL> --json baseRefName,headRefName --jq '.baseRefName+" "+.headRefName'`
   → error text `codejob: could not read PR %s: %w`.
2. `git fetch origin <base> <head>` → `codejob: git fetch failed: %w`.
3. `git log --reverse --format=%H%x09%an%x09%s origin/<base>..origin/<head>` →
   `codejob: could not list PR commits: %w`.
4. Per commit: `git show --format= --numstat <sha>` (added, removed, files; binary `-` counts 0)
   and `git show --format= --diff-filter=D --name-only <sha>` → `codejob: could not read commit %s: %w`.
5. `git diff --numstat origin/<base>...origin/<head>` → net counts (same error as 3).

`New`: walking oldest → newest, every agent commit **after** the one whose SHA equals
`reviewedCommit` is new; when `reviewedCommit` is "" or not found in the list, every agent
commit is new. `UndoneLines = max(0, sum(agent Added) - NetAdded)`.

### 3.2 Output (exact shape; one commit per line, oldest first)

```
Commits on feat-x (2 agent, 2 codejob) — review each one, not only the last:
  🆕 a9013a7  feat: ServiceCard   google-labs-jules[bot]   3 files +190 −1
     74f0aaf  (codejob) status transition to review [pr …/pull/10]
  🆕 034c3ea  feat: ServiceCard   google-labs-jules[bot]   2 files +39 −19
     561d943  (codejob) status transition to review [pr …/pull/10]
     ⚠ 034c3ea deletes: tests/old_test.go
     ⚠ later commits removed 18 lines that earlier commits of this PR added
Net vs main: 4 files +221 −1
Inspect one: git show <sha>
```

Machine commits show `(codejob)` and the subject without the prefix, no stats, never `🆕`.
Deletion warnings, one line per commit that deletes files. The undone-lines warning only when
`UndoneLines > 0`. Markers (`🆕`, `⚠`, `(codejob)`) are constants.

## 4. Stage 3 — `Pull` prints and records; `Close` refuses (diagram §2)

`frontmatter.go`: `FrontmatterKeyReviewedCommit = "REVIEWED_COMMIT"` (machine-only, like
`STATUS`); `PlanMeta.ReviewedCommit string`; read and written like the other keys.

`Pull`:
- PR just ready (`checkStatus`, after `CheckoutPRBranch`): build the report with
  `meta.ReviewedCommit`; append `report.String()` to the returned message; set
  `meta.ReviewedCommit = report.LastAgent` **in the same frontmatter write and commit** as the
  status transition.
- Status `review`/`reviewing` (after `fastForwardPR`): build the report; return
  `report.String()` instead of `✅ fast-forwarded <branch>` (first line stays
  `✅ fast-forwarded <branch>`). If `report.LastAgent != meta.ReviewedCommit`: write it, commit
  `CodejobCommitPrefix + "reviewed up to <short>"`, push.
- A report error never hides the status: return the status line plus
  `⚠ could not list commits: <err>` and do not move `REVIEWED_COMMIT`.

`Close` (before `MergeAndPublish`): build the report;
- `meta.ReviewedCommit == ""` → error
  `codejob: no review recorded; run codejob pull and review the commits first`;
- `report.LastAgent != meta.ReviewedCommit` → error
  `codejob: %d new agent commit(s) since your last review; run codejob pull`
  (count = agent commits marked New);
- report error → return it (never merge blind).

## 5. Stage 4 — docs

- `docs/CODEJOB.md`: review section — "`codejob pull` lists every commit; review each one; close
  refuses commits you were not shown". Add `REVIEWED_COMMIT` to the frontmatter table
  (machine-only).
- `docs/diagrams/CODEJOB_FLOW.md`: link to `CODEJOB_PR_COMMITS.md` from the review step.
- `README.md`: one line in the codejob section.

## 6. Tests — diagram → test (`tests/codejob_commits_test.go`, `tests/codejob_test.go`)

Use the `fakeRunner.respond` func to answer each command by its args.

| Branch | Test |
|---|---|
| `V1-yes` | `TestPRCommitReport_PRViewError` |
| `V2-yes` | `TestPRCommitReport_FetchError` |
| `V3-yes` | `TestPRCommitReport_LogError` |
| `V4-yes` | `TestPRCommitReport_ShowError` |
| `K1-yes/no` | `TestPRCommitReport_SeparatesCodejobCommits` (the 4-commit PR #10 shape) |
| `N1` none reviewed | `TestPRCommitReport_AllAgentCommitsNewWhenNothingReviewed` |
| `N1` reviewed found | `TestPRCommitReport_OnlyLaterCommitsNew` |
| `N1` reviewed unknown sha | `TestPRCommitReport_UnknownReviewedCommitMarksAllNew` |
| `D1-yes/no` | `TestPRCommitReport_WarnsOnDeletions` |
| `R1-yes/no` | `TestPRCommitReport_WarnsWhenLaterCommitsUndoLines` (layout #42 shape: +120 then −120 → net 0) |
| numstat binary `-` | `TestPRCommitReport_BinaryFilesCountZeroLines` |
| no agent commits | `TestPRCommitReport_OnlyMachineCommits` (`LastAgent == ""`) |
| output shape | `TestCommitReport_StringShape` (exact lines of §3.2) |
| prefix everywhere | `TestCodejob_AllOwnCommitsUsePrefix` (drive dispatch/pull/ci phases with the fake runner; every `git commit -m` starts with `CodejobCommitPrefix`) |
| `P1` + `M1-yes` | `TestPull_PRReadyPrintsReportAndRecordsReviewedCommit` (one commit carries status + `REVIEWED_COMMIT`) |
| `P2` + `M1-yes` | `TestPull_ReviewPrintsReportAndRecordsNewMark` |
| `P2` + `M1-no` | `TestPull_ReviewWithNothingNewDoesNotCommit` |
| report error in pull | `TestPull_ReportErrorKeepsStatusAndMark` |
| `C1-yes` | `TestClose_RefusesWithoutRecordedReview` |
| `C2-yes` | `TestClose_RefusesNewAgentCommits` |
| `C2-no` | `TestClose_MergesWhenReviewIsCurrent` |
| report error in close | `TestClose_ReportErrorBlocksMerge` |
| frontmatter | `TestFrontmatter_ReviewedCommitRoundTrip` |

## 7. Acceptance criteria

- `gotest` green; coverage criterion of §1.1 holds.
- Every test of §6 exists with its exact name.
- `grep -n '"chore: ' codejob*.go` → empty; `grep -n "fast-forwarded" codejob.go` appears only
  as the first line of the report output.
- PR history: red `test(codejob)` commit before each `feat(codejob)` commit.
- Before opening the PR: `git diff --stat main` lists every file of the stages table, and no file
  is deleted that this plan does not order deleted.

## 8. Stages table

| Stage | Files |
|---|---|
| 1 | `codejob.go` (messages), `codejob_commits.go` (prefix), affected tests in `tests/` |
| 2 | `codejob_commits.go`, `tests/codejob_commits_test.go` |
| 3 | `frontmatter.go`, `codejob.go`, `codejob_state.go`, `tests/codejob_test.go`, `tests/codejob_commits_test.go` |
| 4 | `docs/CODEJOB.md`, `docs/diagrams/CODEJOB_FLOW.md`, `README.md` |
