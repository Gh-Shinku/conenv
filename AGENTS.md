## Git Delivery

For implementation, fixes, or configuration changes, complete Git delivery as part of the task.

- Work and commit on `main`; synchronize with `origin/main` before editing.
- Make atomic commits by logical work unit and follow the commit-message rules in [`CONTRIBUTING.md`](CONTRIBUTING.md).
- Inspect staged changes and complete the project's required validation before committing.
- Preserve unrelated changes and limit commits to the authorized scope.
- Respect branch protection and review requirements. Fix failures caused by the task; report blockers that require user action or external changes.
- Obtain explicit authorization for force-pushes, shared-history rewrites, repository deletion, releases, tags, and deployment.
- At delivery, push task commits to `origin/main`, satisfy required checks, and report commit and push status or any blocker.

## Compatibility

Develop against the current manifest, CLI, and on-disk formats. Breaking changes replace the previous interface.

- Replace changed fields, commands, flags, and formats throughout the implementation, using one current form.
- Document the current behavior.
- Update the documentation, examples, and embedded JSON Schema together with interface changes.
- Report unknown commands, flags, harnesses, and manifest fields as input errors.
