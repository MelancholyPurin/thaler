# Git and pull requests

- **The maintainer does all the git work.** Never commit, push, add a remote, open a PR, or create GitHub milestones or issues unless asked. Leave changes in the working tree. When a GitHub step would help (milestones, labels), write the `gh` commands for them to read and run instead of running them.
- **They name the branches**, one per feature or maintenance task: `<initials>/<ID>/<path>/<slug>`, for example `ah/SAF-9/internal/core/reminder-backup-age` or `ah/maintenance/add-claude`. Work on the branch you are on, and say so if it is `main`.
- **Pull request and squash-commit titles** are `[ID] domain/service: Name`, for example `[SAF-6] internal/core: Back up automatically before migration`. Maintenance work uses `[MAINTENANCE]`. The release notes are written from these titles (`make release-notes`), so the ID must be a real story ID or `MAINTENANCE`: anything else lands under "Other changes". At the end of a task, suggest the title; do not apply it.
- If you are ever asked to commit or open a PR, end the commit message and the PR description with the attribution lines the session gives you.
