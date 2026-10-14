# conenv

`conenv` is a declarative, repository-first manager for personal development environments. It installs tools, links or renders configuration files, provisions Codex skills and MCP servers, detects drift, and can restore replaced files.

The CLI and configuration repository are deliberately independent. Windows is the supported v0.1 runtime; the Go core and manifest format are designed to accommodate Linux and macOS later.

## Install

Build with Go 1.27 or newer:

```powershell
go build -o conenv.exe ./cmd/conenv
```

Windows links require Developer Mode or permission to create symbolic links.

## Quick start

Register a configuration repository once, then validate and apply its selected profile:

```powershell
./conenv.exe use C:\path\to\conenv-config
./conenv.exe config
./conenv.exe validate
./conenv.exe plan
./conenv.exe apply --select
./conenv.exe apply --selection selection.yaml --yes
./conenv.exe doctor
```

`--select` opens a keyboard-driven hierarchical checklist, so a machine can install only the programs and AI resources it needs. Components are selected by default, and nested resources such as individual Codex skills can be toggled independently; a `~` mark denotes a partially selected parent. Use the arrow keys and Space to change the selection, `/` to filter, and Enter to continue. Interactive `apply` includes a plan preview and lets you return to the checklist before confirming. `conenv select --write selection.yaml` exports the same choice for agents and scripts; `--selection` replays it exactly, while repeatable `--only` accepts stable targets such as `git` or `codex/skills/release-notes`. These three selection modes are mutually exclusive. `plan`, `status`, and `doctor` are read-only. `--yes` skips only the apply preview and confirmation.

## Commands

```text
conenv validate [--repo DIR] [--profile NAME]
conenv plan     [--repo DIR] [--profile NAME] [--select | --selection FILE | --only TARGET...] [--json]
conenv apply    [--repo DIR] [--profile NAME] [--select | --selection FILE | --only TARGET...] [--yes]
conenv status   [--repo DIR] [--profile NAME] [--select | --selection FILE | --only TARGET...] [--json]
conenv doctor   [--repo DIR] [--profile NAME] [--select | --selection FILE | --only TARGET...] [--json]
conenv select   --write FILE [--repo DIR] [--profile NAME]
conenv profile  list|show NAME [--repo DIR] [--json]
conenv component list|show NAME [--repo DIR] [--json]
conenv history  list|show RUN_ID [--json]
conenv restore  RUN_ID [--yes]
conenv use      REPOSITORY
conenv config   [--json]
conenv schema   [--write FILE]
conenv version
```

Exit status `0` means success, `1` means an operational failure, `2` means invalid input or configuration, and `3` means a completed status or health inspection found drift or an unhealthy check.

## Documentation

- [Configuration guide](docs/configuration.md) — manifests, profiles, files, secrets, packages, health checks, Codex, MCP, and recovery
- [Example manifest](examples/conenv.yaml)
- [Embedded manifest JSON Schema](internal/assets/schema/conenv.v1.schema.json)

## Development

```powershell
gofmt -w cmd internal
go vet ./...
go test ./...
go build ./cmd/conenv
```
