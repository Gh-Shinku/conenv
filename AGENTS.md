<!-- vibe-bootstrap:git:start -->
## Git Delivery

Autonomous delivery applies to implementation, fixes, and configuration changes in this project. Questions, analysis, and read-only tasks do not trigger this workflow.

- The agent owns delivery from implementation through committing and pushing to GitHub, without additional reminders.
- Work directly on the `main` branch and push commits to the `origin` remote. Do not create task branches or pull requests for this repository.
- Create atomic commits by logical work unit and follow the commit-message rules in [`CONTRIBUTING.md`](CONTRIBUTING.md). Do not commit by conversation turn or leave completed implementation uncommitted indefinitely.
- Before committing, synchronize with `origin/main`, inspect staged changes, and complete the project's required validation. Push the resulting `main` commit once validation passes.
- Respect branch protection and other repository requirements without bypassing gates. Report missing permissions and failing checks accurately. Fix failures caused by the task and continue delivery. Stop the affected remote operation when resolving a blocker requires the user or an external system.
- Preserve pre-existing changes unrelated to the task; do not overwrite, discard, or commit them without authorization. Do not force-push, rewrite shared history, or delete remote repositories by default.
- Merging does not include publishing releases, creating release tags, or deploying. Perform these actions under separate authorization.
- Report commit and push status at delivery, with reasons for any unfinished steps.
<!-- vibe-bootstrap:git:end -->

## Compatibility

This project is early and intentionally fluid: any implementation may change in a breaking way. Do not add compatibility layers for the sake of existing users or existing repositories.

- Do not keep deprecated aliases, dual formats, or version fallbacks. When a manifest field, command, flag, or on-disk format changes, replace it outright.
- Do not write migration code paths. `conenv config --check` reports what no longer parses, and the repository owner updates the configuration repository.
- Update the documentation, examples, and the embedded JSON Schema in the same change that breaks something, and list the replacement in the README's removed-commands table.
- Validate input strictly: an unknown command, flag, harness, or manifest field is an error, never a silent fallback.
