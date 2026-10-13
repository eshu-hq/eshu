# Git Command Deadline Evidence

Issue #8052: the git collector's sync loop runs one repository at a time and
gave each git command no deadline of its own. On the QA environment on
2026-10-12 and 13, a fetch that waited on name resolution ran until libcurl
gave up (`Resolving timed out after 300040 milliseconds`), and the whole fleet
sync waited with it: 14 stalls of 300 s, 70 minutes of paused sync in 10 h
54 min. The change runs every git command under a deadline derived from the
loop context and kills the whole process group when it expires. The initial
clone gets `ESHU_GIT_CLONE_TIMEOUT_SECONDS` (default 600); every other
command gets `ESHU_GIT_FETCH_TIMEOUT_SECONDS` (default 90). The failure takes
the existing isolated path and the repository is retried next cycle.

No-Regression Evidence: baseline is `origin/main` c223cd432e plus the new tests
and a stub of the config fields (the tests do not compile without the
fields). Input shape: a fake `git` on `PATH` that sleeps 20 s for the stalled
command and answers every other command like the fast isolation fake; test
deadline 750 ms; wall-time guard 10 s. Backend: none, filesystem only.
Before (2026-10-13 09:45Z, go 1.26.9): `TestGitCommandDeadlineKillsStalledFetch`,
`TestSyncLoopMovesPastStalledFetch`, `TestSyncLoopMovesPastStalledClone`,
`TestSyncLoopMovesPastStalledListRefs` and `TestStaleLockRetryFetchIsAlsoBounded`
fail with `did not return within 10s; the stalled git command was not bounded`.
After: the same five pass in 0.76 s to 0.94 s each, which is the 750 ms
deadline plus process start. The two repository cells assert that repository
A is reported failed and repository B is selected, so one stall costs its own
deadline and the loop moves on. `TestSlowCloneWithinCloneDeadlineSucceeds` shows
a clone slower than the fetch deadline but inside the clone deadline still
succeeds (2 s clone, 750 ms fetch deadline), so a large healthy clone is not
turned into a permanent failure. Process group: the fake records the pid of
its shell and of its sleeping child, and the tests assert both are gone after
the deadline. Mutations, each restored (`git status --short` empty): replacing
`context.WithTimeout` with `context.WithCancel` fails the five stall tests;
replacing the process-group kill with a direct-child kill fails six tests
(the surviving child holds the stderr pipe, so `Run` blocks); dropping the
parent-context check fails `TestParentCancelIsNotADeadline`. Not measured: the
added cost on a healthy command is one `context.WithTimeout` timer per git
process spawn (inferred negligible next to a process fork); no throughput run
was made. Two tests in the same package,
`TestGitSourceNextBuildsCollectedGenerationFromSelectionAndPerRepoSnapshots`
and `TestBuildStreamingGenerationDeltaChangedFileFactsMatchFullSnapshot`,
fail on `origin/main` as well on a macOS temp path under `/var`; they are
unrelated.

Observability Evidence: new counter `eshu_dp_git_fetch_deadline_exceeded_total`
with the closed `operation` label `clone`, `fetch`, `ls_remote`, `other`, one
increment per killed command, never incremented for a shutdown cancel
(`TestParentCancelIsNotADeadline`). The `git repository sync failed` log line
gains `deadline_exceeded`, `git_command` and `deadline_seconds` next to its
existing `elapsed_seconds`, `repository_id` and `repository_index`. The
`git.deadline_exceeded` span event attaches to the active span when one
exists. The error text names the deadline and the elapsed time, for example
`git fetch exceeded the 90s deadline after 90.0s`. Rows are in
`docs/public/observability/telemetry-coverage.md`,
`docs/public/reference/telemetry/metrics.md`,
`docs/public/reference/telemetry/metrics-ingestion-collectors.md` and
`docs/public/reference/env-registry.md`.
