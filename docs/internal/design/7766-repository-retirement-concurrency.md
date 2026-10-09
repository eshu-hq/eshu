# Repository Retirement: Concurrency Contract (#7766)

Issue: #7766
Companions: [Repository Retirement](7766-repository-retirement.md) (the
design) and [Proof And Rollout](7766-repository-retirement-proof-and-rollout.md)
(prove-first table, results, corrections).

This file holds what the prove-first results changed: the phase 1 lock budget,
the intent-delete barrier that replaced the shared-lease horizon, the
invariants that make the phase 2 order safe, the folded commit gate, and the
re-admission interleavings. P1 and P9 failed as originally designed, so these
sections are the amended design, not the first draft. Binding inputs: the
[shared-contract arbiter ruling](https://github.com/eshu-hq/eshu/issues/7766#issuecomment-6073882598)
and the [arbiter ruling on the #7766 prove-first
results](https://github.com/eshu-hq/eshu/issues/7766#issuecomment-6082885964). Measurements are in
[Prove-First Results](7766-repository-retirement-proof-and-rollout.md#prove-first-results).

Source check: origin/main 16c8c2a36 (read by the arbiter, 2026-10-09); the
first draft was checked against 195337b97.

## Phase 1 Lock Budget

Phase 1 takes `LOCK TABLE fact_work_items IN EXCLUSIVE MODE` (step 6). The
measured cost has three parts: how long the section runs, who it blocks, and
how a waiting `LOCK TABLE` blocks everyone behind it.

### Bindings

The design form bound 7c, 7d, and the step 6 recheck to every
`(scope_id, generation_id)` pair of the scopes. It failed the 250 ms bar
(P1: design-form Rc5000 cold p99 252.2 ms, Wc5000 cold 885.9 ms). The
amended bindings:

| Statement | Design form | Amended binding |
| --- | --- | --- |
| 7c, projector rows to `superseded` | pairs for all generations | step-4 non-superseded `(scope_id, generation_id)` pairs |
| 7d, reducer rows delete | pairs for all generations | `scope_id = ANY($scopes)` |
| Step 6 recheck of live reducer leases | pairs for all generations | `scope_id = ANY($scopes)` |

This is valid because the retire unit is the whole scope, every generation.
`refinalize` binds a subset of generations, which is why its pair form is
right for it and wrong here.

- **7d by scope** deletes the reducer rows of every generation of a scope
  that is going away. Phase 2d's cascade would delete the same rows later.
  The live-lease guard of the 7d predicate is unchanged.
- **7c by non-superseded pairs** skips projector rows of generations that were
  already `superseded`. The #7130 claim branch already supersedes those rows,
  and Ack and Heartbeat refuse them.

Both stay in phase 1.

- **7d cannot move to phase 2.** The reducer claim's supersede CTE requires
  `scope.active_generation_id = active_generation.generation_id`
  (`storage/postgres/reducer_generation_filter_sql.go`). Step 7b nulls that
  pointer, so after phase 1 commits a pending reducer row of the retired
  scope is claimable unless it was deleted under the fence.
- **7c is droppable for fencing, and still stays.** `candidate_pool`
  excludes every supersedable row
  (`storage/postgres/projector_queue_claim_sql.go`) and Ack and Heartbeat
  refuse. Dropping 7c would move a one-off sweep of up to 5,000 generation-row
  and 5,000 work-row locks into an unrelated worker's claim statement, which is
  a hot path nobody measured. Tuned 7c costs 6.5 ms cold on the realistic
  shape and 32 ms on the worst.
- 7e could move to phase 2. It costs 0.5 ms, so it stays.

### Lock waits

Set `lock_timeout` per acquisition, not once per transaction:

1. `set_config('lock_timeout','5min',true)` before step 3. The advisory wait
   convoys nothing: a commit waiting on it holds no table lock yet
   (`ingestion.go:219` precedes every write), and only this repository's commits
   queue behind it, which is the intent.
2. `set_config('lock_timeout','1s',true)` immediately before step 6. This is
   the pattern in `activation/sql.go:63`: a short wait plus a deferred retry.

On `55P03` at step 6: roll back, sleep a jittered 100 to 500 ms, and re-run
steps 1 to 8. The total budget is 30 s. After that the API returns 409
`blocked` with reason `claim_fence_busy`. No marker row exists, so
`claim_fence_busy` is an API response reason and not a `blocked_reason` value.
A retry restarts step 2's drain wait, which is cheap when the scopes are
already drained.

Why 1 s and not the old 5 minutes: PostgreSQL grants conflicting lock requests
in queue order, so a waiting `EXCLUSIVE` blocks every later `ROW EXCLUSIVE`
requester. Measured: a `LOCK TABLE` waited 6,975 ms behind a `ROW EXCLUSIVE`
holder, and an unrelated single-row `UPDATE` then waited 5,963 ms behind that
waiter (`p1_convoy_B_lock.txt`, `p1_convoy_C_unrelated_update.txt`). The convoy
came from step 6 inheriting step 3's `SET LOCAL`, not from the advisory wait.
`refinalize` has the same defect (see Out Of Scope in the proof file).

### Request budget

The 25-repository cap stays as the request cap. It does not bound the cost.
The cost is bound by two sums measured on the worst shape (Ww5000: 5,000
generations to supersede, 19,996 reducer rows to delete):

- After step 1, before any lock, count per repository the non-superseded
  generations and the reducer rows that match the 7d predicate. Refuse with 409
  `request_too_large` and the per-repository counts when the sum of
  non-superseded generations exceeds 5,000 or the sum of 7d-predicate rows
  exceeds 20,000.
- The precheck is a snapshot. Step 4 is authoritative: if the re-read under the
  lock exceeds either bound, abort with the same 409.

## Shared-Projection Barrier

The first draft recorded `max(lease_expires_at)` of live shared-projection
leases as `shared_lease_horizon` and waited for `now()` to pass it. P9H showed
that this is unsound in code, not only in a repro:

- `claimPartitionLeaseSQL`'s `ON CONFLICT` rewrites `lease_expires_at` whenever
  `lease_owner = $4` (`storage/postgres/shared_intents.go:180-186`), and the
  heartbeat calls it every TTL/2
  (`reducer/intents/shared/worker/heartbeat.go:76-80`).
- The lease is held from claim (`worker/process.go:301`) through `WriteEdges`
  and `MarkIntentsCompleted` (`:421`) to release (`:339`).
- Measured: with the horizon taken at t=300 ms, `now() > horizon` was true at
  t=1.5 s, the lease was still renewed 3.0 s past the horizon at t=3.5 s, and
  the worker stayed inside `RetractEdges` until t=4.0 s.
- code_calls shares the table and the heartbeat shape
  (`code/call/projection/lease.go:58-66`, `runner.go:267`; TTL 60 s at
  `cmd/reducer/config_projection.go:45`, shared default 60 s at
  `worker/runner.go:30`).

The replacement is a claim-epoch barrier. Rejected alternatives:

- **Claim every partition of every shared domain** (a fleet-wide pause). This
  serialises unrelated work for the length of a retirement.
- **A worker-side marker or acceptance check alone.** It is check-then-write
  across Postgres and the graph, so it narrows the window and cannot close it.

The barrier is sufficient by itself: every edge written from a pre-barrier read
lands before the barrier passes, and 2c retracts it.

### Migration and claim SQL

Both changes ship in the retirement migration:

```sql
ALTER TABLE shared_projection_partition_leases
    ADD COLUMN IF NOT EXISTS claimed_at TIMESTAMPTZ NULL;
-- claimPartitionLeaseSQL: the INSERT sets claimed_at = clock_timestamp();
-- the DO UPDATE arm adds
--   claimed_at = CASE WHEN shared_projection_partition_leases.lease_owner = EXCLUDED.lease_owner
--                     THEN shared_projection_partition_leases.claimed_at
--                     ELSE clock_timestamp() END
```

The same owner keeps its epoch across heartbeats. `releasePartitionLeaseSQL`
nulls the owner, so the next cycle gets a fresh epoch. This is safe because
every projection lease owner is process-unique
(`cmd/reducer/config_projection.go:118-142`: hostname plus a process suffix for
shared, code_calls, and repo_dependency).

### Phase 2 order

The order is **2b, 2b', 2a, 2c, 2d, 2e**.

- **2b, intent delete and barrier.** Chunked (10,000 rows)
  `DELETE FROM shared_projection_intents WHERE repository_id = $repo` (index
  `shared_projection_intents_repo_run_idx`, `repository_id` leading) and
  `WHERE scope_id = ANY($scopes)` (index `..._acceptance_lookup_idx`,
  `scope_id` leading). The same for `shared_projection_unroutable_intents`.
  Assert 0 rows remain by both predicates, and commit. Then, in a later
  statement, `UPDATE repository_retirements SET intent_barrier_at =
  clock_timestamp()`. The barrier must postdate the delete's commit.
- **2b', lease-epoch wait.** The row stays `state=blocked`,
  `blocked_reason=shared_lease_live`, until this returns 0:

  ```sql
  SELECT count(*) FROM shared_projection_partition_leases
  WHERE lease_owner IS NOT NULL AND lease_expires_at > clock_timestamp()
    AND (claimed_at IS NULL OR claimed_at <= $intent_barrier_at)
  ```

  The query is fleet-wide because nothing records which partition holds this
  repository's intents. The readback names the oldest blocking
  `(projection_domain, partition_id, lease_owner, claimed_at)`, so an operator
  sees which partition to look at.
- **2a, projector-lease wait.** No projector row of the scopes has
  `claim_until > now()` (`projector_lease_live`). It converges because phase 1
  stopped new claims.
- 2c, 2d, and 2e are unchanged (see the design).

## Invariants

- **I1.** Every writer of shared-domain graph edges holds a
  `shared_projection_partition_leases` row from before it reads intents until
  after its last graph write and `MarkIntentsCompleted`. Confirmed for the
  shared worker (`worker/process.go:301-339`) and code_calls. NOT_CHECKED for
  repo_dependency (see the proof file).
- **I2.** After phase 1 commits, no new intent keyed to the retired scopes can
  be created: reducer rows are deleted or have no live lease at commit
  (`AssertRetirementFenced`, `refinalize.go:401-418`), and commits are refused.
- **I3.** No acceptance row of the retired scopes is deleted before 2b's delete
  has committed. The only acceptance delete is 2d's cascade
  (`migrations/011:2-5`), which runs after.
- **I4.** An edge written for the retired repository by a pre-barrier cycle is
  harmless: 2c retracts it, and that cycle's `MarkIntentsCompleted` affects 0
  rows.

## Why 2b Goes First

Intents and acceptance rows are written together
(`shared_intent_acceptance_writer.go:111-121`, one transaction when a beginner
exists, `:78-93`). An intent without an acceptance row can therefore come only
from a cascade delete or a torn non-transactional write. All three selectors
skip such an intent forever (`reducer/intents/shared/worker/selection.go:98-101`,
`code/call/projection/selection.go:203-206`,
`repo_dependency_projection_runner.go:348-350`, which errors at its 10,000 cap
at `:366-369`). The indexed shared path returns an empty batch at the 10,000
cap with no error (`process.go:174-177`), and `MarkIntentsCompleted` ignores
rows affected (`shared_intents.go:265-276`).

Measured (P9, real Postgres): an intent whose acceptance row or generation was
deleted before selection stayed unprocessed and uncompleted for five cycles,
and 10,100 such intents plus one healthy intent made a cycle take 2.1 s and
process 0.

With 2b first and I3, retirement creates no orphans. The zero-count assertion
after 2b is the #7766 guard. A drain for orphans that already exist is a
separate pre-existing bug (Out Of Scope in the proof file).

## Commit Gate Folded Into The Scope Upsert

A separate lookup statement failed the 1% bar at facts=1 (P2: median +3.04% at
0 marker rows, +2.25% at 10,000). Folding the gate into the scope upsert
measured +0.46% and -0.09%. The design uses the folded shape:

```sql
WITH gate AS (
  SELECT state FROM repository_retirements
  WHERE repo_id = $7 AND readmitted_at IS NULL LIMIT 1)
INSERT INTO ingestion_scopes (...)
SELECT ... WHERE NOT EXISTS (SELECT 1 FROM gate WHERE state <> 'complete')
ON CONFLICT (...) DO UPDATE SET <arm unchanged>
RETURNING (SELECT state FROM gate)
```

- **Zero rows returned** is the `repository_retiring` refusal, on the
  finalized-skip path in the design's writer table.
- **`state = 'complete'`** in the returned value triggers the `readmitted_at`
  stamp: one extra statement, on the rare re-admission.
- **Invariant:** the gate and the upsert share one statement snapshot, taken
  under the shared advisory lock. Phase 1 holds that key exclusively until its
  commit, so a marker either committed before the gate reads or phase 1 is still
  waiting. There is no check-then-write window.
- **The `ON CONFLICT` arm stays byte-identical** to `ingestion_queries.go`
  (the status CASE, bound at `:249`). A test derives the expected prefix from
  that file and compares.
- **Cost of the shape:** `upsertIngestionScope` moves from `Exec` to `Query`,
  and `LIMIT 1` relies on the partial unique open-repo index.
- **Bar:** median delta at most 1% at facts=1 and facts=400 with 10,000 marker
  rows, at least 6 runs each, re-measured on the real PR 3 code.

## Re-Admission And In-Flight Syncs

The gate reads the open row for `partition_key` under the shared advisory lock:
- if the row is non-`complete`, the gate refuses;
- if it is `complete`, the commit transaction runs
  `UPDATE ... SET readmitted_at=$now WHERE retirement_id=$1 AND readmitted_at IS NULL AND state='complete'`.
  With one affected row it logs WARN `repository readmitted after retirement`
  and increments `outcome=readmitted_after_retirement`. A rollback undoes both.

The scope row is gone at this point, so the upsert inserts fresh. The claim's
prior-generation probe (`projector_queue_claim_sql.go:428-433`) returns false,
which makes this a first generation.

Interleavings are linearised by the advisory key:
- a commit that holds the shared lock first lands, and phase 1 then supersedes
  its generation at step 4;
- a phase 1 that commits first makes the later commit refuse;
- a sync that started before retirement but commits after `complete`
  re-admits. That is the ruling's semantics, and the dry-run `will_readmit`
  warns about it.

Two concurrent re-admitting commits (default plus ref scope) serialise on the
row, and only one stamps it.

Two cases cannot happen:
- **A sync over a half-purged scope.** Every state before `complete` refuses.
- **Lost triggers.** A retiring-time trigger fails with `repository_retiring`.
  Triggers that arrive after `complete` re-admit, as the ruling says for
  webhook-only mode.

## Residual Risks Of The Barrier

- A worker that renews forever holds the barrier. The row stays
  `blocked/shared_lease_live` with the lease named. It never fails
  automatically.
- Lease rows from before the migration have `claimed_at IS NULL` and block
  until their first re-claim, which is one cycle.
- A writer whose lease already expired is outside the lease contract and is not
  covered. This is true of every lease in the system today.
- `claimPartitionLeaseSQL` is on the heartbeat path. P9b bounds the cost of the
  added arm before code lands.

## Runner Crash And Retry

**Crash or restart.** The lease expires, and another runner reclaims the row
and resumes at `phase`/cursor:
- each 2d batch committed its deletes and its counts together, so a re-run
  selects only the generations that remain;
- a graph statement re-run deletes nothing;
- 2b's chunked delete re-runs to zero. `intent_barrier_at` is set once, after the
  delete commits, and a re-run never moves it earlier;
- graph counts are at most once, because a statement that commits before its
  cursor persists is not re-counted. This is the same caveat as 7324.

**Failure handling:**
- transient errors (40001, 40P01, 55P03, retryable graph errors) back off
  exponentially, capped at 5 min;
- non-transient errors (`failure_class` bounded) go to `failed` after 5 attempts;
- re-issuing the retire request (new key) resets `failed` → `pending` on the same row.
