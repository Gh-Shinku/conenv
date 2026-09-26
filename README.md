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
./conenv.exe apply
./conenv.exe doctor
```

`plan`, `status`, and `doctor` are read-only. `apply` displays its actions and requests confirmation unless `--yes` is supplied.

## Commands

```text
conenv validate [--repo DIR] [--profile NAME]
conenv plan     [--repo DIR] [--profile NAME] [--json]
conenv apply    [--repo DIR] [--profile NAME] [--yes]
conenv status   [--repo DIR] [--profile NAME] [--json]
conenv doctor   [--repo DIR] [--profile NAME] [--json]
conenv restore  RUN_ID [--yes]
conenv use      REPOSITORY
conenv config   [--json]
conenv schema   [--write FILE]
conenv version
```

Exit status `0` means success, `1` means an operational failure, and `2` means invalid input, drift, or an unhealthy check.

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
