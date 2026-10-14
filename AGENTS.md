# AGENTS.md — webtyp/devflow

<!-- webtyp:base — copied from devskills skill agents-md; do not edit here -->
## Ecosystem rules — read before any change

`docs/PLAN.md` is the spec: no design judgment; anything unclear is a question in the PR, not a guess.

- Public API: typed (no `any`/`interface{}`), explicit, minimal; never export a symbol only a test uses.
- One intent, one path: the old path is deleted in the same change. No silent fallback, `TODO`, commented code or stub.
- Fail at compile time, else a loud diagnostic — never silently. Zero values deny.
- A defect in a dependency is reported, never wrapped or patched here.
- No string literals in logic: named constants. The same logic never lives in two repos.
- `cmd/*/main.go`: parse flags, inject, print/exit only. stdout = data, stderr = logs, exit codes are the contract.
- WASM code: `webtyp/fmt`, `webtyp/time`, `webtyp/json` instead of stdlib; no `map`; `dom.Element` embedded by value.
- `css.go`, `svg.go`, `js.go`, `html.go` carry `//go:build !wasm`; no `ssr.go`, no `front.go`.
- Code and keys in English; UI text is `lang.Text`, translations live in `lang.json`.
- Tests cover behaviour and domain rules, never the look (CSS, colours, markup order, class strings). In `tests/` (external package), real stack with fakes only at the edge; run `gotest`.
- Docs (`README.md`, `docs/*`) updated with the API; permanent docs never link `docs/PLAN.md`.
- Before the PR: `git diff --stat main` lists every file of the plan's stages, and nothing the plan did not order is deleted.
<!-- /webtyp:base -->

## This repo

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
