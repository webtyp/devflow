# gotest

Automated Go testing: runs vet, stdlib tests, race detection, coverage, and WASM tests.

## Installation

```bash
go install webtyp.com/devflow/cmd/gotest@latest
```

## Usage

```bash
gotest [-t seconds] [go test flags]
```

**No arguments**: Runs the full test suite (vet, race, cover, wasm, badges)
**With arguments**: Passes flags to `go test` (fast path, no vet/wasm/badges/cache)

### MCP Tool

`gotest` is also available as a single MCP tool `run_tests` for LLMs:

- **No arguments**: Runs the full suite on the active project root and returns the summary line.
- **`run="TestName"`**: Runs only tests matching the specified name or pattern.

WASM tests and project root detection are handled automatically.

### Options

| Flag | Description | Default |
|------|-------------|---------|
| `-t N` | Per-package timeout in seconds | `30` |
| `-no-cache` | Force re-execution of tests, skipping cache | `false` |
| `-all` | Run all tests including integration tests (sets timeout to 60s) | `false` |

### Examples

```bash
gotest              # Full suite, 30s timeout
gotest -all         # Full suite + integration, 60s timeout
gotest -no-cache    # Full suite, bypass cache
gotest -t 120       # Full suite, 120s timeout
gotest -run TestFoo # Run specific test, 30s timeout
gotest -t 60 -run X # Custom args, 60s timeout
gotest -bench .     # Run benchmarks
```

### Note on Verbose Output
`gotest` now runs internal tests with `-v` by default, but uses `ConsoleFilter` to keep the output clean. It only shows failed tests and critical summaries, making verbose diagnostic data available without the noise.

## What it does

### Without arguments (full suite):

1. Runs `go vet ./...`
23. Runs `go test -race -cover ./...` (stdlib tests)
4. **Exact weighted coverage** using profile merging (`go tool cover`) across all packages.
5. Auto-detects and runs WASM tests in a real browser (`wasmbrowsertest`). Detection is by **build tag, not filename**: the WASM suite activates when a package has a test file present in the `GOOS=js GOARCH=wasm` build but absent from the native build — i.e., gated by `//go:build wasm`. The filename is irrelevant.
6. Detects slowest test (if > 2.0s)
7. Detects WASM released function calls
8. Updates README badges
9. Displays total execution time

### `-tinygo` — compile the WASM suite with TinyGo

```bash
gotest -tinygo
```

By default the WASM suite is built with the **Go** toolchain
(`GOOS=js GOARCH=wasm`), whose backend supports the full standard library. That
means a green `wasm ✅` says **nothing** about TinyGo compatibility: a package
TinyGo would reject still passes.

`-tinygo` closes that gap. It runs the suite through
`wasmbrowsertest -tinygo`, which rebuilds the package with TinyGo (the binary
`go test -exec` hands over came from the Go compiler, so it cannot prove
anything) and serves TinyGo's `wasm_exec.js` — the two toolchains emit different
host imports and their shims are not interchangeable.

Use it on any library that ships to the edge (Cloudflare Workers, `goflare`) or
to the browser via TinyGo, whenever an import is added or removed.

It is opt-in because it is slow: TinyGo compiles through LLVM, so a run takes
minutes instead of seconds. It also bypasses the test cache. If TinyGo is not
installed, the run fails with install instructions
(`go run webtyp.com/tinygo/cmd/tinygoinstall@latest`).

### With arguments (fast path):

1. Runs `go test [your flags] ./...` (stdlib tests)
2. Auto-detects and runs WASM tests with same flags if found
3. Skips vet and badge updates
4. Always filters output for clean results
5. Race detection only if explicitly requested (e.g., `gotest -race -run TestFoo`)

## Test Caching

`gotest` includes an intelligent caching mechanism to avoid re-running tests when the code hasn't changed.

- **How it works**: It generates a unique key for the current module based on its git state (last commit hash + hash of uncommitted changes).
- **Behavior**: If a match is found in the cache, `gotest` returns the previous successful result immediately without executing any tests.
- **Persistence**: Caches are stored in `/tmp/gotest-cache/` and are automatically invalidated if any `.go` file or the git state changes.
- **Note**: Cache is **disabled** when using custom flags (e.g., `-run`, `-v`, `-bench`) to ensure accurate results.

## Output

**Full suite (no arguments):**
```
vet ✅, race ✅, tests ✅, wasm ✅, coverage: 85% (12.4s)
```
The total execution time is appended in parentheses at the end. Slow tests
(`⚠️ slow: TestFoo (3.2s)`) are printed on their own line during the run, not
inside the summary.

**Partial runs:**
- Displays individual package results when using flags.
- Summarizes slow tests if detected.
- Includes total execution time in parentheses.

### Special Detection
- **❌ timeout**: Reports tests that exceeded the per-package timeout (default 30s).
- **⚠️ slow**: Automatically reports the single slowest test if it exceeds 2.0s.
- **⚠️ WASM**: Summarizes calls to released functions in WASM tests.
- **(×N)**: Automatically deduplicates identical output lines for cleaner logs.

**Custom flags (e.g., `gotest -run TestFoo`):**
```
tests ✅ (0.4s)
```

**Custom flags with WASM tests (e.g., `gotest -run TestWasmFoo`):**
```
tests ✅, wasm ✅ (1.2s)
```

**Cached run:**
The message is identical to the original run, but it executes instantly.

**On failure:**
Shows only failed tests with error details, filters out passing tests. Failed runs are never cached.

**Note:** Output is always filtered for clean results, even when using `-v` flag.

## Exit codes

- `0` - All tests passed
- `1` - Tests failed, vet issues, or race conditions detected

## Timeout

`gotest` uses a **per-test stall watchdog** (default 30s). Unlike Go's native cumulative package timeout, `gotest` kills the process only if a *single* test makes no progress for the specified duration.

If a test stalls:
```
❌ timeout: TestSlowOperation stalled >30s (no progress)
```

Override with `-t`:
```bash
gotest -t 120       # 120s per test stall
```

A **backstop timeout** (10x the watchdog limit) is also injected into `go test -timeout` to catch genuine package-level hangs where the watchdog might be bypassed. If hit, it reports:
```
❌ timeout: package exceeded 300s total (backstop)
```

If you pass `-timeout` directly (Go's native flag), `gotest` still injects its watchdog using that value (or its default if not parseable), but respects your explicit package timeout.

## Notes

- Supports all standard `go test` flags
- Auto-detects test types when running full suite
- Filters verbose output automatically (even with `-v`)
- Badge updates in `README.md` under `BADGES_SECTION` (only for full suite runs)
- Cache is disabled when using custom flags for accurate results
