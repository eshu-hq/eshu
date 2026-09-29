# Shared-Projection Intent Upsert Moves created_at By Design (#7323)

Issue #7323 asked whether the shared-projection intent upsert can stall
projection by overwriting `created_at` on a row that is already completed. An
arbiter ruled that the behavior is intended and converges. This change makes no
runtime change: it documents the contract in `upsertSharedIntentBatchSuffix` and
pins it with tests.

## Root-Cause Evidence

Root-Cause Evidence: the `ON CONFLICT (intent_id) DO UPDATE` clause in
`upsertSharedIntentBatchSuffix` (`go/internal/storage/postgres`) overwrites every
column, including `payload` and `created_at`, and keeps `completed_at` advance-only
through `COALESCE(shared_projection_intents.completed_at, EXCLUDED.completed_at)`.
The trace, by symbol:

- `sharedintent.Build` derives `intent_id` through `sharedintent.StableIntentID`
  from acceptance unit, generation, partition key, domain, repository, scope, and
  source run. `created_at` is not part of the identity, so a re-emitted intent
  conflicts on the same row and moves its `created_at`.
- `repoDependencyRunsOnFenceRequests` (reducer) hashes each active RUNS_ON row's
  `intent_id` and `created_at`. `created_at` is therefore the acceptance epoch of
  the RUNS_ON workload readiness fence
  (`TestRepoDependencyRunsOnFenceIsOrderIndependentAndInputSensitive` already
  asserts that acceptance-epoch fences differ).
- Unit selection is pending-only: `listPendingDomainIntentsSQL` filters
  `completed_at IS NULL`, and `RepoDependencyProjectionRunner` selects units from
  that read. Moving `created_at` on a completed row with no pending sibling
  changes nothing. With a pending sibling, `processAcceptanceUnit` loads the
  whole unit (`ListAcceptanceUnitDomainIntents`), `ensureRunsOnWorkloadReadiness`
  finds the new token unpublished, `replayWorkloadMaterializationForFence`
  requests one fenced replay (`ReplayWorkloadMaterializationForFence`), the
  workload handler echoes the token, and the next cycle finds it ready and
  rewrites the active rows idempotently. It converges and does not stall.
- Rejected alternatives: freezing `created_at` for completed rows would let a
  token published before the re-upsert satisfy the fence (weakening the
  WorkloadInstance prerequisite for RUNS_ON) and would corrupt pending ordering
  after a rebuild reset reopens rows; reopening on upsert would force a
  repo-wide retract and rewrite per retry, and PR #7304 chose that intents are
  not reopened. Reopening stays the explicit act of
  `storage/postgres/rebuild/reset`.

Pinning tests:

- `TestSharedIntentUpsertCreatedAtLastWriterWinsLive` (Postgres, ledgered in
  `specs/live-tests.v1.yaml` and enrolled in the reducer contention gate's `-run`
  filter, where an unset DSN is a failure) proves a re-upsert overwrites
  `created_at` and `payload` on a completed row while `completed_at` keeps its
  original value, that a pending row takes the supplied `completed_at`, and that a
  retry without `completed_at` never reopens a completed row.
- `TestRepoDependencyProjectionRunnerReplaysFencedWhenCompletedRunsOnCreatedAtMoves`
  (hermetic) proves a completed RUNS_ON row at a newer `created_at` plus a pending
  sibling yields a new fence token, exactly one fenced replay, and that the active
  rows are written and completed once the new token is ready.

## No-Observability-Change / No-Regression Evidence

No-Observability-Change: no runtime code, SQL, metric, span, log key, or status
field changed. The edit is a Go doc comment, two tests, and their CI enrollment, so the existing
readiness-blocked and replay signals in `RepoDependencyProjectionRunner` remain
the operator view of this path.

No-Regression Evidence: the SQL text of `upsertSharedIntentBatchSuffix` is
byte-identical to `origin/main`, so the statement plan, locking, and cost are
unchanged. No hot-path, queue, lease, or claim behavior is touched.
