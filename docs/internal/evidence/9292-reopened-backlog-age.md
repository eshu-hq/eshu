# Reopened work items age from the reopen, not first creation (#9292)

## Problem

The admin status backlog and queue snapshot computed
`oldest_outstanding_age_seconds` as `MIN(created_at)` over outstanding fact work
items. A reducer item that completes and is later reopened keeps its original
`created_at`; `reopened_at` (migration 088) records the reopen. In the QA
environment on 2026-10-27, each OSV pass reopened about 137
`supply_chain_impact` reducer items that drained in four to five minutes, while
admin status reported an oldest outstanding age of about 115 hours for that
domain. `status.health` also compares `queue.OldestOutstandingAge` against its
stall threshold, so the stale age could read as a stalled queue.

## Definition

Age is the time since the item last became outstanding:
`COALESCE(reopened_at, created_at)`. This is the same expression the reducer
claim path already calls `cycle_started_at`
(`reducer_queue_claim_query.go`, `reducer_queue_batch_query.go`) and the
readiness producers use as their anchor, so there is one "start of this cycle"
across claim, readiness, and status. `visible_at` was rejected: retry backoff
moves it into the future and several paths clear it, so it does not mean
"became outstanding".

Surfaces on the one definition:

| Surface | Source |
| --- | --- |
| Admin status `queue.oldest_outstanding_age`, per-domain `oldest_age` | `domainBacklogCTEs`, `queueSnapshotTail` (`status_queries.go`) |
| `eshu_runtime_queue_oldest_outstanding_age_seconds`, evidence bundle, local progress, `status.health` stall check | read `Report.Queue.OldestOutstandingAge`, fed by the same statement |
| `eshu_dp_queue_oldest_age_seconds`, `eshu_dp_queue_source_oldest_age_seconds` | `queueOldestAgeQuery`, `sourceQueueOldestAgeQuery` (`queue_observer.go`) |
| `eshu_dp_corpus_fence_oldest_deferred_age_seconds` | `corpusFenceGaugesQuery` (`corpus_fence_observer.go`); `TestQueueObserverStoreCorpusFenceCountsRunOnPostgres` seeds a row created 200 hours ago and reopened two hours ago and expects 7200 s (failed with 720000 s before the change) |

The stored status summary row carries the statement's SHA-256 and falls back to
the live read on a mismatch, so rows written by the previous binary are served
live until the periodic writer replaces them.

## Reopen paths

Every reducer-stage writer that moves a `succeeded` row back to `pending`
resets `reopened_at` with the reopen timestamp (the activation wake does the
same for the dead-lettered own-gate consumers it wakes):

| Writer | How |
| --- | --- |
| `ReopenSucceeded`, `ReplayDomain`, admin reopen, completion wake (`reopenDeployableUnitCorrelationsForReposQuery`) | `ReopenSucceededReducerSetClause` (`reducer_queue_replay.go`), one shared SET list |
| cross-scope materialization replay | `scheduleCrossScopeMaterializationReplayQuery`, `reopened_at = CASE WHEN status = 'succeeded' THEN $1` |
| fenced workload replay (ack path) | `scheduleWorkloadMaterializationFencedReplayQuery` (`reducer_queue_ack.go`) |
| service catalog runtime retrigger | `service_catalog_runtime_retrigger.go` |
| activation wake | `relationship_store_resolved.go` |
| cross-scope completion fanout | `scope/completion/fanout.go` |

Not covered, by design of this change (tracked in #9320, which carries the
design constraint: stamp `reopened_at` only on a terminal-to-outstanding
transition, because `reprojectReopenedCTE` also re-drives expired `claimed` and
`running` rows, and stamping those would understate the age of a real stall):

- Projector-stage reopens (`reprojectReopenedCTE`, the liveness re-drive, the
  refinalize reset, zombie heal) and failed or dead-letter replays
  (`projector_queue_sql.go` replay templates, the admin replay, the poison
  sweep, `RecoverDeadLettersQuery`) do not set `reopened_at`. Those rows still
  age from `created_at`, so a replayed dead letter or a re-driven projector
  generation can still show days of age. Setting it there would also hand those
  rows a fresh readiness grace window, a behavior change the reducer claim path
  reads, so it needs its own proof.
- `shared_projection_intents` can be reopened (admin reopen and the refinalize
  reset both clear `completed_at`) but has no reopen marker column, so its age
  still starts at `created_at`. Fixing that needs a schema column.

## Proof

- Regression: `TestStatusOldestOutstandingAgeMeasuresFromReopen` seeds a
  reducer row created 115 hours ago and reopened five minutes ago, a never
  reopened row, a mixed domain, a reopened retrying row, and a finished row. It
  failed before the change with `414000s` (115 h) and passes after, for the
  summary statement, the standalone statements, `QueueOldestAge`, and
  `SourceQueueOldestAge`.
- The pre-#7009 oracle (`status_active_work_oracle_test.go`) took the same two
  edits and was re-pinned; `TestActiveWorkSummaryDiffersFromOracleOnlyInHistoryGroups`
  proves the oracle is still the reverse derivation of the shipped statement,
  and `TestActiveWorkSummaryMatchesPreHistoryGroupsOracle` runs both against
  one snapshot.

## Performance Evidence

No-Regression Evidence: `EXPLAIN (ANALYZE, BUFFERS)` on a private PostgreSQL
18.6 with 801 scopes, 453,201 `fact_work_items` rows (450,000 succeeded, 3,200
outstanding, 428 reopened, plus the standing migration row) and 2,000 pending
shared-projection intents,
before and after, five interleaved runs each with alternating first mover, host
load average 14 to 42 (a shared laptop):

| Statement | Before exec (ms) | After exec (ms) | Shared buffer hits, before and after |
| --- | --- | --- | --- |
| `activeWorkSummaryQuery` | 169.3, 351.5, 195.5, 196.4, 161.4 | 141.9, 154.6, 172.2, 172.2, 169.9 | 10682 and 10682 |
| `queueOldestAgeQuery` | 4.3, 5.0, 4.6, 4.3, 4.7 | 4.4, 4.8, 5.0, 4.7, 5.3 | 1879 and 1879 |
| `sourceQueueOldestAgeQuery` | 11.8, 12.2, 12.6, 11.8, 11.8 | 12.2, 12.2, 11.3, 12.0, 12.3 | 11479 and 11479 |

A separate sequential baseline of the summary statement ran 158.6, 169.4,
159.3, and 158.2 ms. The plans are node-for-node identical (the gate took the
grouped branch; the only differences are row widths, +8 bytes for the extra
`reopened_at` column in the materialized CTE, and its sort memory, 367 kB to
371 kB). The one 351 ms before sample coincided with a load spike on the shared
host. The change adds one nullable column to a CTE that is already materialized
and one `COALESCE` inside an existing aggregate.

## Observability Evidence

No-Observability-Change: `eshu_runtime_queue_oldest_outstanding_age_seconds`,
`eshu_runtime_domain_oldest_age_seconds`, `eshu_dp_queue_oldest_age_seconds`,
and `eshu_dp_queue_source_oldest_age_seconds` keep their names, labels, and
types. Only the value they report changes: it no longer includes time a
completed item spent finished before its reopen. The telemetry and status docs
state the definition.
