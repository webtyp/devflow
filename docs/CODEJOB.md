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

# Dispatch / advance: runs the phase implied by the current STATUS.
codejob

# Close the loop with an explicit message/tag override (optional).
codejob 'feat: implemented feature'
codejob 'feat: implemented feature' v0.3.0
```

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
