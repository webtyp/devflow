# AGENTS.md — webtyp/devflow

Working notes for AI agents operating in this repository. End-user docs: [README.md](README.md).

## What this repo is

The developer CLIs of the webtyp ecosystem (`gotest`, `gopush`, `gonew`, `codejob`, …) and the
library behind them. Commands live in `cmd/*`; every decision lives in the library.

## This repo does NOT compile to WASM. The standard library is legitimate here.

It runs on the developer's machine and in CI. `os`, `os/exec`, `path/filepath`, `strings` are
correct here — do **not** replace them with `webtyp.com/*` browser packages.

## The build that defines "done"

```bash
go install ./cmd/gotest   # this repo builds its own test runner
gotest
```

## Rules

- `cmd/*/main.go` only parses arguments, injects dependencies and maps errors to exit codes; every
  conditional is an exported library function.
- Every repeated string (env keys, file names, prefixes, flags) is a named constant.
- Tests live in `tests/` (never `test/`). A root-level test is allowed only with a top-of-file comment justifying the unexported identifier it needs. **Never export a symbol so a test can reach it.** Tests never touch the real home, real git remotes or the network: build trees under
  `t.TempDir()` and use the fake runners in `tests/`.
- Reuse before writing. Module discovery, `*.code-workspace` root detection and walking the
  `go.mod` files of a workspace belong to `webtyp.com/modfind`: call it, never re-implement it here.
