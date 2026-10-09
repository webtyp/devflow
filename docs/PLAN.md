---
PLAN: "feat(codejob)!: explicit commands — bare codejob only shows help and status, never dispatches, merges or publishes"
EXECUTOR: jules
REVIEWER: none
---

> This plan is dispatched via the CodeJob workflow. See skill: agents-workflow.

# Plan — `codejob`: every action is an explicit command

## 0. Context (read first)

`codejob` (`cmd/codejob/main.go`, logic in `codejob.go`, `codejob_state.go`, `cli.go`) drives a
plan loop whose state lives in the frontmatter of `docs/PLAN.md` (`STATUS: dispatch → running →
reviewing → review`, then the plan is published and deleted).

Today **bare `codejob` (no arguments) advances the state machine one step**, and the step
depends on `STATUS`:

```go
// codejob.go, CodeJob.Run
if message != "" || strings.ToLower(meta.Status) == "review" {
    ... MergeAndPublish(...)   // merges the PR, runs gopush (tag + push), deletes docs/PLAN.md
}
if status == "running" { return c.checkStatus(meta) }
...
return c.Send(DefaultIssuePromptPath) // dispatch
```

**Verified incident (2026-10-09):** a reviewer ran bare `codejob` to *fetch* the agent's
corrected PR. `STATUS` was already `review`, so the same call **tried to merge and publish** an
unreviewed correction. It only failed because the local branch was behind the remote (push
rejected). Had the branch been current, unreviewed code would have been tagged and published.

The same command meaning "look", "dispatch", "fetch the PR" or "publish" depending on hidden
state is the defect. Every action must be an explicit command; the bare command must be
harmless.

## Design gate

### 1. Prior art
- **git / gh / kubectl / terraform**: every state change is a named subcommand
  (`gh pr merge`, `terraform apply`); the bare command prints help. `terraform apply` vs
  `plan` is the precedent for separating "look" from "change".
- **The ecosystem's own CLI contract** (skill core-principles, "AI-Consumable CLIs"): *no args →
  print help to stdout, exit 0; never block or act by default.* `codejob` violates it today.
- **npm / cargo publish**: publishing is always its own explicit verb.

### 2. Novice-name test
`codejob dispatch` (send the plan), `codejob pull` (bring the agent's PR here),
`codejob reply "…"`, `codejob approve`, `codejob close "message" [tag]` (merge and publish).
`codejob` alone: help + where this repo's plan stands.

### 3. Complexity ledger
```
Concepts the developer must learn   +3 verbs (dispatch, pull, close) / −1 (hidden state-dependent bare call)
Files they must touch to do X        0
Lines at the call site               +1 word per command
Ways to do the same thing            −2 ('codejob "msg"' and bare-on-review both closed the loop → only 'close')
```

### 4. Where it belongs
`webtyp/devflow`, which owns `codejob`.

### 5. What this deletes
- The positional-message form `codejob "message" [tag]` (replaced by `codejob close "message" [tag]`).
- The `--reply` and `--approve` flags (replaced by the `reply` and `approve` commands).
- The state-dependent branching of `CodeJob.Run` (replaced by one method per command).
- **Kept unchanged** (machines depend on them): `--ci <phase>` (used by the generated GitHub
  Actions workflow `templates/codejob.yml`), `--init-action` (+ `--force`, `--org`,
  `--visibility`), `--reset-gh-token`, `--release` (now only valid with `close`).

## 1. Target CLI (normative)

```
codejob                          help + read-only status block; exit 0; changes nothing
codejob help | -h | --help       help only; exit 0
codejob dispatch                 STATUS absent or "dispatch" → send docs/PLAN.md to the EXECUTOR
codejob pull                     STATUS "running"  → ask the agent; PR ready → check out its branch
                                                     in place, STATUS → review | reviewing
                                 STATUS "review" | "reviewing" → fetch the PR branch and
                                                     fast-forward the local branch to it
codejob reply "text"             answer the agent session of docs/PLAN.md
codejob approve                  approve the plan that session is waiting on
codejob close "message" [tag] [--release]
                                 STATUS "review" → merge the PR, publish (gopush), delete docs/PLAN.md
codejob --ci <phase> | --init-action [...] | --reset-gh-token     unchanged
```

**Status block** printed by bare `codejob` (read-only: it reads `docs/PLAN.md` and never calls the
agent API, git or GitHub):
```
docs/PLAN.md: STATUS review · PR https://github.com/... · session 123
next: codejob pull   (or: codejob close "message" when the review is done)
```
No `docs/PLAN.md` → `no docs/PLAN.md in this directory`. The suggested next command follows the
table in §2.

**Errors** (stderr, exit code 2 for usage errors, 1 for runtime errors). Texts are constants:
- unknown command / extra words → `codejob: unknown command "<x>"; run codejob for help`
- `dispatch` when STATUS is not absent/`dispatch` → `codejob: plan is <status>; dispatch only
  sends a plan whose STATUS is dispatch`
- `pull` when STATUS is absent/`dispatch` → `codejob: plan was not dispatched yet; run codejob dispatch`
- `close` without a message → `codejob: close needs a commit message: codejob close "message" [tag]`
- `close` when STATUS is not `review` → `codejob: plan is <status>; close only publishes a plan in
  review (run codejob pull first)`
- `--release` with any command other than `close` → usage error.

## 2. Behaviour

| STATUS | `dispatch` | `pull` | `close "msg"` | bare: suggested next |
|---|---|---|---|---|
| absent / dispatch | sends (as `Send` today) | error | error | `codejob dispatch` |
| running | error | `checkStatus` (as today) | error | `codejob pull` |
| reviewing | error | fast-forward PR branch; print "reviewer is reviewing" | error | `codejob pull` |
| review | error | fast-forward PR branch | `MergeAndPublish` | `codejob pull` / `codejob close "message"` |

**Fast-forward rule** (`pull` in review/reviewing, and as the first step of `close`): `git fetch
origin <pr-branch>`; if the local branch is behind → `git merge --ff-only origin/<pr-branch>`; if
it has local commits the remote lacks, keep them (they are the reviewer's corrections; `close`
pushes them as today); if the two **diverged** (both have commits the other lacks) → stop with
`codejob: local branch and the PR branch diverged; reconcile them by hand before closing` and do
**not** merge or publish. This closes the incident: `close` can never publish without first having
the agent's latest commits, and can never overwrite them.

`MergeAndPublish` keeps its current behaviour otherwise (commit uncommitted review corrections,
push, merge, gopush, delete the plan).

## 3. Code changes

| Stage | Files | Content |
|---|---|---|
| 1 | `cli.go` | `CodeJobCLIOpts` gets `Command string` (one of unexported constants `cmdDispatch`, `cmdPull`, `cmdReply`, `cmdApprove`, `cmdClose`, `""`), `Message`/`Tag` only filled for `close`, `ReplyText` for `reply`. Delete `Reply`/`Approve` flag parsing and the bare-positional message. Unknown first word → a parse error value (not a silent message). Keep `ParseCodeJobArgs` only if something outside the package uses it (`grep`); otherwise delete it |
| 2 | `codejob.go` | Replace `Run(message, tag, isRelease)` with explicit methods: `Dispatch() (string, error)`, `Pull() (string, error)`, `Close(message, tag string, isRelease bool) (string, error)`, `StatusLine() (string, error)` (read-only). Each validates STATUS per §2 with the error constants. `RunCI` unchanged |
| 3 | `codejob_state.go` | the fast-forward rule as one unexported function used by `Pull` (review/reviewing) and at the start of `MergeAndPublish` |
| 4 | `cmd/codejob/main.go` | thin: parse, switch on `Command`, print, exit codes. `showHelp()` rewritten for §1 (the "Workflow" section: 1 `codejob dispatch` · 2 `codejob pull` (repeat until the PR is here; answer questions with `codejob reply`) · 3 review · 4 `codejob close "message"`) |
| 5 | tests (repo convention: root `*_test.go`, e.g. `codejob_test.go`, `codejob_state_test.go`) | §4 |
| 6 | `docs/CODEJOB.md`, `docs/diagrams/CODEJOB_FLOW.md`, `README.md` | every bare `codejob` that meant an action becomes its command; add a short "Why explicit commands" paragraph citing the incident |

## 4. Tests

Use the package's existing fakes (runner, Jules driver, publisher — read `codejob_test.go` and
`codejob_state_test.go` for them). No network.
1. Bare `codejob` with `STATUS: review` → prints the status block; the fake publisher, runner and
   driver record **zero** calls.
2. Same for each STATUS (absent, dispatch, running, reviewing): zero side effects, correct "next".
3. `dispatch` on dispatch → driver `Send` called once; on running/review → error constant, no call.
4. `pull` on running with a PR ready → checkout + STATUS review (as today's test of `checkStatus`).
5. `pull` on review with the local branch behind → runner receives `git fetch` + `git merge --ff-only`.
6. `close` on review, branch diverged → error, publisher **not** called.
7. `close` on running → error, publisher not called. `close` without message → usage error.
8. Parser: `codejob "some message"` → unknown-command error (the old form no longer closes).
9. `--ci publish` still routes to `RunCI` (unchanged).

## 5. Code rules (non-negotiable)
- This repo is **host tooling**, not WASM: it legitimately uses the standard library (`fmt`,
  `strings`, `os`, `os/exec`). Do NOT replace those imports.
- `cmd/` stays thin (parsing, wiring, print/exit); every decision is a library function.
- stdout = data (help, status block, results); stderr = errors and logs.
- Repeated strings (commands, statuses, error texts) are constants.
- Do not change `templates/codejob.yml` or the `--ci` phases.

## 6. Acceptance criteria
- `gotest ./...` green.
- `grep -n "status == \"review\"\|Status) == \"review\"" codejob.go` → no branch that publishes
  outside `Close`.
- `codejob` with no args in a repo whose plan is in review exits 0 and leaves `git status` and
  `docs/PLAN.md` byte-for-byte unchanged (covered by test 1).
