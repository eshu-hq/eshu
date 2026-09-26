# #7115 projector claim: per-scope claim fence on a claim-only table

## Problem

Root-Cause Evidence: `claimProjectorWorkQuery` keeps a second lease out of a
busy scope with a `NOT EXISTS` in-flight guard, and that guard reads the
statement snapshot. It cannot see a lease another claimer committed after the
snapshot was taken. Two claimers can disagree about a scope's oldest ready row.
A typical case is a retry that became visible between their two clocks, sitting
next to a newer pending generation. The claimers then lock different work rows,
and both claim.

The deterministic test `TestProjectorClaimScopeFenceExcludesCrossSnapshotClaim`
reproduces this on the d85fefd07d claim SQL:

1. Claimer B takes its snapshot, then pauses.
2. Claimer A, whose clock is 10 s ahead, claims gen-s1 and commits.
3. B resumes and claims gen-s2.

scope-s then holds 2 leases.

The #7108 load harness only logged overlapping leases. It now fails on them.
On the d85fefd07d SQL (40 s x 32 workers x 40 scopes), overlapping pairs per
run were:

| run set | 40 scopes | 10k-scope backlog |
| --- | --- | --- |
| set 1 | 2/1/0/3 | - |
| set 2 | 3/3/0/2 | 2 and 1 |

## Change

**Migration 130** adds the table `projector_scope_claim_fences`:

- `scope_id` is the primary key and references `ingestion_scopes`, `ON DELETE
  CASCADE`. `fence` is a `BIGINT`.
- An `AFTER INSERT` trigger on `ingestion_scopes` seeds a row with
  `ON CONFLICT DO NOTHING`.
- A backfill seeds rows for existing scopes, guarded by `NOT EXISTS`.

The file runs as one implicit transaction, and `CREATE TRIGGER` comes before
the backfill. `CREATE TRIGGER` holds SHARE ROW EXCLUSIVE on `ingestion_scopes`,
so a scope inserted during a rollout either committed first and is backfilled,
or waits for the migration and then fires the trigger.

**The claim** now works like this:

1. `claimProjectorWorkQuery` reads the scope's fence value into the
   materialized `candidate_pool` (`scoped_fence`).
2. The `candidate` lock step reads the pool through a subquery sorted on the
   claim-order keys. It locks the work row, then the fence row, both
   `SKIP LOCKED`, and joins on `claim_fence.fence = pool.snapshot_fence`. The
   sorted subquery lets the planner nested-loop the probes in claim order and
   stop at the first lockable row (see Plan and cost).
3. `claimed_scope_fence` bumps the fence in the same statement.

A claimer still in flight holds the fence row, so this claim skips the scope
without waiting. A claimer that already committed left a new row version, so
the EvalPlanQual recheck fails the fence comparison, the row drops out, and
`LockRows` moves on to the next candidate.

The claim never locks `ingestion_scopes`. Ack, Fail, heartbeat, retry, enqueue
and ingestion are unchanged.

**Protocol invariant** (stated next to the claim SQL and in
`go/internal/storage/postgres/AGENTS.md`):

- Any statement that creates a projector live lease must lock the scope's fence
  row and bump it in the same transaction.
- Nothing else may lock or update that table.
- No table may reference it.
- Any insert into it outside the trigger must use a `NOT EXISTS` guard.
  `INSERT ... ON CONFLICT` waits on an in-flight bump of the conflicting row.
  In a postgres:16 probe with one fence bump left open,
  `INSERT ... ON CONFLICT DO NOTHING` for that scope waited 2,670 ms, until
  the bump committed. The same insert guarded by `NOT EXISTS` returned in
  0.56 ms.

## Rejected first design (fence2)

The first design put the fence on `ingestion_scopes.projector_claim_fence` and
locked the scope row `FOR NO KEY UPDATE ... SKIP LOCKED`. It stopped the double
lease, but it deadlocked the claim against Ack.

Load runs at 40 s x 32 workers x 40 scopes, interleaved with the old SQL:

| variant | server deadlocks per run | overlapping pairs |
| --- | --- | --- |
| fence2 | 1/3/2/2, then 1/0/2/1 | 0 in all runs |
| old SQL | 0 in all runs | see Problem |

All eight Postgres deadlock reports pair the claim with Ack's
`ackProjectorWorkItemQuery`. The claim side reports `CONTEXT: while locking
updated version (...) of tuple in relation "ingestion_scopes"`.

Mechanism, confirmed against PG16 `heap_lock_tuple` and reproduced
deterministically:

- Every foreign-key child insert takes KEY SHARE on the scope row. The children
  include `fact_records`, `fact_work_items` and `scope_generations`.
- Suppose the claimer's snapshot version has a multixact xmax made of a running
  KEY SHARE locker and a committed Ack update, and a second Ack is updating the
  newer version.
- Locking the snapshot version then walks the update chain
  (`heap_lock_updated_tuple_rec`, `XLTW_LockUpdated`). That walk waits, and
  `SKIP LOCKED` does not cover it.
- The claim waits on the second Ack while holding the work row, and that Ack's
  next statement needs the same work row. The two block each other.

`TestProjectorClaimDoesNotWaitOnScopeUpdateChain` builds that state: a KEY
SHARE holder, a committed scope update, an open Ack-shaped transaction, and a
paused claimer.

- On the fence2 SQL it saw the `Lock`/`transactionid` wait in 2 of 2 runs. In
  one run Ack's second statement got `40P01`; in the other the claim was the
  deadlock victim and retried.
- Without the KEY SHARE holder the claim skipped immediately.

The fence table avoids this by construction. Only claimers lock it, always with
NO KEY UPDATE, which conflicts with itself, so no multixact with a running
member can form. Nothing references the table, so no KEY SHARE locker can
exist.

Two other variants were probed in scratch builds and not adopted:

- **fence2 with `FOR UPDATE` on the scope row.** It had 0 deadlocks and 0
  overlaps in 4 load runs, because KEY SHARE conflicts with `FOR UPDATE`, so
  the claim skips instead of walking the chain. But every FK child insert on
  a scope would then wait out a claim's lock-to-commit tail, and claims would
  skip any scope with an insert in flight.
- **A fence table joined in `source_scoped_projector_work`.** It is correct,
  but it probes the fence once per projector row rather than once per pool
  row.

## Proof

Environment: postgres:16 with the full bootstrap schema. The host load average
was 40-60, so wall times are noisy.

### Deterministic tests

RED is the d85fefd07d claim SQL (with the new harness), the fence2 SQL, or a
named mutation. GREEN is the branch head. The full live suite passed with
exit 0 at 993ad47186, at cf652057b7, and again after the rebase onto main.

| test | RED | GREEN |
| --- | --- | --- |
| `TestProjectorClaimScopeFenceExcludesCrossSnapshotClaim` | old SQL: B claims gen-s2, scope-s has 2 leases | B claims gen-z2; 1 lease |
| `TestProjectorClaimDoesNotWaitOnScopeUpdateChain` | fence2: lock wait observed, then 40P01; the scope-row-lock mutation fails too | returns gen-s1 attempt 2 while Ack is open; the old attempt's Ack matches 0 rows; deadlock delta 0 |
| `TestProjectorClaimFenceRowNeverMakesClaimersWait` (C1 committed a bump; C3 is in flight with lock only, or lock plus bump) | old SQL: C2 claims gen-s in both subtests | no wait; C2 claims gen-z2 |
| `TestProjectorClaimSkipsBusyScopeWithoutWaiting` (fence row held) | old SQL: claims gen-s | skips in 10 ms; claims gen-o |
| `TestProjectorClaimIgnoresIngestionHoldingScopeRow` | scope-row-lock mutation: fails | claims gen-s in 11 ms while ingestion holds the scope row |
| `TestProjectorClaimLeavesFenceUnlockedWhenWorkRowBusy` | old SQL and swapped clauses: fail | skipped scope's fence row is free; claimed scope's fence row is locked (55P03); its scope row is free |
| `TestProjectorClaimFenceRowLifecycle` | old SQL: no bump; trigger removed: no row | upsert creates the row, re-upsert keeps it, direct insert plus `Enqueue` is claimable, delete cascades |
| `TestProjectorClaimSkipsScopeWithoutFenceRow` | old SQL: claims gen-m | never claimed; gauge query returns 1 |
| `TestProjectorScopeClaimFenceMigrationBackfills` | trigger removed from 130: fails | one row per scope; rerun is a no-op that keeps a bumped fence; the trigger fires afterwards |
| `TestProjectorClaimSortedLockStepMatchesWholePool` (differential against the whole-pool lock step derived from the shipped constant; states: key ties, expired lease, source with a live lease, busy first fence row, busy first work row, all busy) | differs if the outer ORDER BY is dropped (see Mutation probes) | same candidate in all six states |
| `TestProjectorClaimSortedLockStepStopsAtFirstLockableRow` (live EXPLAIN ANALYZE; 30 ready scopes, first fence row held) | whole-pool lock step: Sort above the join, 30 probes | Limit -> LockRows -> Nested Loop, no Sort; fence and work probes 2 loops each; claims the second candidate |
| hermetic `TestProjectorQueueClaimFencesTheScope`, `TestProjectorQueueClaimNeverLocksIngestionScopes`, `TestWholePoolClaimQueryDerivedFromShippedQuery` | old SQL: fails | pass |

The following suites also stay green:

- every #7108 live test (DoesNotWait, LockRechecks, MaintenanceSemantics, the
  concurrent load);
- the heartbeat, Ack and ingestion lock-order, supersession, attempt-fence and
  stranded-retry suites;
- generation liveness and the workflow dead-letter integration tests.

### Mutation probes

Each mutation was applied to d09bd845b5, which is b2776bcd0c before the rebase
onto main. `git range-diff` shows those commits unchanged apart from migration
pins, the checksum manifest, schema order and one metrics-table context line.
After each mutation the files were restored and confirmed unchanged with
`git diff --quiet`.

| mutation | live test that kills it | hermetic |
| --- | --- | --- |
| claim SQL from d85fefd07d (RED) | `ScopeFenceExcludesCrossSnapshotClaim`, `SkipsBusyScopeWithoutWaiting`, `LeavesFenceUnlockedWhenWorkRowBusy`, `FenceRowLifecycle`, `SkipsScopeWithoutFenceRow`, `FenceRowNeverMakesClaimersWait`, both sorted-lock-step tests | killed |
| drop the fence bump | `ScopeFenceExcludesCrossSnapshotClaim`, `FenceRowLifecycle` | killed |
| drop the fence equality | `ScopeFenceExcludesCrossSnapshotClaim` | killed |
| compare the fence to itself | `ScopeFenceExcludesCrossSnapshotClaim` | killed |
| drop `SKIP LOCKED` on `claim_fence` | `SkipsBusyScopeWithoutWaiting`, `FenceRowNeverMakesClaimersWait`, and the differential (see note 1) | killed |
| also lock `ingestion_scopes` in the lock step | `DoesNotWaitOnScopeUpdateChain`, `IgnoresIngestionHoldingScopeRow`, `LeavesFenceUnlockedWhenWorkRowBusy` | killed (`NeverLocksIngestionScopes`) |
| swap the locking clauses | `LeavesFenceUnlockedWhenWorkRowBusy` | killed |
| drop `claim_until` from the lock step | `LockRechecksRowsChangedAfterSnapshot` (#7108) | killed |
| drop `MATERIALIZED` | `SortedLockStepStopsAtFirstLockableRow` | killed |
| drop the lock step's subquery ORDER BY | `SortedLockStepStopsAtFirstLockableRow` | killed |
| change only the subquery's ORDER BY keys | `SortedLockStepStopsAtFirstLockableRow`; the differential passes (see note 2) | killed |
| change the subquery's keys and drop the outer ORDER BY | `SortedLockStepMatchesWholePool`, `SortedLockStepStopsAtFirstLockableRow`, `IgnoresIngestionHoldingScopeRow` | killed |
| `LIMIT 1` inside the subquery | `SortedLockStepMatchesWholePool`, `SkipsBusyScopeWithoutWaiting`, `ScopeFenceExcludesCrossSnapshotClaim`, `FenceRowNeverMakesClaimersWait`, `LeavesFenceUnlockedWhenWorkRowBusy`, `LockRechecksRowsChangedAfterSnapshot`, `SortedLockStepStopsAtFirstLockableRow` | killed |
| remove the trigger from migration 130 | `FenceRowLifecycle`, `MigrationBackfills`, `SkipsScopeWithoutFenceRow` and every claim test | killed |

Notes:

1. With `SKIP LOCKED` dropped, the differential's busy-first-fence-row case
   blocked on the held lock until the run was stopped. The claim step in both
   new tests now sets `lock_timeout = 5s`, so a waiting claim fails fast
   instead.
2. Changing only the inner keys leaves the result unchanged, because the outer
   ORDER BY still decides it; only the early stop is lost, and the plan test
   catches that. The differential is shown able to fail by the mutation that
   also drops the outer ORDER BY. In that run, the ties state claimed
   gen-scope-a against the reference's gen-scope-c, and the multi-source state
   claimed gen-aws against gen-git.

### Load

No-Regression Evidence: `TestProjectorClaimConcurrentLoadHasNoDeadlock` at
993ad47186. Each run lasts 40 s with 32 workers and 150 ms leases, while a
producer enqueues concurrently. The run fails on any overlapping lease pair or
any server deadlock.

| scopes | run | claims | server deadlocks | overlapping pairs |
| --- | --- | --- | --- | --- |
| 40 | 1 | 474 | 0 | 0 |
| 40 | 2 | 503 | 0 | 0 |
| 40 | 3 | 589 | 0 | 0 |
| 40 | 4 | 1,493 | 0 | 0 |
| 10,000 | 1 | 368 | 0 | 0 |
| 10,000 | 2 | 476 | 0 | 0 |

cf652057b7 made the lock step read a sorted subquery. The same load then ran
on a binary whose claim SQL is identical to cf652057b7's (built in a scratch
worktree; the only differences are SQL comments):

| scopes | run | claims | server deadlocks | overlapping pairs |
| --- | --- | --- | --- | --- |
| 40 | 1 | 364 | 0 | 0 |
| 40 | 2 | 1,685 | 0 | 0 |
| 40 | 3 | 551 | 0 | 0 |
| 40 | 4 | 390 | 0 | 0 |
| 10,000 | 1 | 621 | 0 | 0 |
| 10,000 | 2 | 706 | 0 | 0 |

A scratch build of the same table design, with the fence join in the snapshot
CTE, recorded per-call `Claim` latency at 40 scopes over 4 rotated runs:

| variant | median p50 | median p99 |
| --- | --- | --- |
| fence table | 4.0 ms | 54 ms |
| old SQL | 4.0 ms | 64 ms |

The run-to-run spread is larger than that difference.

### Plan and cost

`EXPLAIN` at 2k and 20k scopes (10 generations per scope, 3 reducer rows per
generation) shows `Index Scan using projector_scope_claim_fences_pkey` for
`scoped_fence`, `claim_fence` and the bump. There is no sequential scan and no
join filter on the fence table.

The first fenced lock step, at 993ad47186, joined the whole pool to its work
and fence rows before its Sort and Limit. It therefore probed every pool row:
`claim_fence` ran 1,800 loops at 2k scopes and 18,000 at 20k. Two
sort-then-probe variants were shimmed against it:

- **ORDER BY inside the materialized `candidate_pool`: no gain.** A CTE scan
  exposes no ordering to the outer query, so the plan stays
  `Limit -> LockRows -> Sort -> Nested Loop` and still probes every row.
- **The lock step reads `(SELECT * FROM candidate_pool ORDER BY <claim-order
  keys>) AS pool`: early stop.** The outer ORDER BY is unchanged. The planner
  sees the subquery's pathkeys, drops the top Sort, and plans
  `Limit -> LockRows -> Nested Loop (Subquery Scan -> Sort -> CTE Scan, work
  pkey, fence pkey)`. `claim_fence` runs 1 loop at both 2k and 20k.
  `LockRows` still pulls the next row after a `SKIP LOCKED` skip:
  `TestProjectorClaimSkipsBusyScopeWithoutWaiting` claims gen-o while scope-s's
  fence row is held.

cf652057b7 adopts the second variant.

Buffer and wall A/B: `EXPLAIN (ANALYZE, BUFFERS)` in BEGIN/ROLLBACK against
the d85fefd07d SQL, on a fresh seed and a fresh postgres:16 container. Four
variants ran interleaved in each round, with the first mover rotated.
Rolled-back supersedes leave dead tuples, so the hit count drifts up by about
1.5k per statement at 2k and about 55k at 20k. Deltas are therefore taken
within each round.

| backlog | rounds | old median hits | added hits: whole-pool lock step | ORDER BY in CTE | sorted subquery (shipped) |
| --- | --- | --- | --- | --- | --- |
| 2,000 scopes | 7 | 1,351,680 | +19,516 (+1.45%) | +16,193 | +3,756 (+0.28%) |
| 20,000 scopes | 2 | 118.9M | +110k and +196k | +144k and +142k | +36k and +34k (+0.03%) |

The remaining added cost is the `scoped_fence` probe in `candidate_pool`, one
per pool row (about 2 hits each). That probe is how the claim reads its
snapshot fence.

Median wall at 2k: old 1,141 ms; whole-pool 1,121 ms; ORDER BY in CTE 1,013 ms;
sorted subquery 881 ms. At 20k each claim took 107-198 s in every variant. Host
load was 18-35. None of these wall differences is claimed as an effect.

An earlier A/B, taken in a lighter queue state where the claim cost 121k hits,
showed the whole-pool lock step at +16.4% hits and 96 -> 106 ms. The sorted
subquery removes that per-pool-row cost. For comparison, fence2 measured about
+1% on the heavier seed. The seed was rebuilt from the #7108 description,
because the #7108 seed script was not committed.

The sort-then-probe lock step resolves #7198, the follow-up filed for this
per-pool-row cost.

Lock-to-commit tail, measured from the candidate `LockRows` start to the end of
the statement: about 4-12 ms at 2k scopes and 0.2-1.2 s at 20k scopes. Ack no
longer waits on this tail, since Ack never touches the fence row.

Fence bumps are HOT-eligible, because `fence` is in no index. 300 committed
claims at 2k scopes produced 300 updates, 256 of them HOT (85%). The
`ingestion_scopes` update counters did not move. The rejected column design got
57% HOT on the wide scope row.

## Documentation deviation

`go/internal/telemetry/README.md` is at the 500-line Markdown cap, so neither
gauge is listed in its observable-gauge table. The two gauges are documented
in `docs/public/reference/telemetry/metrics.md` and in the projector queue row
of `docs/public/observability/telemetry-coverage.md`, and the telemetry
coverage gate passes.

## Observability

Observability Evidence: `telemetry.RegisterObservableGauges` registers both
gauges whenever the queue observer it is handed implements
`ProjectorClaimInvariantObserver`. Since #7214 no binary hands it the raw
`QueueObserverStore`; both binaries pass snapshot-cached wrappers, and a wrapper
that does not implement the interface silently drops the gauges. The reducer's
Postgres gauge snapshot (`registerPostgresBackedGauges`, snapshot gauge
`reducer_projector_claim_invariants`) therefore reads both counts on its
background refresh and its cached queue observer implements the interface, so
the gauges are served from the last snapshot, never computed on a `/metrics`
scrape. `TestPostgresBackedGaugesServeProjectorClaimInvariants` drives that
production wiring path. Only the reducer serves them: both counts are
database-wide, so one emitter is complete and a second would only repeat the
query; the ingester's wrappers deliberately do not carry them. The refresh
cadence is `ESHU_POSTGRES_GAUGE_REFRESH_INTERVAL` (default `5m`), and
`eshu_dp_gauge_snapshot_age_seconds{gauge="reducer_projector_claim_invariants"}`
shows staleness. Like the other scalar snapshot gauges they report nothing, not
zero, until the first successful refresh.

- `eshu_dp_projector_scopes_multiple_live_leases`: scopes holding more than one
  unexpired claimed or running projector lease. It should always read zero.
- `eshu_dp_projector_scopes_missing_claim_fence`: scopes with claimable
  projector work but no fence row. Such a scope is silently stalled. It should
  always read zero. The query is one anti-join; at 200k projector rows (800k
  `fact_work_items`) it ran as a Hash Anti Join in 8.6 ms with 1,393 shared
  hits. That cost is paid once per snapshot refresh (default every 5 minutes),
  not per scrape.

Existing signals still apply:

- `eshu_dp_queue_claim_conflict_retries_total` should stay near zero.
- `eshu_dp_queue_claim_duration_seconds` carries claim latency.

## Residual risk

- **Pre-existing, filed as #7187.** The #7108 maintenance lock
  `locked_stale_scope_generations` locks `scope_generations` rows. Foreign-key
  children take KEY SHARE on those rows, so the same update-chain wait is
  reachable there. This change leaves that path alone.
- **Mixed-version rollout.** Old claimers neither lock nor bump the fence, so a
  double lease between an old and a new claimer stays possible until the
  rollout completes.
- **Missing fence row.** It makes the scope unclaimable, never double-leased.
  The trigger, the backfill and the missing-fence gauge cover it.
- **Behaviour change relative to fence2.** A scope whose `ingestion_scopes` row
  is held by ingestion is no longer deferred. This matches main before #7115.
- **Migration 130 rollout lock.** `CREATE TABLE ... REFERENCES
  ingestion_scopes` and `CREATE TRIGGER` both take SHARE ROW EXCLUSIVE on
  `ingestion_scopes`, which conflicts with the ROW EXCLUSIVE lock any open
  ingestion writer holds. The migration runner (`applyTrackedDefinitions`)
  gives each attempt a 5 s `lock_timeout`, then retries SQLSTATE 55P03 with
  backoff inside a 3-minute budget. While an attempt waits, other
  `ingestion_scopes` writers queue behind it for up to that `lock_timeout`:
  Ack's scope update, the ingestion upsert, and Fail's scope update. Readers
  are not blocked.
  - A reviewer probe with a 3 s `lock_timeout` and one open writer: the
    migration got 55P03 at 3.20 s, and a writer on another scope waited
    2.75 s.
  - What an operator sees: `bootstrap.postgres.migration.lock_wait` warnings
    with `budget_left_ms`, then `bootstrap.postgres.migration.lock_recovered`.
  - Under continuous ingestion that never releases its lock for 3 minutes,
    the budget runs out, the schema Job fails with "lock retry budget ...
    exhausted", and the Job's own retry tries again.
  - Migrations 093, 112 and 120 recreate triggers on `fact_work_items`, which
    carries the same exposure.
- **Migration number.** The migration is numbered 130, after #6679's 125 and
  #6475's 126-129 (the service materialization lineage scope). The embed
  ledger pins 151 definitions.
