<!-- vibe-bootstrap:git:start -->
## Git Delivery

Autonomous delivery applies to implementation, fixes, and configuration changes in this project. Questions, analysis, and read-only tasks do not trigger this workflow.

- The agent owns delivery from implementation through merging on GitHub, without additional reminders to commit, push, or create a PR.
- Create a short-lived task branch from the latest default branch. Reuse its branch and PR when continuing the same task. The default branch is `main` and the remote is `origin`.
- Create atomic commits by logical work unit using Conventional Commits. Do not commit by conversation turn or leave completed implementation uncommitted indefinitely.
- Inspect staged changes and complete the project's required validation before committing. At task delivery, push the branch and create or update a PR describing behavior changes and validation results.
- Once required checks pass and repository policy permits it, autonomously merge the PR and clean up merged task branches. Preserve logical commits using a permitted merge method. If no checks are configured, state that explicitly instead of claiming CI passed.
- Respect branch protection and review requirements without bypassing gates. Report missing permissions, pending external reviews, and failing checks accurately; pushed does not mean merged. Fix failures caused by the task and continue delivery. Stop the affected remote operation when resolving a blocker requires the user or an external system.
- Preserve pre-existing changes unrelated to the task; do not overwrite, discard, or commit them without authorization. Do not force-push, rewrite shared history, or delete remote repositories by default.
- Merging does not include publishing releases, creating release tags, or deploying. Perform these actions under separate authorization.
- Report commit, PR, and merge status at delivery, with reasons for any unfinished steps.
<!-- vibe-bootstrap:git:end -->
