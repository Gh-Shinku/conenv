# Configuration guide

The `conenv.yaml` committed to a dedicated configuration repository is the source of truth for an environment. The CLI repository contains only the program, documentation, examples, and embedded schema.

Export the schema into a configuration repository for editor completion:

```powershell
conenv schema --write C:\path\to\conenv-config\.conenv\conenv.v1.schema.json
```

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
3. The default saved by `conenv use REPOSITORY` in `~/.conenv/config.json`.
4. The current working directory.

Use `conenv config` to inspect the resolved path and its source. The older `--root` flag remains a compatibility alias for `--repo`.

## Complete example

```yaml
schemaVersion: conenv/v1
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
    components: [codex]
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

  codex:
    codex:
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
  codex-skills:
    category: ai
    description: Shared Codex skills
```

`category` accepts `program` or `ai`. If omitted, file/package components default to `program` and Codex-only components default to `ai`.

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

A rendered file or generated Codex configuration may still contain a resolved value. Keep those destinations sensitive and outside version control.

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

`conenv doctor` combines custom checks with desired-state checks for packages, links, rendered files, Codex configuration, and skills.

## Codex

Conenv merges declared settings into `~/.codex/config.toml`:

```yaml
codex:
  config:
    approval_policy: on-request
    sandbox_mode: workspace-write
```

Existing unmanaged keys and MCP servers are retained, though TOML comments and formatting may be normalized. The generated config is always handled as sensitive.

Skills are linked to `~/.codex/skills/<name>` and each source directory must contain `SKILL.md`:

```yaml
codex:
  skills:
    - name: release-notes
      source: ai/skills/release-notes
```

### MCP servers

An HTTP server requires a URL:

```yaml
codex:
  mcpServers:
    documentation:
      type: http
      url: https://example.com/mcp
```

A stdio server requires a command and may include arguments and environment values:

```yaml
codex:
  mcpServers:
    localTools:
      type: stdio
      command: npx
      args: [-y, example-mcp]
      env:
        TOKEN: '{{ secret "api_token" }}'
```

See the official [Codex configuration reference](https://developers.openai.com/codex/config-reference) and [MCP guide](https://developers.openai.com/learn/docs-mcp).

## Planning, drift, and recovery

`conenv plan` reports changes without mutating the machine. `conenv status` reports the same drift and exits with status `2` when actions are pending. Applying the same manifest twice is a no-op.

Add `--select` to `plan`, `apply`, `status`, or `doctor` to open an interactive checklist containing the profile's components supported by the current platform. Toggle individual numbers or ranges, press Enter to confirm, and use `a`, `n`, or `q` for all, none, or cancel. The selection affects only that command and does not modify the profile.

Before replacing a destination, conenv stores its current contents under:

```text
~/.conenv/backups/<run-id>
```

The active profile, managed paths, ownership, desired digests, and run history are recorded in `~/.conenv/state.json`. Secret values and rendered contents are not stored there.

If a filesystem action fails, changes from that run are rolled back in reverse order. Restore a completed run explicitly with:

```powershell
conenv restore RUN_ID
```

Restore replaces the managed destinations but does not uninstall packages. Direct edits to managed destinations are treated as drift; edit the repository source instead. Conenv does not perform two-way synchronization.

## Command behavior

- `validate` checks schema fields, inheritance, sources, secrets, skills, MCP declarations, checks, and destination ownership.
- `plan` emits the deterministic actions required for the selected profile.
- `apply` installs packages, backs up conflicts, and commits filesystem changes.
- `status` detects drift and supports machine-readable JSON.
- `doctor` runs desired-state and custom health checks.
- `restore` restores the pre-apply filesystem state for a run.
- `use` records the default configuration repository for future commands.
- `config` shows the selected repository and selection source.
- `schema` prints or writes the JSON Schema embedded in the CLI binary.

`--json` output never includes rendered file contents or resolved secret values.
