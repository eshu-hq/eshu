# Git Command Deadline Evidence

Issue #8052: the git collector's sync loop runs one repository at a time and
gave each git command no deadline of its own. On the QA environment on
2026-10-12 and 13, a fetch that waited on name resolution ran until libcurl
gave up (`Resolving timed out after 300040 milliseconds`), and the whole fleet
sync waited with it: 14 stalls of 300 s, 70 minutes of paused sync in 10 h
54 min. The change runs every network-capable git command under a deadline
derived from the loop context and kills the whole process group when it
expires: `ESHU_GIT_RESOLVE_TIMEOUT_SECONDS` (default 60) for `ls-remote` ref
listing and the default-branch lookup, `ESHU_GIT_FETCH_TIMEOUT_SECONDS`
(default 90) for fetch, `ESHU_GIT_CLONE_TIMEOUT_SECONDS` (default 600) for the
initial clone. Purely local commands have no deadline. Fetch and clone also pass
`http.lowSpeedLimit=1` and `http.lowSpeedTime=60`. The failure takes the
existing isolated path and the repository is retried next cycle.

## Default values: QA elapsed distribution

Source: the QA ingester log stream copy from 2026-10-12 about 19:45Z to
2026-10-13 04:48Z, read by the change owner's orchestrator over every
`git repository sync completed` line. 107,248 completed lines, all
`operation=fetch`; the window holds no clone events because the QA checkouts
already exist.

| `elapsed_seconds` of a completed fetch | seconds |
|---|---|
| min | 0.128 |
| p50 | 0.242 |
| p90 | 0.296 |
| p99 | 0.384 |
| p99.9 | 0.819 |
| p99.99 | 1.430 |
| max | 10.118 |

One fetch took more than 10 s, none took more than 30 s. The slowest eight were
1.51, 1.53, 1.70, 1.99, 2.02, 2.40, 4.60 and 10.12 s. In the same window 11
fetches failed: two after 7.0 s (HTTP/2 framing errors) and nine after 300.0 to
300.1 s (resolver stalls). The 90 s fetch default is about 9 times the slowest
successful fetch and about 350 times the p99. Limits of this reading: QA
steady state only; there is no clone sample, so 600 s is a judgment, not a
measurement; `ls-remote` ref listing and default-branch lookup are not logged
as separate completed lines, so the 60 s resolve default is not measured at all
(NOT_CHECKED); a first fetch after a long gap or a very large delta on another
environment could exceed 90 s, which is why all three values are settable.

## Proof

No-Regression Evidence: baseline is `origin/main` c223cd432e. The cells run the
real `git` binary against a local TCP listener on 127.0.0.1 that accepts a
connection and never writes (a stall). Before (2026-10-13 11:23Z, go 1.26.9,
base code plus only the config field and the `deadline` package the test
imports): `TestRealGitStalledCommandsHitTheirOwnDeadline` for `fetch`,
`ls_remote`, `resolve` and `clone`, `TestRealGitLoopMovesPastStalledFetch`, and
`TestRealGitLowSpeedAbortsAStalledTransfer` fail with, for example, `fetch did
not fail within 5s of a 2s deadline; the stalled real git was not bounded` and
`the sync cycle did not finish; one stalled real fetch paused the loop`. After:
each operation fails at 2.04 to 2.13 s against its own 2 s deadline (the other
two deadlines are an hour, so only that operation's value can fire). The error
reads `exceeded the 2s deadline after 2.x s`; the counter holds exactly that
operation once; while the command is in flight `pgrep -f` finds the
`git-remote-http` helper, and after the deadline it finds nothing, so the real
child and its helper both died; no `.lock` file remains in the repository.
The two-repository loop with real git fails repository A's fetch at the
deadline, selects repository B synced from a real `file://` remote, logs
`failure_class=deadline_exceeded` with `deadline_operation=fetch`,
`deadline_seconds=2`, `operation=fetch`, `repository_index=1`, and meters one
`fetch` in both `eshu_dp_git_repo_sync_failures_total` and
`eshu_dp_git_command_deadline_exceeded_total`; the whole cycle takes about
2.4 s. A listener that drips a valid advertisement prefix, 3 bytes every
250 ms for 2.5 s with a 1 s low-speed window and a 10 s deadline, is not
killed by either limit (`TestRealGitDripUnderDeadlineIsNotKilled` ends with
git's own invalid-reply error after 2.67 s). With the process deadline at an
hour, `http.lowSpeedTime=2` makes real git abort the stalled connection after
2.15 s with `Operation too slow. Less than 1 bytes/sec transferred the last 2
seconds` (`TestRealGitLowSpeedAbortsAStalledTransfer`). The argv cell records
that fetch and clone carry `-c http.lowSpeedLimit=1 -c http.lowSpeedTime=60`
immediately before the subcommand while `GIT_CONFIG_COUNT`,
`GIT_CONFIG_KEY_0` and `GIT_CONFIG_VALUE_0` still hold the single GitHub-scoped
credential header, and that a local `rev-parse` carries no low-speed options.
Mutations, each restored: removing the deadline fails the real-git stall cells,
the loop cell, `TestOperationSetIsClosed` and `TestRunBoundsAnEscapedDescendant`;
killing only the direct child fails the stall cells and the loop cell (the
surviving helper holds the pipes past the 5 s guard); labelling any error a
deadline fails the drip, low-speed and parent-cancel cells; dropping the
low-speed options fails the argv cell and the low-speed cell; dropping
`WaitDelay` fails `TestRunBoundsAnEscapedDescendant`. A killed command adds
nothing to `SelectedRepoPaths`, and both selection callers build the batch only
from that list, so no generation is built for the repository that cycle; the
Postgres baseline is untouched and the next poll retries it. Not measured: the
added cost on a healthy command is one `context.WithTimeout` timer and a
`WaitDelay` per git spawn (inferred negligible next to a process fork); no
throughput run was made. Two tests in the same package,
`TestGitSourceNextBuildsCollectedGenerationFromSelectionAndPerRepoSnapshots`
and `TestBuildStreamingGenerationDeltaChangedFileFactsMatchFullSnapshot`,
fail on `origin/main` as well on a macOS temp path under `/var`; they are
unrelated.

Observability Evidence: new counter `eshu_dp_git_command_deadline_exceeded_total`
with the closed `operation` set `clone`, `fetch`, `ls_remote`, `resolve` (a
test runs every operation, checks each records its own label, and checks that an
operation outside the set is refused and records nothing), one increment per
killed command, never incremented for a shutdown cancel. The existing
`git repository sync failed` log line carries `failure_class=deadline_exceeded`,
`deadline_operation`, `deadline_seconds`, and its existing `operation`,
`elapsed_seconds`, `repository_id` and `repository_index`. The
`git.deadline_exceeded` span event attaches to the active span when one
exists. The error text names the deadline and the elapsed time, for example
`git fetch exceeded the 90s deadline after 90.0s`. Rows are in
`docs/public/observability/telemetry-coverage.md`,
`docs/public/reference/telemetry/metrics.md`,
`docs/public/reference/telemetry/metrics-ingestion-collectors.md` and
`docs/public/reference/env-registry.md`.
