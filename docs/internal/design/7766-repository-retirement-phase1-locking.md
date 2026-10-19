# Repository Retirement: Phase 1 Locking (#7766)

Issue: #7766
Companions: [Repository Retirement](7766-repository-retirement.md) (the fence
sequence these sections justify), [Concurrency Contract](7766-repository-retirement-concurrency.md)
(the barrier, the invariants, the commit gate), and
[Proof And Rollout](7766-repository-retirement-proof-and-rollout.md).

This file holds the phase 1 lock budget and deadline, the reuse of the #7852
fence, and the lock-order rule with the `Fail` fix. It was split out of the
concurrency contract to keep each file under the Markdown line cap. Binding
inputs are the same arbiter rulings the concurrency contract names.
Source check: origin/main f4f14a4096, 2026-10-19, re-anchored from c88c3806a
(see the [corrections](7766-repository-retirement-corrections.md#re-anchor-at-f4f14a4096)).

## Phase 1 Lock Budget

Phase 1 takes `LOCK TABLE fact_work_items IN EXCLUSIVE MODE` (step 6). The
measured cost has three parts: how long the section runs, who it blocks, and
how a waiting `LOCK TABLE` blocks everyone behind it. Every figure below for
7d is for the DELETE form that P1 measured. 7d is now an UPDATE (a mark), so
P1' re-measures it before code lands.

### Bindings

The design form bound 7c, 7d, and the step 6 recheck to every
`(scope_id, generation_id)` pair of the scopes. It failed the 250 ms bar
(P1: design-form Rc5000 cold p99 252.2 ms, Wc5000 cold 885.9 ms). The
amended bindings:

| Statement | Design form | Amended binding |
| --- | --- | --- |
| 7c, projector rows to `superseded` | pairs for all generations | step-4 non-superseded `(scope_id, generation_id)` pairs |
| 7d, reducer rows to `superseded` (a mark) | pairs for all generations, a delete | `scope_id = ANY($scopes)` |
| Step 6 recheck of live leases, both stages | pairs for all generations (reducer stage only) | `scope_id = ANY($scopes)`, `stage IN ('projector','reducer')` |

This is valid because the retire unit is the whole scope, every generation.
`refinalize` binds a subset of generations, which is why its pair form is right
for it and wrong here.

- **7d by scope** marks the reducer rows of every generation of a scope that is
  going away. Phase 2d's cascade deletes the same rows later. The live-lease
  guard of the 7d predicate is unchanged.
- **7c by non-superseded pairs** skips projector rows of generations that were
  already `superseded`. The #7130 claim branch already supersedes those rows,
  and Ack and Heartbeat refuse them.

Both stay in phase 1.

- **7d cannot move to phase 2.** The reducer claim's supersede CTE requires
  `scope.active_generation_id = active_generation.generation_id`
  (`reducer_generation_filter_sql.go:136`), applied in the sweep's lock step
  (`:196-203`). #8205 made that step `FOR NO KEY UPDATE OF stale` in
  `work_item_id COLLATE "C"` order and kept the predicate. Step 7b nulls that pointer, and the
  reducer claim has no generation-status predicate
  (`reducer_queue_claim_query.go:9-104`), so after phase 1 commits a pending
  reducer row of the retired scope is claimable unless 7d marked it under the
  fence.
- **7c is droppable for fencing, and still stays.** `candidate_pool` excludes
  every supersedable row (`projector_queue_claim_sql.go:188-209`) and Ack and
  Heartbeat refuse. Dropping 7c would move a one-off sweep of up to 5,000
  generation-row and 5,000 work-row locks into an unrelated worker's claim
  statement, which is a hot path nobody measured. Tuned 7c costs 6.5 ms cold on
  the realistic shape (mean, n=10, rollback mode). The stalled-shape run
  (Ww5000, 32.3 ms mean) touched only 106 to 170 7c rows, not 5,000, so it does
  not measure the stalled-shape cost and P1' owes that figure.
- 7e could move to phase 2. It costs 0.5 ms, so it stays.

### Deadline and lock waits

`WriteTimeout` is `DefaultRefinalizeDrainTimeout + apiRecoveryResponseMargin`,
5 min plus 1 min (`cmd/api/main.go:27`, `:159-160`; `refinalize.go:31`). The
first amendment gave the drain 5 min, the advisory wait 5 min, and the fence 30
s. Those sum past 6 min, so a slow drain could outlive the response. Phase 1
now runs under **one deadline of 5 min 30 s** from request start, leaving 30 s
to write the response. Each wait is bounded by `min(its own bound, remaining)`:

1. Step 2, the scope-bound drain: `min(5 min, remaining)`. It waits for live
   leases of both stages, so an in-flight projector Run finishes before the
   fence starts (I0).
2. Step 3, the advisory wait: `set_config('lock_timeout', <ms>, true)` with
   `<ms> = remaining - fenceReserve`, computed at that moment and not a
   constant. `fenceReserve` is 35 s (the 30 s fence budget plus 5 s for steps 4
   to 8). If that is not positive, return 409 `lock_timeout` without taking the
   key. The advisory wait convoys nothing: a commit waiting on it holds no
   `fact_work_items` lock. It may already hold workflow-control rows
   (`HeartbeatClaim` at `ingestion.go:206` writes `workflow_claims`), so
   `ingestion.go:212` does not precede every write, but nothing before it
   touches `fact_work_items` or `ingestion_scopes`. Only this repository's
   commits queue behind the key, which is the intent.
3. Step 6, the fence: `set_config('lock_timeout','1s',true)` immediately before
   it. This is the pattern in `activation/sql.go:63`: a short wait plus a
   deferred retry. The total fence budget is `min(30 s, remaining)`.

On `55P03` at step 6: roll back, sleep a jittered 100 to 500 ms, and restart at
step 1, with every wait recomputed from the remaining deadline. When the
deadline or the fence budget runs out, the API returns 409 `blocked` with the
reason of the wait that could not finish. No marker row exists, so these are API
response reasons. The same name (`claim_fence_busy`) is also a `blocked_reason`
value, but only for 2q step B, where a marker row exists.

Why 1 s and not the old 5 minutes: PostgreSQL grants conflicting lock requests
in queue order, so a waiting `EXCLUSIVE` blocks every later `ROW EXCLUSIVE`
requester. Measured: a `LOCK TABLE` waited 6,975 ms behind a `ROW EXCLUSIVE`
holder, and an unrelated single-row `UPDATE` then waited 5,963 ms behind that
waiter (evidence note, convoy section). The convoy came from step 6 inheriting
step 3's `SET LOCAL`, not from the advisory wait. `refinalize` had the same
defect. #7852 fixed it with this shape (`rebuild/reset/claim_fence.go:16-20`,
`rebuild/reset/retry.go:24-89`), and phase 1 reuses that code.

### Reuse Of The #7852 Fence

PR 5 extracts the shipped pieces and reuses them unchanged:
- the acquisition: `lock_timeout` 1 s set just before `LOCK TABLE
  fact_work_items IN EXCLUSIVE MODE`, and the prior value restored after the
  grant (`claim_fence.go:16`, `:206`, `:212`, `:220-222`), with
  `ClaimFenceLockTimeoutError` on a timeout;
- the 100 to 500 ms jitter (`rebuild/reset/retry.go:71-74`), the 30 s budget constant
  (`claim_fence.go:20`), and the `blocked/claim_fence_busy` sentinel
  (`recovery/replay.go:16-18`);
- the retry wrapper, made generic over its result type, with refinalize kept as
  a thin wrapper. The pair-bound recheck (`claim_fence.go:223`) moves out of the
  acquisition.

Phase 1 differs from refinalize in four ways, decided here:
1. **The fence budget counts fence time only.** Phase 1 counts the time inside
   fence attempts and their backoff against the 30 s budget. The shipped
   `FenceAdmissionBudget` starts at the first attempt and counts re-reads and
   re-drains too (`claim_fence.go:23-129`), which would cap a re-drain at the
   rest of 30 s. The drain and the advisory wait stay under the 5 min 30 s
   deadline.
2. **A live lease at the recheck restarts.** It rolls back and restarts at step
   1, and the next fence attempt is admitted afresh. The shipped wrapper returns
   `InflightReducersError` at once (`rebuild/reset/retry.go:62-65`).
3. **Phase 1 maps its own outcome.** After the budget expires, the shipped
   wrapper reports any error as `claim_fence_busy` (`rebuild/reset/retry.go:59-61`). Phase 1
   reports `claim_fence_busy` only when the last failure was the fence's
   `55P03`, and keeps `projector_lease_live` or `inflight_reducers` when a
   lease wait used up the deadline.
4. **Steps 7 and 8 run under step 3's setting.** The acquisition restores the
   prior `lock_timeout`, which is the advisory-wait value step 3 set. A `55P03`
   in steps 7 or 8 is not a fence timeout; it returns 409 `lock_timeout`.

### Request budget

The 25-repository cap stays as the request cap. It does not bound the cost. The
precheck bounds it:

- After step 1, before any lock, count per repository the non-superseded
  generations and the reducer rows that match the 7d predicate. Refuse with 409
  `request_too_large` and the per-repository counts when the sum of
  non-superseded generations exceeds 5,000 or the sum of 7d-predicate rows
  exceeds 20,000.
- **The limits are candidates, not measurements.** P1 measured the design form
  on shapes with 5,000 generations and 19,996 reducer rows. P1' sets the final
  limits by measuring the tuned, mark-form phase 1 in commit mode, with the projector-stage
  recheck count in the fenced section, and the limits drop if the 250 ms bar
  fails. P1' also measures unrelated claim
  throughput across the section: at least 90% of the no-retirement baseline over
  the whole run, with the per-statement max as the second bar.
- The precheck is a snapshot. Step 4 is authoritative: if the re-read under the
  lock exceeds either bound, abort with the same 409.

## Lock-Order Rule And The Fail Fix

**Rule: advisory key, then scope row, then the `fact_work_items` relation.** A
writer that blocks on a retired scope row or on the key must take it in a
statement that references no other relation.

`Fail` breaks the rule, and phase 1 deadlocks against it. The facts, all from
source (no live run yet; `TestRetirementAndProjectorFailDoNotDeadlock` is the
RED proof):

- `failProjectorWorkQuery` is one statement (`projector_queue_sql.go:195-267`).
  The order is unchanged at `f4f14a4096`: the scope row, then the owned work rows,
  then the updates. Its newer `spared_generation` read (`:213-222`) keeps a
  spared generation active; it reads no other relation.
  A `locked_scope ... FOR NO KEY UPDATE` CTE comes first, then `owned_work ...
  FOR UPDATE OF work`, then UPDATEs of `scope_generations`, `ingestion_scopes`,
  and `fact_work_items`. The outer UPDATE target takes `ROW EXCLUSIVE` on
  `fact_work_items` at parse time, before the scope-row wait.
- It runs on the pool with no `lock_timeout` (`projector_queue.go:292`).
  `failWork` returns the error joined to the cause (`service_superseded.go:293-300`).
  There is no retry and no dead-letter row; the lease expires.
- Ack takes the scope row first in a single-table UPDATE
  (`projector_queue_ack.go:74-77`) under a lock_timeout (`:61`). The comment at
  `:65-71` lists "Fail (scope, work, generation)", which is true at the row
  level and false at the relation level.
- Phase 1 holds the scope row (step 5) before it requests `LOCK TABLE ...
  EXCLUSIVE` (step 6). The cycle closes only when `Fail` is already waiting on
  the scope row while holding `ROW EXCLUSIVE`. Then `Fail`'s `deadlock_timeout`
  (default 1 s) expires before phase 1's 1 s `lock_timeout`, because `Fail`
  started waiting first, and the detector aborts the transaction that runs the
  check. `Fail` loses every time, not only when it starts waiting first.
- No such cycle exists today. `refinalize` takes the table lock without first
  holding a scope row: before the fence it runs only the lock-free generation
  read and the drain count (`storage/postgres/recovery.go:363-371`), and since
  #7852 its `LOCK TABLE` (`rebuild/reset/claim_fence.go:212`) waits at most 1 s
  before the attempt rolls back and retries. Its FK inserts
  (`refinalize.go:70-122`, `migrations/005_fact_work_items.sql:3-4`) take
  `KEY SHARE`, which is compatible with `NO KEY UPDATE`.

Every transaction that holds a lock conflicting with `EXCLUSIVE` on
`fact_work_items` and can then block on a retired scope row or the repo key:

| Transaction | Order | Verdict |
| --- | --- | --- |
| `Fail` | `ROW EXCLUSIVE` on the relation, then the scope row | The only member. Fixed below. |
| Ack | Scope row first, single-table UPDATE (`projector_queue_ack.go:74`); since #7127 it also journals behind `pg_try_advisory_xact_lock_shared`, a try-lock that never waits (`freshness/links/journal_sql.go:30`) | Safe |
| Ingestion commit | Key (`ingestion.go:212`), scope upsert (`:224`), work enqueue later | Safe. `HeartbeatClaim` (`:206`) writes workflow-control tables before the key and is not part of the cycle. |
| Activation Finalize | Scope row first, 1 s lock_timeout (`activation/sql.go:63-72`); producer-settle reopens inside it | Safe |
| Admin reopen | Scope row first, 5 s lock_timeout (`admin/store/reopen.go:196-211`, #7734 fence) | Safe |
| Duplicate-generation reproject (#7978) | Scope row first, in its own statement (`generation_freshness_reproject.go:23-25`, run at `:165`), then the projector work row and the generation | Safe |
| Acceptance gate and writer | Key first (`storage/postgres/repo_dependency_acceptance_gate.go:72`, `shared_intent_acceptance_writer.go:84-88`) | Safe |
| Backfill (per commit, pool, all-repos) | Key first (`ingestion_backfill_seam.go:286`, `backfill/fanin.go:331`, `backfill/engine.go:340`) | Safe |
| Fanout, reducer claim (its #8205 lock step locks `fact_work_items` rows only, `reducer_generation_filter_sql.go:196-203`), ack, heartbeat, batch acks, value-flow ack, the #8169 service-catalog re-trigger (`FOR NO KEY UPDATE OF catalog`, scopes read unlocked), activation for claim | Lock `fact_work_items` rows, never block on a scope row (`fanout.go:32-54` reads scopes unlocked; `relationship_schema.go:270-296`) | Safe |
| Package-manifest backfill | Locks scope rows `FOR NO KEY UPDATE` under a 5 s `lock_timeout`, one transaction per page (`consumption/statements.go:23-25`, taken at `consumption/rebuild.go:200-204`), never references `fact_work_items` | Safe |
| Vulnerability suppression; generation retention | Locks a non-git scope; `SKIP LOCKED` | Safe |

**The fix is on `Fail`, not on phase 1**, in its own small PR (PR 4a) that
lands before PR 5. It is a fix to projector code, not retirement code. `Fail`
becomes a transaction:
1. `set_config('lock_timeout', <ackLockTimeoutSetting>, true)`, as Ack does;
2. statement 1: `SELECT scope_id FROM ingestion_scopes WHERE scope_id = $1 FOR NO KEY UPDATE`;
3. statement 2: the existing chain without the `locked_scope` CTE.

On `55P03`, `Fail` returns a deferred error and the service retries it the way
it retries Ack, bounded by `DefaultAckWaitMaxRetries`
(`service_superseded.go:306`). A phase 1 holding the scope row delays the
dead-letter instead of losing it. The same PR corrects the comment at
`projector_queue_ack.go:65-73` to state the relation-level rule.

Tests (real Postgres, `reducer-contention` gate):
- `TestProjectorFailScopeLockPrecedesWorkTableLock`: after statement 1,
  `pg_locks` shows the scope row lock and no lock on `fact_work_items` for that
  backend.
- `TestRetirementAndProjectorFailDoNotDeadlock`: both orders, no `40P01`. The
  dead-letter row and `scope_generations.status='failed'` land after phase 1
  commits, or `Fail` is refused as superseded.
