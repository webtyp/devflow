# DevFlow
<img src="docs/img/badges.svg">
<img src="docs/img/badges.svg">

Complete Go development automation: project init, testing, versioning, updates, and backups. Single-line output optimized for AI agents and terminals.

## Commands

- **[gonew](docs/GONEW.md)** - Initialize new Go projects
- **[gotest](docs/GOTEST.md)** - Run tests, vet, race detection, coverage and badges
- **[gopush](docs/GOPUSH.md)** - Automated publish workflow: test + push + update dependents
- **[devbackup](docs/DEVBACKUP.md)** - Configure and execute automated backups
- **[badges](docs/BADGES.md)** - Generate SVG badges for README (test status, coverage, etc.)
- **[goinstall](docs/GOINSTALL.md)** - Install all devflow commands at once
- **[codejob](docs/CODEJOB.md)** - Send coding tasks to AI agents (Jules, etc.)

## Configuration

- **[GitHub Auth](docs/GITHUB.md)** - Configure GitHub authentication (OAuth, tokens, multi-account)

## Roadmap

- **[PLAN: bug-fix orchestrator](docs/PLAN.md)** - Master plan coordinating the sub-plans below
- **[PLAN: gotest stall watchdog](docs/GOTEST_TIMEOUT_PLAN.md)** - Per-test stall detection replacing the per-package timeout budget ([diagram](docs/diagrams/GOTEST_WATCHDOG.md))
- **[PLAN: gopush same-repo submodules](docs/GOPUSH_SELFDEP_PLAN.md)** - Stop gopush/codejob from re-dirtying a just-published module whose submodules depend on it

## Installation

```bash
# Install all commands at once (includes codejob and all other tools)
go install webtyp.com/devflow/cmd/goinstall@latest && goinstall
```

Or install a single command — see each tool's doc linked above.

## Features

- **Transactional CodeJob** - Branch switching is atomic; state (PLAN.md, .env) is only mutated after a verified checkout.
- **Intelligent push** - Auto-pulls with `--rebase` on non-fast-forward rejection
- **Zero config** - Auto-detects tests, project structure, WASM environments
- **Minimal output** - Single-line summaries for terminals and LLMs
- **Smart versioning** - Auto-increments tags, skips duplicates
- **Multi-account** - Switch GitHub orgs easily (cdvelop, veltylabs, webtyp)
- **Dependency updates** - Auto-updates dependent modules in workspace
- **Full testing** - Combines vet, tests, race detection, and **exact weighted coverage** across all packages

## License

MIT
