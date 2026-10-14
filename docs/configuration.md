# Configuration guide

The `conenv.yaml` committed to a dedicated configuration repository is the source of truth for an environment. The CLI repository contains only the program, documentation, examples, and embedded schema.

Export the schema into a configuration repository for editor completion by copying [`internal/assets/schema/conenv.v1.schema.json`](../internal/assets/schema/conenv.v1.schema.json) to `C:\path\to\conenv-config\.conenv\conenv.v1.schema.json`.

Then reference it from the manifest:

```yaml
# yaml-language-server: $schema=./.conenv/conenv.v1.schema.json
schemaVersion: conenv/v1
defaultProfile: personal
```

## Repository selection

Conenv locates the configuration repository in this order:

1. `--repo DIR` on the current command.
2. The `CONENV_REPO` environment variable.
3. The default saved by `conenv config DIR` in `~/.conenv/config.json`.
4. The current working directory.

Use `conenv config` to inspect the resolved path and its source, `conenv config DIR` to save a default, and `conenv config --unset` to clear it. Adding `--profiles`, `--components`, `--profile-detail NAME`, or `--component-detail NAME` inspects the inventory without requiring runtime prerequisites, and `conenv config --check` validates the declarations.

## Complete example

```yaml
schemaVersion: conenv/v2
defaultProfile: personal

variables:
  git_name: Ada Example

secrets:
  github_token:
    env: GITHUB_TOKEN

profiles:
  base:
    components: [shell, git]
  personal:
    extends: [base]
    components: [ai-agents]
    variables:
      git_email: ada@example.com

components:
  shell:
    platforms: [windows]
    packages:
      - manager: winget
        id: Microsoft.PowerShell
    files:
      - source: dotfiles/Microsoft.PowerShell_profile.ps1
        target: '{{ .home }}/Documents/PowerShell/Microsoft.PowerShell_profile.ps1'
        mode: link

  git:
    files:
      - source: templates/gitconfig.tmpl
        target: '{{ .home }}/.gitconfig'
        mode: render
    checks:
      - name: git is available
        type: command
        command: git
        args: [--version]

  ai-agents:
    category: ai
    agents:
      codex:
        harness: codex
        config:
          approval_policy: on-request
        skills:
          - name: my-workflow
            source: ai/skills/my-workflow
        mcpServers:
          openaiDeveloperDocs:
            type: http
            url: https://developers.openai.com/mcp
```

## Profiles and components

Profiles select reusable components. A profile can extend multiple profiles:

```yaml
profiles:
  workstation:
    extends: [base, personal]
    components: [windows-tools]
```

Inheritance is resolved from left to right. Parent components are selected before child components, duplicate component names are ignored after their first occurrence, and later variables override earlier values. Cycles, unknown profiles, and unknown components are validation errors.

Components may be restricted by Go platform names:

```yaml
components:
  windows-tools:
    platforms: [windows]
    architectures: [amd64, arm64]
```

Supported platform examples are `windows`, `linux`, and `darwin`; architecture examples are `amd64` and `arm64`. A non-matching component is skipped.

Add menu metadata to categorize and explain components during runtime selection:

```yaml
components:
  powershell:
    category: program
    description: PowerShell profile
  ai-agents:
    category: ai
    description: Shared agent skills
```

`category` accepts `program` or `ai`. If omitted, file/package components default to `program` and agent-only components default to `ai`.

## Variables and paths

These variables are always available:

| Variable | Value |
| --- | --- |
| `home` | Current user's home directory |
| `repo` | Absolute repository root |
| `profile` | Selected profile name |
| `os` | Current Go operating-system name |
| `arch` | Current Go architecture name |

Global and profile variables are accessed through Go template syntax:

```yaml
target: '{{ .home }}/.config/example/config.toml'
```

`${NAME}` expands from manifest variables first and then from the process environment. File sources may be relative to the repository; destinations must resolve to absolute paths. Secrets are prohibited in paths because paths appear in plans, state, and errors.

## Managed files

A component can link repository content or render a template:

```yaml
files:
  - source: dotfiles/gitconfig
    target: '{{ .home }}/.gitconfig'
    mode: link
  - source: templates/tool.toml.tmpl
    target: '{{ .home }}/.config/tool/config.toml'
    mode: render
    sensitive: true
```

`link` creates a symbolic link and never silently falls back to copying. On Windows, enable Developer Mode or otherwise grant symbolic-link permission.

`render` executes a Go text template and replaces the destination atomically:

```text
name = {{ printf "%q" .git_name }}
email = {{ printf "%q" .git_email }}
token = {{ secret "github_token" }}
```

Templates using `secret` are marked sensitive automatically. `sensitive: true` can also be set explicitly. Sensitive files use owner-only permissions where supported.

## Machine-local configuration

`conenv.local.yaml` is loaded after `conenv.yaml` and is ignored by Git. Use it for machine paths, secret references, and components that should not be committed:

```yaml
variables:
  work_root: D:/Work

secrets:
  private_token:
    file: '{{ .home }}/.secrets/private-token'

profiles:
  personal:
    components: [work-machine]
```

Local variables and secrets override committed values. Local profile inheritance and components are appended; a local component with the same name replaces its committed definition.

## Secrets

Secrets are named references to either an environment variable or a local file:

```yaml
secrets:
  api_token:
    env: API_TOKEN
  signing_key:
    file: '{{ .home }}/.secrets/signing-key'
```

Exactly one source is required. Conenv validates that the source exists, resolves its value only when rendering, and excludes values from plans, JSON output, backups metadata, and `~/.conenv/state.json`.

A rendered file or generated agent configuration may still contain a resolved value. Keep those destinations sensitive and outside version control.

## Packages

Windows v0.1 supports `winget` and `scoop`:

```yaml
packages:
  - name: PowerShell
    manager: winget
    id: Microsoft.PowerShell
  - manager: scoop
    id: ripgrep
    version: 14.1.1
```

Conenv checks the exact package ID and optional version before planning installation. The requested package manager must already be available; conenv does not run remote package-manager bootstrap scripts.

Package installation happens before filesystem changes. Successfully installed packages are not removed during rollback because package-manager operations are not reliably reversible.

## Health checks

Path checks assert that a file or directory exists:

```yaml
checks:
  - name: SSH directory
    type: path
    path: '{{ .home }}/.ssh'
```

Command checks execute a program without an intermediate shell and succeed when it exits with status zero:

```yaml
checks:
  - name: Git version
    type: command
    command: git
    args: [--version]
```

`conenv status --check` combines custom checks with desired-state checks for packages, links, rendered files, agent configuration, and skills. Without `--check`, `status` reports drift but does not execute these checks.

## Agents

A component declares which agent harnesses it contributes to. The component name is the repository-side grouping; the agent name under `agents` is the machine-side target, and `harness` selects the adapter that knows where that harness keeps its global configuration.

```yaml
components:
  ai-agents:
    category: ai
    agents:
      codex:
        harness: codex
        config:
          approval_policy: on-request
          sandbox_mode: workspace-write
        skills:
          - name: release-notes
            source: ai/skills/release-notes
```

The same component can target several harnesses, and several components can contribute to the same agent; their settings are merged into one file per agent.

### Global directories

Paths come from the adapter, so a declaration stays portable:

| Harness | Configuration | Skills |
|---|---|---|
| `codex` | `~/.codex/config.toml` | `~/.agents/skills/<name>` |

Skills live in one shared global directory and are linked there once, even when several agents declare the same skill. Every skill source must contain `SKILL.md`.

Override a path only when a machine deviates from these defaults:

```yaml
agents:
  codex:
    harness: codex
    configPath: '{{ .home }}/.codex/config.toml'
    skillsDir: '{{ .home }}/.agents/skills'
```

`CONENV_AGENT_CODEX_CONFIG` and `CONENV_AGENT_CODEX_SKILLS_DIR` override the defaults for experiments and tests. `conenv config --list-agents` prints each registered harness with its resolved paths and whether those directories exist on this machine.

### Settings and merging

`config` accepts only the settings the adapter manages; an unregistered key is a configuration error, so a typo cannot inject an unknown key into a tool's file. Existing unmanaged keys and MCP server entries are retained, though TOML comments and formatting may be normalized. Generated configuration is always handled as sensitive: it is written with owner-only permissions, backed up before a change, reparsed after the write, and rolled back if the result cannot be read back.

`mcpServers` is declared once and rendered into whatever field name the harness expects:

An HTTP server requires a URL:

```yaml
agents:
  codex:
    harness: codex
    mcpServers:
      documentation:
        type: http
        url: https://example.com/mcp
```

A stdio server requires a command and may include arguments and environment values:

```yaml
agents:
  codex:
    harness: codex
    mcpServers:
      localTools:
        type: stdio
        command: npx
        args: [-y, example-mcp]
        env:
          TOKEN: '{{ secret "api_token" }}'
```

See the official [Codex configuration reference](https://developers.openai.com/codex/config-reference) and [MCP guide](https://developers.openai.com/learn/docs-mcp).

When `--select` is used, a component shows its agent as a child node, and each agent exposes its configuration and MCP servers as an `Agent resources` child plus one node per skill. Resource IDs follow the same shape: `ai-agents/agents/codex`, `ai-agents/agents/codex/resources`, and `ai-agents/agents/codex/skills/release-notes`.

## Planning, drift, and recovery

`conenv apply --dry-run` reports changes without mutating the machine. `conenv status` reports the same drift and exits with status `3` when actions are pending. Applying the same manifest twice is a no-op.

Add `--select` to `apply` or `status` to open an interactive hierarchical checklist containing the profile's components supported by the current platform. Components and their nested resources are selected by default; agents and individual skills can be toggled independently, and `~` marks a partially selected parent. Move with the arrow keys or `j`/`k`, toggle with Space, use `a` or `n` for all or none, and press `/` to filter. Enter continues and `q` cancels. Interactive `apply` then previews the generated plan; Escape returns to the checklist and Enter confirms it. `--yes` skips that preview and confirmation.

A scope given on one command is temporary. To stop repeating the same arguments, save it as the machine default, which every later `apply` and `status` reads:

```powershell
conenv apply --select --save
conenv apply --only git --yes --save
conenv status
conenv config --unset-selection
```

The saved selection lives in `~/.conenv/selection.yaml`, and `conenv config` reports the path and the scope source. `conenv apply --select --write selection.yaml` exports the same choice to any file, which `--selection selection.yaml` replays:

```yaml
schemaVersion: conenv/selection/v1
profile: personal
components:
  git:
    resources: true
  ai-agents:
    agents:
      codex:
        resources: true
        skills:
          - release-notes
```

The file is an explicit allowlist: omitted components and newly added resources are not selected. At component level, `resources` covers packages, files, and checks, while `agents` selects individual agent declarations; inside an agent, `resources` covers its configuration and MCP servers and `skills` selects individual skills. Unknown profiles, components, agents, skills, fields, and duplicate skills are errors. For short-lived automation, repeat `--only` with `COMPONENT`, `COMPONENT/resources`, `COMPONENT/agents/AGENT`, `COMPONENT/agents/AGENT/resources`, or `COMPONENT/agents/AGENT/skills/SKILL`. `--select`, `--selection`, and `--only` are mutually exclusive. Selection is resolved before runtime prerequisites are checked, so a missing source or secret used only by an omitted resource does not block the command.

Before replacing a destination, conenv stores its current contents under:

```text
~/.conenv/backups/<run-id>
```

The active profile, managed paths, ownership, desired digests, and run history are recorded in `~/.conenv/state.json`. Secret values and rendered contents are not stored there.

Use `conenv apply --undo --list` to discover recorded runs and whether their backups still exist, and `conenv apply --undo RUN_ID --dry-run` to inspect one before undoing it. Both accept `--json`.

If a filesystem action fails, changes from that run are rolled back in reverse order. Undo a completed run with:

```powershell
conenv apply --undo
conenv apply --undo RUN_ID
```

Undo replaces the managed destinations and rolls the recorded state metadata back, but it does not uninstall packages. Direct edits to managed destinations are treated as drift; edit the repository source instead. Conenv does not perform two-way synchronization.

Add `--dry-run` to preview the exact reverse-order file operations without executing them, and `--json` for automation. New apply records include the previous managed-state snapshot, allowing undo to reinstate state metadata as well as files. Successfully installed packages are listed as retained. Non-interactive undo requires `--yes`; a dry run never does.

## Command behavior

- `config` reports or sets the configuration repository and its scope, and `config --check` validates schema fields, inheritance, sources, secrets, skills, MCP declarations, checks, and destination ownership.
- `config --profiles`, `--components`, `--profile-detail`, and `--component-detail` expose manifest inventory without requiring runtime prerequisites.
- `apply` installs packages, backs up conflicts, and commits filesystem changes.
- `apply --dry-run` emits the deterministic actions required for the selected scope.
- `status` reports each selected component as `in_sync`, `drifted`, or `error`. Drift exits with status `3`.
- `status --check` runs desired-state and custom health checks.
- `apply --undo` previews or reverses the filesystem and recorded managed state of a run.

`--json` output never includes rendered file contents or resolved secret values.
When JSON is requested, stdout contains only the result document; diagnostics and prompts use stderr. In non-interactive environments, `apply` and a mutating `apply --undo` fail immediately unless `--yes` is supplied.
