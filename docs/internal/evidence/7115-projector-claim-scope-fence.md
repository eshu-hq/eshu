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

**Migration 126** adds the table `projector_scope_claim_fences`:

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
2. The `candidate` lock step locks the work row, then the fence row, both
   `SKIP LOCKED`. It joins on `claim_fence.fence = pool.snapshot_fence`.
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
  `INSERT ... ON CONFLICT` waits on an in-flight bump of the conflicting row;
  the arbiter measured 2,670 ms, against 0.56 ms with the guard.

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

The raw reports and scratch-build matrices are in the #7115 working notes. Two
alternatives were probed there and not adopted:

- fence2 with `FOR UPDATE`;
- a fence table with the join in the snapshot CTE.

## Proof

Environment: postgres:16 with the full bootstrap schema. The host load average
was 40-60, so wall times are noisy.

### Deterministic tests

RED is the d85fefd07d claim SQL (with the new harness), the fence2 SQL, or a
named mutation. GREEN is 993ad47186 with d2f1d8f95f.

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
| `TestProjectorScopeClaimFenceMigrationBackfills` | trigger removed from 126: fails | one row per scope; rerun is a no-op that keeps a bumped fence; the trigger fires afterwards |
| hermetic `TestProjectorQueueClaimFencesTheScope` and `TestProjectorQueueClaimNeverLocksIngestionScopes` | old SQL: fails | pass |

The following suites also stay green:

- every #7108 live test (DoesNotWait, LockRechecks, MaintenanceSemantics, the
  concurrent load);
- the heartbeat, Ack and ingestion lock-order, supersession, attempt-fence and
  stranded-retry suites;
- generation liveness and the workflow dead-letter integration tests.

### Mutation probes

Each mutation was applied to 993ad47186. Afterwards the files were restored and
confirmed unchanged with `git diff --quiet`.

| mutation | live test that kills it | hermetic |
| --- | --- | --- |
| drop the fence bump | `ScopeFenceExcludesCrossSnapshotClaim`, `FenceRowLifecycle` | killed |
| drop the fence equality | `ScopeFenceExcludesCrossSnapshotClaim` | killed |
| compare the fence to itself | `ScopeFenceExcludesCrossSnapshotClaim` | killed |
| drop `SKIP LOCKED` on `claim_fence` | `SkipsBusyScopeWithoutWaiting` | killed |
| also lock `ingestion_scopes` in the lock step | `DoesNotWaitOnScopeUpdateChain`, `IgnoresIngestionHoldingScopeRow`, `LeavesFenceUnlockedWhenWorkRowBusy` | killed |
| swap the locking clauses | `LeavesFenceUnlockedWhenWorkRowBusy` | killed |
| drop `claim_until` from the lock step | `LockRechecksRowsChangedAfterSnapshot` (#7108) | killed |
| drop `MATERIALIZED` | none | killed |
| remove the trigger from migration 126 | `FenceRowLifecycle`, `MigrationBackfills`, `SkipsScopeWithoutFenceRow` and every claim test | killed |

Dropping `MATERIALIZED` changes plan shape, not correctness, which is why no
live test catches it. `EXPLAIN` at 2k and 20k scopes shows the pool inlined
into one join tree with a hash join and a fence join filter.

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

The buffer A/B ran `EXPLAIN (ANALYZE, BUFFERS)` inside BEGIN/ROLLBACK against
the d85fefd07d SQL, interleaved, with the first mover alternating:

| backlog, queue state | pairs | old -> fence table shared hits | median wall |
| --- | --- | --- | --- |
| 2k scopes, before committed claims | 7 | 121,425 -> 141,420 (+16.4%) | 96.2 -> 106.1 ms |
| 2k scopes, after 300 committed claims | 5 | 2,489,544 -> 2,506,909 (+0.7%) | noise (1.4-2.0 s both) |
| 20k scopes | 2 | 1,660,618 -> 1,860,532 (+12.0%) | 17.7 -> 8.8 s, both orders; not claimed as a speedup |

The added cost is roughly 10 shared hits per pool row, and it scales linearly
with the pool:

- one fence primary-key probe per pool row in `candidate_pool`;
- one fence probe and one work-row primary-key probe per pool row in the lock
  step, because the lock step joins the whole pool before its Sort and Limit.

fence2 had the same lock-step probes against `ingestion_scopes`. Relative cost
therefore depends on how expensive the rest of the claim is: +0.7% when the
claim costs 2.5M hits, and +16% when it costs 121k.

A shim of a lazy lock step (the pool sorted in a subquery, so `LockRows` probes
only until the first lockable row) touched one pool row. It cut the added cost
to +5,472 hits (+0.22%) in the 2.5M-hit state. That shape is not in this
change; it is a follow-up candidate.

The seed was rebuilt from the #7108 description, because the #7108 seed script
was not committed.

Lock-to-commit tail, measured from the candidate `LockRows` start to the end of
the statement: about 4 ms at 2k scopes and 202-212 ms at 20k scopes. Ack no
longer waits on this tail, since Ack never touches the fence row.

Fence bumps are HOT-eligible, because `fence` is in no index. 300 committed
claims at 2k scopes produced 300 updates, 256 of them HOT (85%). The
`ingestion_scopes` update counters did not move. The rejected column design got
57% HOT on the wide scope row.

## Observability

Observability Evidence: `telemetry.RegisterObservableGauges` registers both
gauges on the queue-depth cadence whenever the queue observer implements
`ProjectorClaimInvariantObserver`. `QueueObserverStore`, used by the reducer and
the ingester, does.

- `eshu_dp_projector_scopes_multiple_live_leases`: scopes holding more than one
  unexpired claimed or running projector lease. It should always read zero.
- `eshu_dp_projector_scopes_missing_claim_fence`: scopes with claimable
  projector work but no fence row. Such a scope is silently stalled. It should
  always read zero. The query is one anti-join; at 200k projector rows (800k
  `fact_work_items`) it ran as a Hash Anti Join in 8.6 ms with 1,393 shared
  hits.

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
- **Migration number.** The migration is numbered 126 because open PRs hold
  122-125. Whichever change lands later renumbers and re-pins the digest and
  manifest.
