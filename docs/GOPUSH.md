# gopush (universal)

Automated build and publish workflow. It automatically detects if the project is a Go module and applies the appropriate pipeline.

## Usage

```bash
gopush 'commit message' [tag]
```

## Arguments

- **commit message**: Required. The message for the git commit.
- **tag**: Optional. The tag to create. If not provided, it will be auto-generated.
- **--skip-race** or **-R**: Optional. Skip race detection tests (only applicable to Go projects).
- **--no-cascade**: Optional. Publish this module only; do not update dependent modules.

## Behavior

### For Go Projects (contains `go.mod`)

0. **CODEJOB protection**: `gopush` rejects publishing if there is an active `CODEJOB` session in the repo's `.env`, as publishing would move the base branch under the agent.
1. Verifies `go.mod`
2. Runs `gotest` (vet, tests, race, coverage, badges)
3. **Internal submodules sync**: Any submodule inside the repo that depends on the parent module is automatically updated:
   - Ensures a relative `replace` points to the local parent.
   - Bumps the parent requirement to the next tag.
   - Runs `go mod tidy`.
   These changes are included in the same release commit.
4. Commits changes with your message
5. Creates/uses tag
6. Intelligent push: Pushes to remote (auto-pulls/rebases if remote is ahead).
6. Automatically installs binaries with version tag (if `cmd/` exists)
7. Finds dependent modules in search path (default: outermost workspace root containing `*.code-workspace`, or `..` when there is none)
8. For each dependent (in parallel):
   - **Guard check**: If the dependent has an active `CODEJOB` session, it is **skipped** (the repo is NOT touched at all: no `go.mod` write, no `go get`, no tests). If it has local `replace`s for OTHER modules (unrelated to the ones just published), the bump still lands: `go.mod`/`go.sum` are updated, tested and committed, but **without a tag** and without propagating to further dependents (deps-only) — replaces on unrelated modules are left untouched.
   - If up-to-date and no `replace` to remove, it is **skipped** (repo untouched).
   - Removes replace directive for published module
   - Runs `go get module@tag` and `go mod tidy`
   - **Revert on failure**: If tests fail after update, `go.mod`/`go.sum` are reverted.
   - If no other replaces exist: auto-publish dependent.
   - Dependent results print in real-time to the console.
9. Executes backup (asynchronous)

### For Non-Go Projects

1. Commits changes with your message
2. Creates/uses tag
3. Intelligent push: Pushes to remote (auto-pulls/rebases if remote is ahead).
4. Executes backup (asynchronous)

See: [GOPUSH_FLOW.md](diagrams/GOPUSH_FLOW.md)

## Output

**Go Project Success:**
```
✅ vet ok, ✅ tests stdlib ok, ✅ race detection ok, ✅ coverage: 71%, ✅ Tag: v1.0.1, ✅ Pushed ok
```

**Non-Go Project Success:**
```
✅ Tag: v0.1.0, ✅ Pushed ok
```

## Examples

```bash
# Simple push
gopush 'feat: new feature'

# With specific tag
gopush 'fix: critical bug' 'v2.1.3'
```

## Exit codes

- `0` - Success
- `1` - Tests failed, git operation failed, or verification failed

## Note: Special characters in commit messages

When your commit message contains backticks (`` ` ``), `$`, or other shell special characters, use **single quotes** to prevent shell interpretation:

```bash
# ❌ Backticks will fail (shell tries to execute as commands)
gopush "feat: Add `afterLine` parameter"

# ✅ Use single quotes
gopush 'feat: Add `afterLine` parameter'

# ✅ Or escape backticks
gopush "feat: Add \`afterLine\` parameter"
```
