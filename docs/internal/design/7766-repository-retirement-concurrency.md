# Repository Retirement: Concurrency Contract (#7766)

Issue: #7766
Companions: [Repository Retirement](7766-repository-retirement.md) (the
design and the two deliverables), [Reducer Reopen And Replay Writers](7766-repository-retirement-reducer-writers.md),
[Runner](7766-repository-retirement-runner.md),
[Proof And Rollout](7766-repository-retirement-proof-and-rollout.md), and the
[evidence note](../evidence/7766-retirement-prove-first.md).

This file holds the phase 1 lock budget and deadline, the lock-order rule, the
intent-delete barrier that replaced the shared-projection horizon, the
invariants that make the phase 2 order safe, the folded commit gate, and the
re-admission interleavings. Binding inputs: the
[shared-contract arbiter ruling](https://github.com/eshu-hq/eshu/issues/7766#issuecomment-6073882598),
the [arbiter ruling on the prove-first results](https://github.com/eshu-hq/eshu/issues/7766#issuecomment-6082885964),
the arbiter ruling, round 3 ([posted on #7766](https://github.com/eshu-hq/eshu/issues/7766#issuecomment-6084044833)),
and the arbiter ruling, round 4 ([posted on #7766](https://github.com/eshu-hq/eshu/issues/7766#issuecomment-6085855231)). Measurements are in the
[evidence note](../evidence/7766-retirement-prove-first.md) and summarized in
[Prove-First Results](7766-repository-retirement-proof-and-rollout.md#prove-first-results).

Source check: origin/main c88c3806a, 2026-10-09. The shared-worker citations in
the barrier and invariant sections were re-read at that SHA (#7724 restructured
`process.go` and `selection.go`).

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
  (`reducer_generation_filter_sql.go:180`). Step 7b nulls that pointer, and the
  reducer claim has no generation-status predicate
  (`reducer_queue_claim_query.go:9-104`), so after phase 1 commits a pending
  reducer row of the retired scope is claimable unless 7d marked it under the
  fence.
- **7c is droppable for fencing, and still stays.** `candidate_pool` excludes
  every supersedable row (`projector_queue_claim_sql.go:158-179`) and Ack and
  Heartbeat refuse. Dropping 7c would move a one-off sweep of up to 5,000
  generation-row and 5,000 work-row locks into an unrelated worker's claim
  statement, which is a hot path nobody measured. Tuned 7c costs 6.5 ms cold on
  the realistic shape (mean, n=10, rollback mode). The stalled-shape run
  (Ww5000, 32.3 ms mean) touched only 106 to 170 7c rows, not 5,000, so it does
  not measure the stalled-shape cost and P1' owes that figure.
- 7e could move to phase 2. It costs 0.5 ms, so it stays.

### Deadline and lock waits

`WriteTimeout` is `DefaultRefinalizeDrainTimeout + apiRecoveryResponseMargin`,
5 min plus 1 min (`cmd/api/main.go:26`, `:130-131`; `refinalize.go:31`). The
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
   (`HeartbeatClaim` at `ingestion.go:213` writes `workflow_claims`), so
   `ingestion.go:219` does not precede every write, but nothing before it
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
step 3's `SET LOCAL`, not from the advisory wait. `refinalize` has the same
defect (see Out Of Scope in the
[corrections file](7766-repository-retirement-corrections.md#out-of-scope-pre-existing-issues)).

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

- `failProjectorWorkQuery` is one statement (`projector_queue_sql.go:176-231`).
  A `locked_scope ... FOR NO KEY UPDATE` CTE comes first, then `owned_work ...
  FOR UPDATE OF work`, then UPDATEs of `scope_generations`, `ingestion_scopes`,
  and `fact_work_items`. The outer UPDATE target takes `ROW EXCLUSIVE` on
  `fact_work_items` at parse time, before the scope-row wait.
- It runs on the pool with no `lock_timeout` (`projector_queue.go:400`).
  `failWork` returns the error joined to the cause (`service_superseded.go:293-300`).
  There is no retry and no dead-letter row; the lease expires.
- Ack takes the scope row first in a single-table UPDATE
  (`projector_queue.go:221-224`) under a lock_timeout (`:210`). The comment at
  `:214-220` lists "Fail (scope, work, generation)", which is true at the row
  level and false at the relation level.
- Phase 1 holds the scope row (step 5) before it requests `LOCK TABLE ...
  EXCLUSIVE` (step 6). The cycle closes only when `Fail` is already waiting on
  the scope row while holding `ROW EXCLUSIVE`. Then `Fail`'s `deadlock_timeout`
  (default 1 s) expires before phase 1's 1 s `lock_timeout`, because `Fail`
  started waiting first, and the detector aborts the transaction that runs the
  check. `Fail` loses every time, not only when it starts waiting first.
- No such cycle exists today. `refinalize` takes the table lock without first
  holding a scope row (`refinalize.go:65-66`, `:381`), and its FK inserts take
  `KEY SHARE`, which is compatible with `NO KEY UPDATE`.

Every transaction that holds a lock conflicting with `EXCLUSIVE` on
`fact_work_items` and can then block on a retired scope row or the repo key:

| Transaction | Order | Verdict |
| --- | --- | --- |
| `Fail` | `ROW EXCLUSIVE` on the relation, then the scope row | The only member. Fixed below. |
| Ack | Scope row first, single-table UPDATE (`projector_queue.go:221`) | Safe |
| Ingestion commit | Key (`ingestion.go:219`), scope upsert (`:231`), work enqueue later | Safe. `HeartbeatClaim` (`:213`) writes workflow-control tables before the key and is not part of the cycle. |
| Activation Finalize | Scope row first, 1 s lock_timeout (`activation/sql.go:63-72`); producer-settle reopens inside it | Safe |
| Admin reopen | Scope row first, 5 s lock_timeout (`admin/store/reopen.go:196-211`, #7734 fence) | Safe |
| Acceptance gate and writer | Key first (`repo_dependency_acceptance_gate.go:72`, `shared_intent_acceptance_writer.go:83-87`) | Safe |
| Backfill (per commit, pool, all-repos) | Key first (`ingestion_backfill_per_commit.go:112`, `ingestion_backfill_pool.go:401`, `ingestion_backfill.go:329`) | Safe |
| Fanout, reducer claim, ack, heartbeat, batch acks, value-flow ack, activation for claim | Lock `fact_work_items` rows, never block on a scope row (`fanout.go:32-54` reads scopes unlocked; `relationship_schema.go:270-296`) | Safe |
| Package-manifest backfill | Locks scope rows `FOR UPDATE` (`package_manifest_consumption_backfill.go:68-70`), never references `fact_work_items` | Safe |
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
`projector_queue.go:214-220` to state the relation-level rule.

Tests (real Postgres, `reducer-contention` gate):
- `TestProjectorFailScopeLockPrecedesWorkTableLock`: after statement 1,
  `pg_locks` shows the scope row lock and no lock on `fact_work_items` for that
  backend.
- `TestRetirementAndProjectorFailDoNotDeadlock`: both orders, no `40P01`. The
  dead-letter row and `scope_generations.status='failed'` land after phase 1
  commits, or `Fail` is refused as superseded.

## Shared-Projection Barrier

The first draft recorded `max(lease_expires_at)` of live shared-projection
leases as `shared_lease_horizon` and waited for `now()` to pass it. P9H showed
that this is unsound in code, not only in a repro:

- `claimPartitionLeaseSQL`'s `ON CONFLICT` rewrites `lease_expires_at` whenever
  `lease_owner = $4` (`storage/postgres/shared_intents.go:180-186`), and the
  heartbeat calls it every TTL/2
  (`reducer/intents/shared/worker/heartbeat.go:32-38`, `:60`, `:78-80`).
- The lease is held from claim (`worker/process.go:137`) through the edge write
  (`:238`) and `MarkIntentsCompleted` (`:250`, call at `:310`) to the deferred
  release (`:175`).
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

### Migration and claim SQL (Deliverable 2, PR 6a)

The `claimed_at` column and the claim SQL change ship in PR 6a, not in the
marker migration, because only the barrier reads them. Use the next free
migration number at that time.

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

### 2b and 2b'

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
    AND projection_domain = ANY($lease_domains)
    AND (claimed_at IS NULL OR claimed_at <= $intent_barrier_at)
  ```

  The query is fleet-wide across the listed domains because nothing records
  which partition holds this repository's intents. The readback names the
  oldest blocking `(projection_domain, partition_id, lease_owner, claimed_at)`,
  so an operator sees which partition to look at.

### The lease-domain list is derived from code

`$lease_domains` is the set of domains whose writers hold a
`shared_projection_partition_leases` row while writing repo-keyed graph edges.
Without the restriction, a maintenance lease that never writes this
repository's edges could hold 2b'. A hand-kept list drifts, so a test derives
it. `TestRetirementLeaseDomainsMatchLeaseWriters` enumerates every non-test
caller of `ClaimPartitionLease` from source, extracts the `projection_domain`
argument, and requires each domain to be classified in
`reducer/retirement/plan.go` as `repo_graph_writer` or `maintenance_lease`
(with a reason). A new lease writer fails the test until someone classifies it.
At this check the writers are:

| Writer | Claim site | Class |
| --- | --- | --- |
| Shared projection worker (every shared domain) | `worker/process.go:137`, heartbeat `worker/heartbeat.go:78` | `repo_graph_writer` |
| code_calls | `code/call/projection/runner.go:267`, heartbeat `lease.go:58` | `repo_graph_writer` |
| repo_dependency | `repo_dependency_projection_runner.go:178`, heartbeat `repo_dependency_projection_telemetry.go:109` | `repo_graph_writer` |
| Graph orphan sweep | `maintenance/orphan/runner.go:161` | `maintenance_lease` |
| Collector evidence summary | `maintenance/evidence/maintainer.go:160` | `maintenance_lease` |
| Code value-flow stale cleanup | `code/value/cleanup/runner.go:215` | `maintenance_lease` |
| Supply-chain impact winners | `supplychain/core/winners_maintainer.go:120` | `maintenance_lease` |

A domain that does not hold the lease while it writes cannot be covered by 2b'
at all, so the restriction removes nothing 2b' could have protected.

## Invariants

- **I0 (Deliverable 1).** At phase 1 commit, no projector or reducer row of the
  scopes holds a live lease, and every projector row of the scopes is terminal.
  From commit on, no projector Run writes the repository's graph or content,
  except a Run that outlived its lease, which is outside the lease contract for
  every lease in the system today. The #7130 claim branch, the Heartbeat
  refusal, and the Ack refusal are backstops, not the fence. The step 2 wait and
  the step 6 recheck under `EXCLUSIVE` carry it. Without the wait, a live Run
  survives phase 1 until the next heartbeat (lease/3 capped at 1 min,
  `cmd/projector/runtime_wiring.go:121-131`) cancels its context, and a Run
  cancelled between its canonical write and its content write leaves a frozen
  half-Run graph: on Neo4j each node phase is one transaction, and the full
  refresh retract deletes every `projector/canonical` entity node whose
  `generation_id` differs from the new one and every File absent from the new
  path list (`canonical_node_writer.go:149-166`, `canonical_node_cypher.go:15-18`,
  `:55-57`), dropping other repositories' edges into those nodes. Lease defaults:
  1 min for the projector service (`cmd/projector/runtime_wiring.go:69`), 5 min
  for the ingester's in-process projector (`cmd/ingester/wiring.go:276`), and 1
  min for bootstrap-index (`cmd/bootstrap-index/wiring.go:115`).
- **I1.** Every writer of shared-domain graph edges holds a
  `shared_projection_partition_leases` row from before it reads intents until
  after its last graph write and `MarkIntentsCompleted`. Confirmed for the
  shared worker (`worker/process.go:137-250`, release `:175`), code_calls, and, by source read,
  repo_dependency: the lease is claimed at `repo_dependency_projection_runner.go:178`
  before `selectAcceptanceUnitWork`; the heartbeat renews it through
  `ClaimPartitionLease` (`repo_dependency_projection_telemetry.go:109`); the
  retract (`:420`), the write (`:452`), and `MarkIntentsCompleted`
  (`repo_dependency_projection_acceptance_cycle.go:128`) all run inside
  `processAcceptanceUnit`, which the gate callback calls (`:258`); the lease is
  released only after the gate returns, or held for the TTL on quarantine.
  That is a static read. No live test exercises repo_dependency under the
  barrier.
- **I2.** From 2q's final fenced pass on, no reducer row of the retired scopes
  is claimable and none holds a live lease. Both are asserted under `EXCLUSIVE`.
  Rows are marked `superseded`, never deleted, so `ON CONFLICT (work_item_id) DO
  NOTHING` is inert for every id that has ever existed. Every reopen and replay
  writer on origin/main touches `status='succeeded'` rows only
  (`reducer_queue_replay.go:49-110`, `admin/store/reopen.go:75-85`, the
  `ReopenSucceeded` loops in `ingestion_reopen_*.go`,
  `ingestion_targeted_maintenance_write.go`,
  `ingestion_producer_activation.go`) or requires an active generation
  (`scope/completion/fanout.go:32-54`), so each finds none. The one open path is
  an INSERT with a fresh `work_item_id`
  (`ReplayWorkloadMaterializationOutcome` at `reducer_queue_replay.go:279`,
  `ReplayCrossplaneSatisfiedByMaterialization` at `:375`, a projector that
  outlived its lease). It is bounded by 2d: `fact_work_items.generation_id` is
  `NOT NULL REFERENCES scope_generations ON DELETE CASCADE`
  (`migrations/005_fact_work_items.sql:4`), so an insert that names a deleted
  generation fails `23503`. A handler that claimed a cascaded row before 2d
  loses its next heartbeat (a heartbeat on a vanished row affects 0 rows and
  returns `ErrReducerClaimRejected`, `storage/postgres/reducer_queue.go:385-409`)
  and is cancelled within `LeaseDuration/2` plus one in-flight statement
  (`reducer/service_heartbeat.go:97-123`, `cmd/reducer/main.go:404`); 2d' waits
  one reducer lease for that. 2z catches what lands in between. **The guarantee is
  the 2q recheck under `EXCLUSIVE`, not phase 1.** Phase 1's recheck cannot
  carry it, because a pre-phase-1 projector Run or a maintenance reopen can
  insert or reopen rows after phase 1 commits.
- **I3.** No acceptance row of the retired scopes is deleted before 2b's delete
  has committed. The only retirement acceptance delete is 2d's cascade
  (`migrations/011:2-5`), which runs after. Generation retention's cascade also
  deletes acceptance rows of superseded generations. That is harmless: the
  retention transaction deletes the generation's intents first
  (`generation_retention.go:441-442`), so it creates no orphan.
- **I4.** An edge written for the retired repository by a pre-barrier cycle is
  harmless: 2c retracts it, and that cycle's `MarkIntentsCompleted` affects 0
  rows.

## Why 2b Goes First

Intents and acceptance rows are written together
(`shared_intent_acceptance_writer.go:111-121`, one transaction when a beginner
exists, `:78-93`). An intent without an acceptance row can therefore come only
from a cascade delete or a torn non-transactional write. All three selectors
skip such an intent forever (`reducer/intents/shared/worker/selection.go:139-147`,
`code/call/projection/selection.go:203-206`,
`repo_dependency_projection_runner.go:348-350`, which errors at its 10,000 cap
at `:366-370`). The indexed shared path returns an empty batch at the 10,000
cap with no error (`selection.go:233-238`, `:322-339`), and `MarkIntentsCompleted` ignores
rows affected (`shared_intents.go:265-276`).

Measured (P9, real Postgres): an intent whose acceptance row or generation was
deleted before selection stayed unprocessed and uncompleted for five cycles,
and 10,100 such intents plus one healthy intent made a cycle take 2.1 s and
process 0.

With 2b first and I3, retirement creates no orphans. 2q runs before 2b so no
reducer handler can emit shared intents for the scopes after 2b's delete: 2q
ends only when no reducer row of the scopes is claimable or leased. The
zero-count assertion after 2b is the #7766 guard. A drain for orphans that
already exist is a separate pre-existing bug (Out Of Scope in the corrections
file).

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
- **Bar (P2'):** median delta at most 1% at facts=1 and facts=400 with 10,000
  marker rows, at least 6 runs each, re-measured on the real PR 3 code. No
  commit-path benchmark exists on main, so PR 3 adds one.

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
