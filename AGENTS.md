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
