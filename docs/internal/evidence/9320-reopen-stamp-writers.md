# #9320 Reopen Stamp On Every Terminal-To-Outstanding Writer

#9292 made every fact work item backlog-age reader use
`COALESCE(reopened_at, created_at)`, but only the reducer reopens from
`succeeded` (and the own-gate activation wake) wrote `reopened_at`. A
dead-letter replay, a poison sweep, or a projector re-drive moved a finished
row back to `pending` with `reopened_at` untouched, so the row kept reporting
its age since first creation. Shared projection intents had no reopen marker at
all.

## Writers

The source guard `TestEveryTerminalReopenWriterStampsReopenedAt` scans every
non-test Go string literal in the module. Before the fix it flagged exactly
these ten writers, the same list found by reading the code:

| Writer | Statement | Stamp |
| --- | --- | --- |
| recovery replay (runtime `/admin/replay`) | `replayFailedWorkItemsTemplate` | `reopened_at = $1` (rows are `dead_letter`/`failed` by WHERE) |
| dead-letter drain (bounded) | `replayFailedWorkItemsBoundedTemplate` | `reopened_at = $1` |
| admin API replay | `query/admin/store` `ReplayFailedWorkItems` | `reopened_at = $1` |
| poison sweep | `poison.RecoverDeadLettersQuery` | `reopened_at = $1` (rows are `dead_letter`) |
| reconcile re-projection (3 composers) | `reprojectReopenedCTE` | `queuestore.ReopenedAtUpsertValue` |
| generation-liveness re-drive | `recoverWedgedActiveGenerationsQuery` | `queuestore.ReopenedAtUpsertValue` |
| zombie heal | `healProjectorActiveGenerationQuery` | `queuestore.ReopenedAtUpsertValue` |
| refinalize re-enqueue | `reset.refinalizeScopeProjectionsQuery` | `queuestore.ReopenedAtUpsertValue` |
| admin repo_dependency intent reopen | `reopenIntentsQuery` | `reopened_at = $2` (store clock) |
| rebuild reset intent reopen | `reset.reopenSharedIntentsTemplate` | `reopened_at = now()` |

`ReopenedAtUpsertValue` stamps `EXCLUDED.updated_at` only when the conflicting
row was not `pending`, `retrying`, `claimed`, or `running`. The upserts also
re-drive an expired `claimed` or `running` row; that row was already
outstanding and keeps its earlier `reopened_at`, so a stall is not hidden.
Already stamping before #9320: `ReopenSucceededReducerSetClause` composers, the
activation wake, the cross-scope replay CASE, the completion fanout CASE, the
service-catalog retrigger CASE, and the 093/096 trigger functions.

Out of scope: `workflow_work_items` claim release and retry (a different table
with no reopen marker; a claim release is not a terminal transition) and the
collector generation dead-letter replay (a different table).

## Schema

Migration 244 adds `shared_projection_intents.reopened_at TIMESTAMPTZ NULL`
(catalog-only, one statement), and migration 245 adds the pending-age index.
Check on 2026-10-29 at the rebase onto `origin/main` 7ef08c6566: main holds
238, 239 and 243; open PRs #9474 and #9458 each add 240 and 241. This branch
first took 239 and 240, then 241 and 242, and now takes 244 and 245, past the
highest number on main or in flight. Gaps in the sequence are tolerated; a
shared sequence number fails
`TestBootstrapDefinitionsHaveUniqueSequenceNumbers`.

## Proof

- RED then GREEN, live Postgres 18.6:
  `TestTerminalReopenWritersStampReopenedAtLive` (7 writer subtests, all RED
  before the fix with `reopened_at` NULL or the earlier value),
  `TestAdminReopenWritersStampReopenedAtLive` (admin replay RED before the fix),
  `TestDeadLetterReplayGetsBoundedFreshReadinessGraceLive` (RED with the old
  replay template: `CycleStartedAt` kept the original enqueue).
- Terminal-only: each upsert writer subtest also re-drives an expired claimed or
  running row and asserts its earlier `reopened_at` is kept; the refinalize
  subtest does the same for a live claim.
- Readiness grace: after a dead-letter replay the first claim anchors
  `CycleStartedAt` on the replay and the AWS drift handler defers while its
  state scope is pending (fresh window). The deferral's retry does not restamp,
  so once the 30-minute bound passes since the replay the next claim keeps the
  replay anchor and the handler commits (not starved).
- Shared intents (`TestSharedIntentReopenAgeThroughStatusLive`,
  `TestSharedIntentReopenRacesLive`): a five-day-old intent reopened by the
  admin reopen or by the rebuild reset reports its status age from the reopen,
  and a fresh pending intent keeps its creation age. A fresh insert leaves
  `reopened_at` NULL and the upsert never clears `completed_at`, so the
  COALESCE is well defined. Two reopeners reopen an intent once (both guard on
  `completed_at IS NOT NULL`; the admin reopen locks `FOR UPDATE SKIP LOCKED`),
  and a drain blocked on a reopen completes the intent afterwards.
- Concurrency (`TestReopenStampConcurrentWritersLive`, `-race`): two bounded
  replayers over 40 dead-letter rows split them with no row replayed twice and
  each row stamped by its replayer; a replay blocked on a poison sweep's row
  lock rechecks, skips the now-pending row, and leaves the sweep's stamp; a
  re-projection blocked on a claimer skips the live claim and keeps its reopen
  evidence, and one blocked on an Ack stamps from the committed `succeeded`
  status; a rolled-back replay leaves no stamp, the retry stamps once, and a
  repeat replay is a no-op. Interleavings are made deterministic by waiting on
  `pg_blocking_pids` of the holder.

- `TestActivationWakeInterleaveLive` (#7597) told the wake from the admin
  replay by `reopened_at` being set, which only the wake did. Both stamp now,
  so a test-only trigger in its isolated schema records every move into
  `pending` (and every rewrite of a pending row's stamp) with the writer's
  backend pid, and the oracle attributes each reopen by pid. That exposed a
  third writer the stamp had hidden: the final Fail's own
  `wakeAfterProducerTerminalDeadLetter`, which the old oracle counted as the
  wake and the new one counts as a wake site. In the race the admin replay
  never runs after a committed wake (it skips a row the wake holds, `FOR
  UPDATE SKIP LOCKED`), so `TestAdminReplayAfterCommittedWakeIsANoOpLive`
  forces that order. Widening the replay's three status guards to admit
  `pending` turns it red (`replayed 1 rows; reopens by wake=1 admin=1`).

## Performance

No-Regression Evidence: private PostgreSQL 18.6, 402,000 `fact_work_items`
(400,000 reducer rows over 40 domains and 2,000 scopes, 5% `dead_letter`, 1%
`failed`; 2,000 projector rows), `ANALYZE`d. Each statement ran under
`EXPLAIN (ANALYZE, BUFFERS)` in `BEGIN`/`ROLLBACK`, six reps, alternating
which variant ran first. The plan node shape (operators, indexes, join order)
is identical with and without the `reopened_at` assignment for all four; only
the UPDATE row width grows by 8 bytes. `reopened_at` has no index, and these
UPDATEs already change indexed columns (`status`, `visible_at`, `updated_at`),
so the extra column adds no index maintenance.

| Statement | Median before | Median after |
| --- | --- | --- |
| unbounded reducer replay, 50 scopes, poison class excluded | 11.13 ms | 11.06 ms |
| bounded drain, `Limit` 100, poison class excluded | 33.63 ms | 34.52 ms |
| unbounded projector replay | 3.48 ms | 3.53 ms |
| poison sweep, batch 100 | 2.25 ms | 2.21 ms |

The re-drive upserts touch one row by primary key; the CASE reads the locked
row already in hand.

Shared-intent age reader: `shared_projection_pending` now reads
`MIN(COALESCE(reopened_at, created_at))`. `shared_projection_intents_pending_idx`
does not hold `reopened_at`, so without help the aggregate read the heap of
every block holding a pending intent. Migration 245 adds
`shared_projection_intents_domain_pending_age_idx` on `(projection_domain,
created_at) INCLUDE (reopened_at) WHERE completed_at IS NULL` (arbiter ruling,
option B). On 500,000 intents with 20,000 pending over 12 domains:

| Aggregate | Plan | Time |
| --- | --- | --- |
| `MIN(created_at)`, before #9320 | Index Only Scan, pending_idx, 207 buffers | 1.46-1.55 ms |
| `MIN(COALESCE(...))`, no index | Bitmap Heap Scan, 8,441 heap blocks | 11.9-12.9 ms warm |
| `MIN(COALESCE(...))`, migration 245 | Index Only Scan, 101 buffers, 808 kB index | 1.27-1.86 ms |

`TestSharedIntentPendingAgeIndexOnlyScanLive` runs the shipped CTE text and
asserts the Index Only Scan with `Heap Fetches: 0`; it fails without 245.
Write path (ruling proof 2): twin 500,000-row tables with every production
index, one also carrying the 245 index; each statement 1,000 rows, under
`EXPLAIN (ANALYZE, BUFFERS, WAL)` in `BEGIN`/`ROLLBACK`, 5 interleaved reps,
alternating first mover. The host was loaded (load average 18-28), so the WAL
columns are the exact comparison and the times are indicative.

| Write | WAL without 245 | WAL with 245 | Median time without / with |
| --- | --- | --- | --- |
| (a) new pending upsert | 1,798,897 B, 9,118 rec | 1,933,033 B, 10,130 rec | 45.5 / 52.0 ms |
| (b) completed-row re-upsert | 750,958 B, 8,048 rec | 750,958 B, 8,048 rec | 44.4 / 45.8 ms |
| (c) `markIntentsCompletedSQL` | 722,876 B, 7,162 rec | 722,876 B, 7,162 rec | 43.1 / 41.3 ms |
| (d) `reopenIntentsQuery` shape | 936,475 B, 10,243 rec | 1,073,282 B, 11,320 rec | 53.7 / 58.0 ms |

(b) and (c) write byte-identical WAL: a completed row is outside the partial
index, so neither path maintains it. (a) and (d) pay one index entry per row
that becomes pending (about 134-137 B and one WAL record each).

Readers (ruling proof 3), after `VACUUM ANALYZE`, 5 interleaved reps, custom
and forced generic plans:

| Reader | Plan with 245 | Median without / with |
| --- | --- | --- |
| liveness `progressed_domains` InitPlan | unchanged (Index Only Scan, pending_idx) | 0.216 / 0.214 ms |
| `listPendingDomainPartitionIntentsSQL` | unchanged | generic 0.80 / 0.96 ms, custom 3.92 / 3.68 ms |
| `listPendingDomainUnhashedIntentsSQL` | unchanged | 0.023 / 0.025 ms |
| `listPendingAcceptanceUnitIntentsSQL` | unchanged | 0.23 / 0.28 ms |
| `listPendingDomainIntentsSQL`, LIMIT 200 | new index + Incremental Sort | custom 3.66 / 0.40 ms, generic 3.07 / 0.41 ms |
| `listPendingDomainIntentsSQL`, whole domain (1,667 rows) | custom unchanged, generic Incremental Sort | custom 2.96 / 2.55 ms, generic 3.63 / 3.43 ms |
| `listPendingDomainIntentsAfterSQL`, LIMIT 200 | new index + Incremental Sort | custom 3.90 / 0.51 ms, generic 4.10 / 0.55 ms |
| status `shared_projection_pending` | Index Only Scan, Heap Fetches 0 | 15.7 / 1.88 ms |

The two plan changes serve the `created_at`-ordered domain reads, which the
new index presorts, and both got faster. The index adds no unique constraint,
no conflict key, and no lock beyond the `CONCURRENTLY` build's
`ShareUpdateExclusiveLock`, which does not block reads or writes.

## Observability

Observability Evidence: no new instrument. Each writer already has an operator
signal that names it, and the stamp does not change how many rows any writer
reopens: poison sweep `eshu_dp_poison_liveness_recovered_total` and its INFO
log; liveness `eshu_dp_generation_liveness_recovered_total`; zombie heal
`eshu_dp_projector_zombie_heal_total{outcome="healed"}`; refinalize the
recovery response counts and `eshu_dp_recovery_delta_active_scopes_total`;
reconcile re-projection the reconcile skip counter and logs; admin replay the
durable replay events; runtime replay and drain the response body and request
span. Per row, the upsert writers' payload counters
(`reconcile_reopen_count`, `liveness_recovery_attempts`, `zombie_heal_count`,
`poison_recovery_attempts`) name the writer. The age signals
(`eshu_dp_queue_oldest_age_seconds`,
`eshu_runtime_queue_oldest_outstanding_age_seconds`, status `oldest_age`) now
restart at each reopen.
