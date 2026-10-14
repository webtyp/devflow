---
PLAN: "fix(codejob): close resumes a PR GitHub already merged instead of failing on the deleted branch"
EXECUTOR: jules
REVIEWER: none
STATUS: review
SESSION: 3966779266703082162
PR: https://github.com/webtyp/devflow/pull/48
REVIEWED_COMMIT: 3a6ba1e047ba219b21ad9ffd03f327af282b66af
---

> Read AGENTS.md at the repo root before any change.

# Plan — `codejob close` on an already-merged PR

## Problem (observed 2026-10-14, webtyp/components PR #32)

A previous `codejob close` merged the PR on GitHub (commit "review: corrections before merge" + merge,
branch deleted) and then failed before publishing. Running `codejob close` again must resume: publish
and delete `docs/PLAN.md`. Instead it fails with:

```
Error: codejob: git fetch failed: command failed: git fetch origin main feat-themetoggle-usermenu-preferences-2116987196617954533
fatal: couldn't find remote ref feat-themetoggle-usermenu-preferences-2116987196617954533
```

Cause: `(*CodeJob).Close` (`codejob.go`) calls `c.reviewIsCurrent(meta)` before `MergeAndPublish`.
`reviewIsCurrent` → `PRCommitReport` → `git fetch origin <base> <head>`, and the head branch no longer
exists. `MergeAndPublish` already handles the merged case (`prIsMerged` → resume from the pull,
covered by `TestMergeAndPublish_ResumesWhenPRAlreadyMerged`), but the guard before it makes that path
unreachable. The same happens when a human merges the PR on GitHub and runs `codejob close` locally.

## Fix

`codejob.go`, in `Close`: skip `c.reviewIsCurrent(meta)` when the PR is already merged — a merged PR
has no new agent commits left to review, its content is already on the default branch:

```go
if !prIsMerged(c.runner, meta.PR) {
	if err := c.reviewIsCurrent(meta); err != nil {
		return "", err
	}
}
```

`prIsMerged` already exists in `codejob_state.go`; reuse it, do not duplicate the `gh pr view` call.
No other file of the library changes.

## Test

`tests/codejob_commits_test.go`: add `TestClose_ResumesWhenPRAlreadyMerged`, built like
`TestClose_MergesWhenReviewIsCurrent` (`closeWith`, `pr10()`), with a runner where
`gh pr view <url> --json state --jq .state` answers `MERGED` and `git fetch origin <base> <head>` fails
with `fatal: couldn't find remote ref <head>`. Expect: no error, `published == true`, and no
`gh pr merge` call recorded. It must fail on today's code with the error quoted above.

## Docs

`docs/CODEJOB.md` (section on `close`): one line — "close on a PR that GitHub already merged resumes:
it pulls, deletes `docs/PLAN.md` and publishes; the review check is skipped because nothing is left to review."

| Stage | Files |
|---|---|
| Fix | `codejob.go` |
| Test | `tests/codejob_commits_test.go` |
| Docs | `docs/CODEJOB.md` |

## Executor notes
All tasks completed.
