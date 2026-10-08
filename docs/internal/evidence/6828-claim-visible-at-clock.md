# #6828 claim visibility runs on the clock that stamped the row

The claim predicate compared `visible_at` against Go's `time.Now()` while the
dirty-reopen trigger stamps `visible_at` from `clock_timestamp()`. Any
DB-ahead skew made a just-reopened pending row invisible to the next claim
("claim after ACK returned no work"). The fix adds one OR arm to the
visibility predicate in the single-claim query and the three batch-claim
filters (base, semantic-next, lock recheck):

`visible_at IS NULL OR visible_at <= $1 OR (status = 'pending' AND visible_at <= clock_timestamp())`

The `$1` arm keeps app-stamped retry rows and simulated clocks working;
`claim_until` stays on `$1` (claim and heartbeat stamp it from the app
clock, so that comparison was already same-clock). No sleeps or retries.

No-Regression Evidence: the added arm is a filter on rows the candidate
fetch already selected; it adds no join, no subquery, and no new index
requirement. `clock_timestamp()` is volatile, so it evaluates per candidate
row, but that is one timestamp read per row against a candidate set the
claim already materializes — no additional table or index access. Baseline:
on main (`reducer_queue_workload_replay_live_test.go` from this branch run
against unmodified `origin/main`),
`TestWorkloadReplayClaimAfterAckToleratesSkewedAppClock` fails with
`reopened single claim = ("", false)` — the issue's exact symptom. After:
the four issue targets
(`TestWorkloadReplayDuringClaimReturnsAckToPending`,
`TestWorkloadFencedReplaySupersedesOnlyOlderInFlightToken`,
`TestWorkloadReplayAndBatchAckContentionConvergesToPending`,
`TestRepoDependencyRunsOnFenceComposesQueuePhaseAndProjectionLive`) plus the
new skew regression pass 3/3 (`-count=3`, exit 0) and once under `-race`
(exit 0, 24.5 s) on local Postgres 18.6 (`postgres:18-alpine`, single
candidate rows per claim). A full-package run on the branch shows the same
33 failures with identical reasons as clean main (verified via `diff` of
the failing sets plus sampled log lines): zero regressions; the
pre-existing failures belong to #7493/#7494 (sibling PRs) and environment
or main-side clusters. Backend/version: Postgres 18.6 only; the changed
statements are Postgres-only (NornicDB never executes these claim queries).

No-Observability-Change: no metric, span, log line, status field, or event
shape changes. Claim latency and row counts keep their existing series; the
fix only changes which already-fetched pending rows pass the visibility
filter under clock skew.
