# Repository Retirement: Concurrency Contract (#7766)

Issue: #7766
Companions: [Repository Retirement](7766-repository-retirement.md) (the
design and the two deliverables), [Reducer Reopen And Replay Writers](7766-repository-retirement-reducer-writers.md),
[Runner](7766-repository-retirement-runner.md),
[Proof And Rollout](7766-repository-retirement-proof-and-rollout.md), and the
[evidence note](../evidence/7766-retirement-prove-first-results.md).

This file holds the intent-delete barrier that replaced the shared-projection
horizon, the invariants that make the phase 2 order safe, the folded commit
gate, and the re-admission interleavings. The phase 1 lock budget, the reuse of
the #7852 fence, and the lock-order rule are in
[Phase 1 Locking](7766-repository-retirement-phase1-locking.md). Binding inputs: the
[shared-contract arbiter ruling](https://github.com/eshu-hq/eshu/issues/7766#issuecomment-6073882598),
the [arbiter ruling on the prove-first results](https://github.com/eshu-hq/eshu/issues/7766#issuecomment-6082885964),
the arbiter ruling, round 3 ([posted on #7766](https://github.com/eshu-hq/eshu/issues/7766#issuecomment-6084044833)),
and the arbiter ruling, round 4 ([posted on #7766](https://github.com/eshu-hq/eshu/issues/7766#issuecomment-6085855231)). Measurements are in the
[evidence note](../evidence/7766-retirement-prove-first-results.md) and summarized in
[Prove-First Results](7766-repository-retirement-proof-and-rollout.md#prove-first-results).

Source check: origin/main f4f14a4096, 2026-10-19, re-anchored from
c88c3806a (see the [corrections](7766-repository-retirement-corrections.md#re-anchor-at-f4f14a4096)). The shared-worker citations in the barrier and
invariant sections were re-read at f4f14a4096: #7853 (d3cd68415, 9b4274f64)
changed `worker/process.go` and `worker/selection.go` and added
`worker/absent_acceptance.go`.

## Shared-Projection Barrier

The first draft recorded `max(lease_expires_at)` of live shared-projection
leases as `shared_lease_horizon` and waited for `now()` to pass it. P9H showed
that this is unsound in code, not only in a repro:

- `claimPartitionLeaseSQL`'s `ON CONFLICT` rewrites `lease_expires_at` whenever
  `lease_owner = $4` (`storage/postgres/shared_intents.go:181-187`), and the
  heartbeat calls it every TTL/2
  (`reducer/intents/shared/worker/heartbeat.go:32-38`, `:60`, `:78-80`).
- The lease is held from claim (`worker/process.go:150`) through the edge write
  (`:246`) and `MarkIntentsCompleted` (`:258`, call at `:353`) to the deferred
  release (`:188`).
- Measured: with the horizon taken at t=300 ms, `now() > horizon` was true at
  t=1.5 s, the lease was still renewed 3.0 s past the horizon at t=3.5 s, and
  the worker stayed inside `RetractEdges` until t=4.0 s.
- code_calls shares the table and the heartbeat shape
  (`code/call/projection/lease.go:58-66`, `code/call/projection/runner.go:271`; TTL 60 s at
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

- **2b, intent delete and barrier.** First, chunked, the three relationship
  tables' rows with `source_repo_id = $repo` (`relationship_evidence_facts`,
  `relationship_candidates`, `resolved_relationships`), so no evidence sourced
  from the repository is left to resolve. Then, chunked (10,000 rows),
  `DELETE FROM shared_projection_intents WHERE repository_id = $repo` (index
  `shared_projection_intents_repo_run_idx`, `repository_id` leading) and
  `WHERE scope_id = ANY($scopes)` (index `..._acceptance_lookup_idx`,
  `scope_id` leading). The same for `shared_projection_unroutable_intents`.
  Assert 0 rows remain by both predicates, and commit. Then, in a later
  statement, `UPDATE repository_retirements SET intent_barrier_at =
  clock_timestamp()`. The barrier must postdate the delete's commit. A re-run of
  2b that deletes rows sets it again the same way; it only ever moves later, so
  the re-run 2b' waits for every lease claimed before the new delete.
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
| Shared projection worker (every shared domain) | `worker/process.go:150`, heartbeat `worker/heartbeat.go:78` | `repo_graph_writer` |
| code_calls | `code/call/projection/runner.go:271`, heartbeat `lease.go:58` | `repo_graph_writer` |
| repo_dependency | `repo_dependency_projection_runner.go:183`, heartbeat `repo_dependency_projection_telemetry.go:112` | `repo_graph_writer` |
| Graph orphan sweep | `maintenance/orphan/runner.go:171` | `maintenance_lease` |
| Collector evidence summary | `maintenance/evidence/maintainer.go:160` | `maintenance_lease` |
| Code value-flow stale cleanup | `code/value/cleanup/runner.go:215` | `maintenance_lease` |
| Supply-chain impact winners | `supplychain/core/winners_maintainer.go:120` | `maintenance_lease` |

A domain that does not hold the lease while it writes cannot be covered by 2b'
at all, so the restriction removes nothing 2b' could have protected.

## Invariants

- **I0 (Deliverable 1).** At phase 1 commit, no projector or reducer row of the
  scopes holds a live lease, and every projector row of a generation that was
  non-superseded at step 4 is terminal. Pending or retrying rows of generations
  that were already superseded are not touched by 7c; the #7130 claim branch
  supersedes them, and 2a rechecks.
  From commit on, no projector Run writes the repository's graph or content,
  except a Run that outlived its lease, which is outside the lease contract for
  every lease in the system today. The #7130 claim branch, the Heartbeat
  refusal, and the Ack refusal are backstops, not the fence. The step 2 wait and
  the step 6 recheck under `EXCLUSIVE` carry it. Without the wait, a live Run
  survives phase 1 until the next heartbeat (lease/3 capped at 1 min,
  `cmd/projector/runtime_wiring.go:124-134`) cancels its context, and a Run
  cancelled between its canonical write and its content write leaves a frozen
  half-Run graph: on Neo4j the atomic path writes all node phases in one
  transaction, then a deferred package-registry edge transaction, and the full
  refresh retract deletes every `projector/canonical` entity node whose
  `generation_id` differs from the new one and every File absent from the new
  path list (`canonical_node_writer.go:197-214`,
  `storage/cypher/canonical/node/templates.go:15-18`, `:55-57`), dropping other repositories' edges into those nodes. Lease defaults:
  1 min for the projector service (`cmd/projector/runtime_wiring.go:72`), 5 min
  for the ingester's in-process projector (`cmd/ingester/wiring.go:304`), and 1
  min for bootstrap-index (`cmd/bootstrap-index/wiring.go:130`).
- **I1.** Every writer of shared-domain graph edges holds a
  `shared_projection_partition_leases` row from before it reads intents until
  after its last graph write and `MarkIntentsCompleted`. Confirmed for the
  shared worker (`worker/process.go:150-258`, release `:188`), code_calls
  (claim `code/call/projection/runner.go:271`, mark `:484`, release `:305`), and,
  by source read, repo_dependency: the lease is claimed at
  `repo_dependency_projection_runner.go:183` before `selectAcceptanceUnitWork`;
  the heartbeat renews it through `ClaimPartitionLease`
  (`repo_dependency_projection_telemetry.go:112`); the retract
  (`repo_dependency_projection_runner.go:506`), the write (`:538`), and
  `MarkIntentsCompleted` (`repo_dependency_projection_acceptance_cycle.go:165`)
  all run inside `processAcceptanceUnit`, which the gate callback calls
  (`repo_dependency_projection_runner.go:273`); the lease is released only after
  the gate returns, or held for the TTL on quarantine. The #7853 drains add
  `MarkIntentsCompleted` calls but no graph writes, and each runs inside the same
  lease.
  That is a static read. No live test exercises repo_dependency under the
  barrier.
- **I2.** From 2q's final fenced pass on, no reducer row of the retired scopes
  is claimable and none holds a live lease. Both are asserted under `EXCLUSIVE`.
  Rows are marked `superseded`, never deleted, so `ON CONFLICT (work_item_id) DO
  NOTHING` is inert for every id that has ever existed. Every reopen and replay
  writer on origin/main touches `status='succeeded'` rows only
  (`reducer_queue_replay.go:50-111`, `admin/store/reopen.go:75-85`, the
  `ReopenSucceeded` loops in `ingestion_reopen_*.go`,
  `backfill/targeted/write.go`,
  `ingestion_producer_activation.go`) or requires an active generation
  (`scope/completion/fanout.go:32-54`), so each finds none. The `failed` and
  `dead_letter` replays (`projector_queue_sql.go:391-427`,
  `query/admin/store/queries.go:26-32`) find none either: 7d and 2q have
  marked those statuses `superseded`. The one open path is
  an INSERT with a fresh `work_item_id`
  (`ReplayWorkloadMaterializationOutcome` at `reducer_queue_replay.go:280`,
  `ReplayCrossplaneSatisfiedByMaterialization` at `:376`, a projector that
  outlived its lease). It is bounded by 2d: `fact_work_items.generation_id` is
  `NOT NULL REFERENCES scope_generations ON DELETE CASCADE`
  (`migrations/005_fact_work_items.sql:4`), so an insert that names a deleted
  generation fails `23503`. A handler that claimed a cascaded row before 2d
  loses its next heartbeat (a heartbeat on a vanished row affects 0 rows and
  returns `ErrReducerClaimRejected`, `storage/postgres/reducer_queue.go:391-415`)
  and is cancelled within `LeaseDuration/2` plus one in-flight statement
  (`reducer/service_heartbeat.go:98-124`, `cmd/reducer/main.go:426`, `main_helpers.go:49`); 2d' waits
  one reducer lease for that. 2z catches what lands in between. **The guarantee is
  the 2q recheck under `EXCLUSIVE`, not phase 1.** Phase 1's recheck cannot
  carry it, because a pre-phase-1 projector Run or a maintenance reopen can
  insert or reopen rows after phase 1 commits.
- **I3.** No acceptance row of the retired scopes is deleted before 2b's delete
  has committed. The only retirement acceptance delete is 2d's cascade
  (`migrations/011:2-5`), which runs after. Generation retention's cascade also
  deletes acceptance rows of superseded generations. That is harmless: the
  retention transaction deletes the generation's intents first
  (`generation_retention.go:405-406`), so it creates no orphan.
- **I4.** Other repositories' work can still write Postgres rows sourced from
  the retired repository: a per-commit backfill writes evidence sourced from it
  under the onboarding repository's generation, and candidates are stored
  without an ownership filter. It cannot turn them into `repo_dependency` edges
  for the retired repository: the resolver keeps only edges whose source is one
  of its own scope's repositories (`partitionResolvedOwnership`,
  `reducer/crossrepo/cross_repo_resolution_ownership.go:71-92`, on whenever the
  generation has a git `repository` fact; wired at `cmd/reducer/main.go:312`).
  From phase 1 on, the backfill source filter stops new evidence (writer table
  in the main design); 2b deletes the rows already written; 2z counts them by
  `source_repo_id`, and intents by `repository_id`. An edge written for the
  retired repository by a pre-barrier cycle is harmless: 2c retracts it, and that cycle's `MarkIntentsCompleted` matches 0
  rows for the intents 2b deleted. Since #7853 the call returns that count and
  the worker logs that it marked fewer intents than requested. That WARN is
  expected during 2b and 2b'.

## Why 2b Goes First

Intents and acceptance rows are written together
(`shared_intent_acceptance_writer.go:113-132`, one transaction when a beginner
exists, `:78-95`). An intent without an acceptance row can therefore come only
from a cascade delete or a torn non-transactional write. Before #7853 all three
selectors skipped such an intent forever. At f4f14a4096 each still drops it in
its acceptance filter (`reducer/intents/shared/worker/selection.go:153-156`,
`code/call/projection/selection.go:253-260`,
`repo_dependency_projection_runner.go:404-411`), then marks the subset whose
`scope_generations` row is absent completed, reason
`acceptance_absent_generation_gone` (`worker/absent_acceptance.go:91-138`). A
torn write whose generation row exists is still skipped, counted, and WARNed.
code_calls and repo_dependency error at the 10,000 cap only when a cap drain
finds nothing new (`code/call/projection/selection.go:383-415`,
`repo_dependency_projection_runner.go:439-457`), and the indexed shared path
drains on its cap round (`worker/selection.go:321-366`). `MarkIntentsCompleted`
returns rows matched (`shared_intents.go:265-287`).

#7853 is a backstop, not part of this design's guarantee. Acceptance rows of
the scopes go only through 2d's cascade, after 2b, so retirement creates
neither kind of orphan. Residue that lands after 2b and whose generation 2d
deletes is drained after 2d with no graph write, and 2z still counts it.

Measured at `3b03f018e`, before #7853 (P9, real Postgres): an intent whose acceptance row or generation was
deleted before selection stayed unprocessed and uncompleted for five cycles,
and 10,100 such intents plus one healthy intent made a cycle take 2.1 s and
process 0.

With 2b first and I3, retirement creates no orphans. 2q runs before 2b so no
reducer handler can emit shared intents for the scopes after 2b's delete: 2q
ends only when no reducer row of the scopes is claimable or leased. Other
scopes can still write relationship rows sourced from the repository; I4 names
their fence. The
zero-count assertion after 2b is the #7766 guard. A drain for orphans that
already exist is a separate pre-existing bug (Out Of Scope in the corrections
file).

## Commit Gate Folded Into The Scope Upsert

A separate lookup statement failed the 1% bar at facts=1 (P2: median +3.04% at
0 marker rows, +2.25% at 10,000). Folding the gate into the scope upsert
measured +0.46% and -0.09%. The design uses the folded shape:

```sql
WITH gate AS (
  SELECT state, retirement_id FROM repository_retirements
  WHERE repo_id = $7 AND readmitted_at IS NULL LIMIT 1)
INSERT INTO ingestion_scopes (...)
SELECT ... WHERE NOT EXISTS (SELECT 1 FROM gate WHERE state <> 'complete')
ON CONFLICT (...) DO UPDATE SET <arm unchanged>
RETURNING (SELECT state FROM gate), (SELECT retirement_id FROM gate)
```

- **Zero rows returned** is the `repository_retiring` refusal, on the
  finalized-skip path in the design's writer table.
- **`state = 'complete'`** in the returned value triggers the `readmitted_at`
  stamp by the returned `retirement_id`: one extra statement, on the rare
  re-admission.
- **Invariant:** the gate and the upsert share one statement snapshot, taken
  under the shared advisory lock. Phase 1 holds that key exclusively until its
  commit, so a marker either committed before the gate reads or phase 1 is still
  waiting. There is no check-then-write window.
- **The `ON CONFLICT` arm stays byte-identical** to `ingestion_queries.go`
  (the status CASE, bound at `:165`). A test derives the expected prefix from
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
prior-generation probe (`projector_queue_claim_pool_sql.go:255-260`) returns false,
which makes this a first generation.

Interleavings are linearised by the advisory key:
- a commit that holds the shared lock first lands, and phase 1 reads its
  generation at step 4 and supersedes it at 7a;
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
