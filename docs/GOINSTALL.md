# goinstall

A utility command that automatically installs all components of the `devflow` toolset into your `GOPATH/bin`.

## Installation

```bash
go install webtyp.com/devflow/cmd/goinstall@latest
```

## Usage

Run it from the root of the `devflow` project:

```bash
goinstall
```

## What it does

The command performs the following steps:

1. Scans the `./cmd/` directory for subdirectories.
2. For each subdirectory found (e.g., `gonew`, `gotest`, `gopush`), it executes:
   ```bash
   go install -ldflags="-X main.Version=<version>" ./cmd/<name>
   ```
3. Automatically detects and injects the current version tag (from git tags) into each binary. Falls back to "dev" if no tags are found.
4. Provides a clean visual summary of the installation status for each command.

## Output Example

```text
✅ gonew
✅ gotest
✅ gopush
✅ devbackup
✅ devllm
```
