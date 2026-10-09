# CodeJob

`CodeJob` is a chain-of-responsibility orchestrator that drives a coding task
(defined in `docs/PLAN.md`) through a sequence of external AI agents and closes
the loop by publishing a new version.

All state lives in the **frontmatter of `docs/PLAN.md`**. There is no `.env`
state and no `CHECK_PLAN.md`: every phase transition is a git commit, so the full
loop runs identically on your machine or in GitHub Actions.

Architecture & diagrams: [diagrams/CODEJOB_FLOW.md](diagrams/CODEJOB_FLOW.md).

## Roles

CodeJob distinguishes two kinds of action on a task, declared in the frontmatter:

| Role | Key | What it does |
|---|---|---|
| **Executor** | `EXECUTOR` | Implements the plan and opens the PR; also applies corrections (commits on the PR branch). |
| **Reviewer** | `REVIEWER` | Judges the PR and posts a **native GitHub review** (`APPROVED` / `CHANGES_REQUESTED`). Never commits code. |

The reviewer is an **optional quality gate before the human** — you still merge
the PR yourself, and the merge is what publishes. Correcting is just executing
again: by default the `EXECUTOR` applies the reviewer's feedback; set the optional
`CORRECTOR` key to route corrections to a different agent.

Everything stays in the PR conversation: the reviewer posts to the same thread
where you already reply corrections, the executor reads it the same way it reads
your comments, and codejob reads the review state to drive the state machine.

## <a name="frontmatter"></a>Plan frontmatter (REQUIRED — dispatch fails without it)

The first line of `docs/PLAN.md` must be `---`:

```markdown
---
PLAN: "feat: what this plan implements"
TAG: v0.2.0
EXECUTOR: jules
REVIEWER: none
---

# Plan — ...
```

| Key | Who writes it | Required | Meaning |
|-----|---------------|----------|---------|
| `PLAN` | human | **yes** | Commit message used when the loop closes (`codejob 'msg'` overrides it). |
| `TAG` | human | no | Explicit version (`v0.2.0`); omitted → `gopush` auto-bumps. |
| `EXECUTOR` | human | no | Agent that implements (default `jules`). |
| `REVIEWER` | human | no | Agent that reviews the PR; `none`/absent → human-only review. |
| `CORRECTOR` | human | no | Agent that applies review feedback (default: the `EXECUTOR`). |
| `REVIEW_GUIDE` | human | no | Path to extra review criteria (e.g. `docs/REVIEW.md`). |
| `STATUS` | machine | — | `dispatch` → `running` → `reviewing` → `review`. |
| `SESSION` | machine | — | Executor session id. |
| `REVIEW_SESSION` | machine | — | Reviewer session id. |
| `ROUND` | machine | — | Executor↔reviewer round count (capped, default 3). |
| `PR` | machine | — | URL of the PR opened by the executor. |

Unknown keys are ignored. `STATUS: dispatch` (or no machine keys) means "pending
dispatch".

## Usage

### Local

```bash
go install webtyp.com/devflow/cmd/codejob@latest

# Every action is an explicit command:
codejob dispatch                 # send docs/PLAN.md to the EXECUTOR
codejob pull                     # fetch/fast-forward the agent's PR branch
codejob reply "..."              # answer the agent
codejob approve                  # approve the plan
codejob close "msg" [tag]        # merge the PR and publish

# Read-only status (no state changes)
codejob
```

### Why explicit commands

Previously, running bare `codejob` advanced the loop blindly depending on hidden state in `docs/PLAN.md` (`STATUS`). A verified incident occurred where a reviewer ran bare `codejob` attempting to *fetch* an agent's corrections, but because the status was already `review`, the command executed a merge and publish instead. Now, `codejob` alone prints a harmless status block and stops; changes require explicit verbs like `dispatch`, `pull`, and `close`.

### Local usage

`codejob` commands **always run from, and against, the local repo you are standing in** — the one you dispatched from. There is nothing else to set up:

- `codejob dispatch` (when `STATUS: dispatch`): sends `docs/PLAN.md` to the `EXECUTOR`, `STATUS` → `running`.
  The executor is told to run every stage and open the PR **without ever stopping to ask**: a
  question pauses its session until someone answers, and the loop stalls. What it could not do
  goes under a final `## Executor notes` heading of the plan and in the PR description, so
  read that section first when reviewing. It is also told never to edit the plan's frontmatter.
- `codejob pull`:
  - When `STATUS: running` and no PR yet: reports the agent is still working, **unless the session stopped**: waiting for a reply, waiting for plan approval, finished without a PR, or failed. Then it prints that state and the agent's last message.
  - When `STATUS: running` and PR is ready: **checks out the PR branch in this same local clone**, `STATUS` → `review`.
  - When `STATUS: review`/`reviewing`: uses a fast-forward rule (`git fetch` + `git merge --ff-only`) to sync your local branch with the PR branch without accidentally publishing it or overwriting local corrections.

Never `gh repo clone` the repo elsewhere or `gh pr checkout` by hand to inspect
a plan's PR — `codejob pull` handles it and correctly syncs branches for you.

### Talking to the agent

When the session stopped, answer it from the same repo. Both read the session id from the
`docs/PLAN.md` frontmatter and use the Jules API key from the keyring:

```bash
codejob reply "Yes, delete Tilde too. Finish every stage and open the PR."
codejob approve        # the session is waiting for plan approval
```

`reply` resumes the session (also one that finished without a PR: tell it to open the PR).
`STATUS` does not change; run `codejob pull` later to see the PR.

```bash
# Close the loop with a required message and optional tag override.
codejob close 'feat: implemented feature'
codejob close 'feat: implemented feature' v0.3.0
```

Review corrections on the PR branch reach the merge either way: uncommitted changes are
committed as "review: corrections before merge", and the branch is always pushed before
`gh pr merge`, so corrections you already committed are not lost.

### Cloud (one-time setup, then zero-touch)

```bash
# Scaffold the workflow and register the secrets from your keyring.
codejob --init-action                          # this repo only
codejob --init-action --org webtyp --visibility all   # once for the whole org
```

After that, the loop runs without opening your PC:

- Edit the `docs/PLAN.md` header and commit → the workflow dispatches the executor.
- The (optional) reviewer runs when the PR opens and posts its review.
- You review the PR from web/mobile and **merge** → the workflow publishes
  (`gopush`, tag-only) and deletes `docs/PLAN.md`.

The workflow invokes `codejob --ci <phase>` (`dispatch`, `review`, `verdict`,
`publish`); you never call `--ci` yourself.

## Tokens & secrets

One identifier everywhere — keyring key, environment variable and GitHub Actions
secret share the **same name**:

| Purpose | Name (keyring = env = secret) |
|---|---|
| Agent (Jules) | `JULES_API_KEY` |
| GitHub token (PAT) | `GH_TOKEN` |

- `GH_TOKEN` (not `GITHUB_TOKEN`): Actions secrets cannot start with `GITHUB_`,
  and a commit pushed with the default `GITHUB_TOKEN` does **not** trigger the next
  workflow — the chained cloud loop needs a PAT.
- `codejob --ci` reads these from environment variables (injected by the Action
  from the secrets); locally it reads them from the keyring under the same name.
- `codejob --init-action` reads them from the keyring and registers them as
  secrets (repo-level, or org-level with `--org`).

### Renaming keyring keys (one-time, manual)

Token names changed to `JULES_API_KEY` / `GH_TOKEN` with no backwards-compat
shim. Simplest path: run `codejob`; when the key isn't found under the new name it
prompts you and stores it. Optional cleanup of the old entries (keyring service
`devflow`):

```bash
# Linux (libsecret)
secret-tool clear service devflow username jules_api_key
# macOS (Keychain)
security delete-generic-password -s devflow -a jules_api_key
# Windows: Credential Manager → search "devflow"
```

To rotate the GitHub token: `codejob --reset-gh-token`.

## Adding a driver

Implement `CodeJobDriver` and register it for a role:

```go
type CodeJobDriver interface {
    Name() string
    SetLog(fn func(...any))
    // Send runs one job. JobSpec carries the role (executor/reviewer), the
    // target branch (for reviews/corrections), the plan path and the prompt.
    Send(spec JobSpec) (string, error)
}
```

## Drivers

| Driver | File | Doc |
|---|---|---|
| Jules | `code_jules.go` | [codejob/JULES_AUTOMATION.md](codejob/JULES_AUTOMATION.md) |
