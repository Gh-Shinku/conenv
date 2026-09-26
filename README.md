# conenv

`conenv` is a declarative, repository-first manager for personal development environments. It installs tools, links or renders configuration files, provisions Codex skills and MCP servers, detects drift, and can restore replaced files.

Windows is the supported v0.1 runtime. The Go core and manifest format are designed to accommodate Linux and macOS later.

## Install

Build with Go 1.27 or newer:

```powershell
go build -o conenv.exe ./cmd/conenv
```

Windows links require Developer Mode or permission to create symbolic links.

## Quick start

Edit `conenv.yaml`, then validate and apply the selected profile:

```powershell
./conenv.exe validate
./conenv.exe plan
./conenv.exe apply
./conenv.exe doctor
```

`plan`, `status`, and `doctor` are read-only. `apply` displays its actions and requests confirmation unless `--yes` is supplied.

## Commands

```text
conenv validate [--root DIR] [--profile NAME]
conenv plan     [--root DIR] [--profile NAME] [--json]
conenv apply    [--root DIR] [--profile NAME] [--yes]
conenv status   [--root DIR] [--profile NAME] [--json]
conenv doctor   [--root DIR] [--profile NAME] [--json]
conenv restore  RUN_ID [--yes]
conenv version
```

Exit status `0` means success, `1` means an operational failure, and `2` means invalid input, drift, or an unhealthy check.

## Documentation

- [Configuration guide](docs/configuration.md) — manifests, profiles, files, secrets, packages, health checks, Codex, MCP, and recovery
- [Manifest JSON Schema](schema/conenv.v1.schema.json)

## Development

```powershell
gofmt -w cmd internal
go vet ./...
go test ./...
go build ./cmd/conenv
```
