# dev-standards: implementation delegation

This file is managed by dev-standards. No personal instructions or skills are required.
Also read this repository's `AGENTS.md` and `.standards/rules.md` when present.

## Applicable requests

For feature implementation, bug fixes, or refactoring, delegate implementation to Codex
through `agent-harness` even when the user does not name it. Claude prepares requirements
and acceptance criteria. Do not apply this workflow to questions, investigation, or review alone.
Adoption, updates, and maintenance of shared rules and settings managed by dev-standards are
also excluded, even when requested directly by the user. Follow the existing shared maintenance
workflow without waiting for additional approval.
Follow an explicit user request for Claude to implement directly or to avoid the harness.
When launched as a harness reviewer or autonomous maintenance worker, perform only that role.
Never launch another harness recursively.

## Procedure

1. Prepare verifiable acceptance criteria and any necessary acceptance tests.
2. Show them to the user and obtain approval. An implementation request alone is not approval
   of acceptance criteria. Explain that the harness runs implementation and review and can
   automatically merge a PR when its conditions are met.
3. Check that `agent-harness` is on PATH, or use an executable location explicitly supplied
   by the user. Confirm the target GitHub repository and base branch, then run
   `agent-harness doctor --repo <owner>/<name>`. The current harness requires macOS and
   Codex, Claude, GitHub, and 1Password CLIs with authentication and permissions already set up.
   Report missing prerequisites or doctor failures and stop.
4. The harness creates a separate working clone from the remote. It does not automatically
   carry over uncommitted changes in the current worktree. For work continuing existing changes,
   establish the remote base branch and how those changes will be included before running.
   Preserve existing changes.
5. Save the task and approved criteria to files and run this command with actual values.
   Add `--acceptance-patch <patch-file>` if acceptance tests were prepared as a patch.

   ```sh
   agent-harness run --repo <owner>/<name> --base <base-branch> \
     --title '<change title>' --kind <feature|bugfix|refactor> \
     --task-file <task-file> --criteria-file <criteria-file> --approved-by-human
   ```

6. Check the exit code and execution status. Accurately report implementation, review,
   and merge progress.

Use `--approved-by-human` only after acceptance criteria were approved in this conversation.
Use API key billing only at the user's explicit request. Report failures, usage-limit waits,
or requests for human judgment and stop. Do not silently implement directly with Claude,
run `clear-stop`, or merge with `gh pr merge` directly.
