# conenv

`conenv` is a declarative, repository-first manager for personal development environments. It installs tools, links or renders configuration files, provisions agent harness configuration with skills and MCP servers, detects drift, and can undo an applied run.

The CLI and configuration repository are deliberately independent. Windows is the supported v0.1 runtime; the Go core and manifest format are designed to accommodate Linux and macOS later.

## Install

Install with Go 1.27 or newer:

```powershell
go install ./cmd/conenv
```

Windows links require Developer Mode or permission to create symbolic links.
Keep the installed executable outside the source checkout. A checkout prepared
for a low-integrity Windows sandbox can make binaries built inside it inherit a
low mandatory integrity label; those binaries remain low-integrity when launched
later from a regular PowerShell session and cannot update normal user paths.

## Quick start

Register a configuration repository once, then apply its profile:

```powershell
conenv config C:\path\to\conenv-config
conenv apply
conenv status
```

Before a default is saved, a command also accepts `--repo DIR`, or uses the working directory when it contains `conenv.yaml`.

Everyday commands:

```text
conenv config [DIR]       view or set the configuration repository
conenv apply              preview, confirm, and apply (--dry-run previews only)
conenv status             view the configuration scope and current state
```

There are three commands to remember:

- **`config` owns the configuration** (repository, declarations, scope) and never touches the machine. Point it at a repository with `conenv config DIR`, then inspect what a profile contains and whether it is valid.
- **`status` reports state** and changes nothing. It lists each component in scope as `in_sync`, `drifted (N pending)`, or `error`, and exits with status `3` when anything drifted, so scripts can branch on it. `conenv status --check` additionally runs real health checks.
- **`apply` changes the machine.** It plans, previews, confirms, then commits. `conenv apply --dry-run` only prints the plan, and `conenv apply --undo` reverses a recorded run.

## Commands

```text
conenv config [DIR] [--unset | --check | --profiles | --components
               | --profile-detail NAME | --component-detail NAME
               | --unset-selection | --write FILE]
conenv apply  [--dry-run] [--select | --selection FILE | --only TARGET...]
              [--save] [--write FILE] [--yes]
conenv apply  --undo [RUN_ID] [--list] [--dry-run] [--yes]
conenv status [--check] [--select | --selection FILE | --only TARGET...]

Common flags: [--repo DIR] [--profile NAME] [--json]
```

Exit status `0` means success, `1` means an operational failure, `2` means invalid input or configuration, and `3` means a completed inspection found drift or an unhealthy check. When JSON is requested, stdout contains only the result document; diagnostics and prompts use stderr.

## Scope

`apply` and `status` inspect the same scope. A scope comes from, in order:

1. An explicit selector on the command line: `--select` (interactive checklist), `--selection FILE` (exact replay), or repeatable `--only TARGET`.
2. The machine default saved in `~/.conenv/selection.yaml`.
3. The whole profile resolved from the configuration repository.

Save a scope once and every later `status` or `apply` reuses it without repeating the arguments:

```powershell
./conenv.exe apply --select --save       # pick interactively, then save the choice
./conenv.exe apply --only git --yes --save
./conenv.exe status                      # reports the saved scope
./conenv.exe config --unset-selection    # back to the whole profile
```

`--only` and `--selection` are temporary unless `--save` is given. Interactive `apply` asks whether to save the selection after the plan is confirmed. Targets are stable resource IDs: `COMPONENT`, `COMPONENT/resources`, or `COMPONENT/skills/SKILL`.

`--select` opens a keyboard-driven hierarchical checklist, so a machine can install only the programs and AI resources it needs. Components are selected by default, and agent declarations and individual skills can be toggled independently; a `~` mark denotes a partially selected parent. Use the arrow keys and Space to change the selection, `/` to filter, and Enter to continue. Interactive `apply` then previews the plan and lets you return to the checklist before confirming. `apply --select --write selection.yaml` exports the same choice for scripts; `--selection FILE` replays it exactly. These three selectors are mutually exclusive. `--yes` skips the preview and confirmation.

## Agents

A component declares which agent harnesses it contributes to. `harness` selects an adapter that knows where that harness keeps its global configuration, so declarations stay portable across machines:

```yaml
components:
  ai-agents:
    agents:
      codex:
        harness: codex
        config:
          approval_policy: on-request
        skills:
          - name: release-notes
            source: ai/skills/release-notes
```

Several components can contribute to one agent and one component can target several harnesses; settings are merged into a single configuration file per agent. Skills are linked into the directory the harness adapter reports: a harness that reads the shared `~/.agents/skills` directory serves every harness on the machine, while a harness with a private directory such as Pi's `~/.pi/agent/skills` keeps its own skills. The `global` harness publishes to the shared directory without owning any tool's configuration file, so it accepts skills only. Paths come from the adapter and can be overridden per declaration with `configPath` and `skillsDir`, or per machine with `CONENV_AGENT_<HARNESS>_CONFIG` and `CONENV_AGENT_<HARNESS>_SKILLS_DIR`.

`config` accepts only the settings the adapter manages: an unregistered key is a configuration error, so a typo cannot inject an unknown key into another tool's file. Existing unmanaged keys are retained, generated configuration is written with owner-only permissions, and a change is committed only after the file can be parsed back.

Inspect what conenv knows about the harnesses on this machine:

```powershell
./conenv.exe config --list-agents
```

The [configuration guide](docs/configuration.md#agents) documents the adapter defaults, merging rules, MCP declarations, and skill requirements.

## Undo

`apply` backs up every destination it replaces under `~/.conenv/backups/<run-id>/` and records the run in `~/.conenv/state.json`. Undo reverses that:

```powershell
./conenv.exe apply --undo --list        # recorded runs and whether their backup exists
./conenv.exe apply --undo --dry-run     # what undoing the last run would change
./conenv.exe apply --undo               # undo the last run (asks first)
./conenv.exe apply --undo RUN_ID        # undo a specific run
```

Undo removes destinations that `apply` created, restores the backed-up originals, and rolls back the recorded profile, managed paths, and run metadata. Installed packages are never uninstalled; they are listed as retained. Non-interactive undo requires `--yes`, and `--undo` cannot be combined with `--select`, `--only`, `--selection`, `--save`, or `--write`.

## Documentation

- [Configuration guide](docs/configuration.md) — manifests, profiles, files, secrets, packages, health checks, agents, MCP, and recovery
- [Example manifest](examples/conenv.yaml)
- [Embedded manifest JSON Schema](internal/assets/schema/conenv.v2.schema.json)

## Development

```powershell
gofmt -w cmd internal
go vet ./...
go test ./...
$out = Join-Path $env:TEMP 'conenv-dev.exe'
go build -o $out ./cmd/conenv
```
