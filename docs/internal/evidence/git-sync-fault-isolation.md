# Git Sync Fault Isolation Evidence

Issue #7001: on ops-qa (2026-09-23 11:42Z) one repository's
`git ls-remote` hit a 300 s DNS timeout. `syncGitRepositoriesWithLogger`
returned that error for the whole cycle, the composite runner logged
`composite_runner_fatal`, and the ingester exited, canceling in-flight graph
writes on unrelated scopes. The change isolates a per-repository `list_refs`
failure the same way clone and fetch failures were already isolated: log it,
count it, skip that repository for this cycle, keep syncing the rest. Parent
context cancellation (shutdown) still returns as fatal.

No-Regression Evidence: `TestSyncGitRepositoriesIsolatesOneListRefsFailure`
copied onto `origin/main` fails with
`list remote git refs for ...: Resolving timed out after 300018 milliseconds, want nil`,
the ops-qa incident error, and passes on this branch.
`TestSyncGitRepositoriesPropagatesCancellationDuringListRefs` cancels while a
repository's `ls-remote` is in flight; deleting the `ctx.Err()` check in
`resolveRepoRefsIsolated` makes it fail.
`TestSyncGitRepositoriesSkippedRepoRetriesWithUnlostDeltaNextCycle` shows the
skipped repository is selected again on the next scheduled poll with its delta
intact, because selection compares the remote SHA with the durable Postgres
baseline. In webhook-only mode the repository waits for its next webhook
trigger instead, the same as an isolated clone or fetch failure.
`go test ./internal/collector/repo/git/... ./cmd/ingester/... ./internal/telemetry/... -count=1 -race`
passes. The success path runs the same `remoteGitRefs` call as before; the
only added work is one counter increment on a failure, so sync throughput is
unchanged.

Observability Evidence: new counter `eshu_dp_git_repo_sync_failures_total`
with a bounded `operation` label (`clone`, `fetch`, `list_refs`), not
incremented for a shutdown cancellation. Repository identity stays in the
existing `git repository sync failed` log line (`failure_class=git_sync_failure`,
`repository_id`, `operation`). Rows are in
`docs/public/observability/telemetry-coverage.md`,
`docs/public/reference/telemetry/metrics.md` and
`docs/public/reference/telemetry/metrics-ingestion-collectors.md`.
