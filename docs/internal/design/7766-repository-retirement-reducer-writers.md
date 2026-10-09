# Repository Retirement: Reducer Reopen And Replay Writers (#7766)

Issue: #7766
Companions: [Repository Retirement](7766-repository-retirement.md) (the design),
[Concurrency Contract](7766-repository-retirement-concurrency.md) (I2 and the
lock-order rule), [Runner](7766-repository-retirement-runner.md) (phase 2), and
[Proof And Rollout](7766-repository-retirement-proof-and-rollout.md).

Binding input: the arbiter ruling, round 3 ([posted on #7766](https://github.com/eshu-hq/eshu/issues/7766#issuecomment-6084044833)), section F1.
Source check: origin/main 3b03f018e, 2026-10-09.

Phase 1 fences claims for the reducer rows that exist at commit. It does not
stop the paths that create or reopen reducer rows afterwards. This file lists
those paths, the decision that closes them, and the tests that prove it.

## The Hazard

Reducer work for a retired scope is re-created after phase 1. The premises, all
from source:

- Enqueue is `INSERT ... ON CONFLICT (work_item_id) DO NOTHING` with no
  generation or marker guard (`reducer_queue.go:25-35`), run on the pool in
  autocommit (`:321`).
- The projector enqueues reducer intents inside `Run`, before Ack
  (`projector/runtime/projection.go:227`). Ack is a separate transaction
  (`projector_queue.go:199-226`), so an Ack refusal does not roll the enqueue
  back.
- A pending reducer row of a scope whose `active_generation_id` is NULL is
  claimable. The supersede CTE needs `scope.active_generation_id =
  active_generation.generation_id` (`reducer_generation_filter_sql.go:180`), and
  `claimReducerWorkQuery` has no generation-status predicate
  (`reducer_queue_claim_query.go:9-104`). 7b nulls the pointer and 7a
  supersedes the generations; neither hides the row.
- Deferred maintenance reopens succeeded rows in its own transaction with no
  repo lock (`ingestion_reopen_deployment_mapping.go:70-143`). The replay floor
  falls back to the latest generation of any status when the pointer is NULL
  (`ingestion_reopen_correlation.go:104-121`), and the bound excludes only
  `work_generation.status <> 'failed'`, not `superseded` (`:149-154`). After
  7a and 7b the floor is the newest superseded generation, and its succeeded
  rows reopen on every drain.
- Every reopen composer touches `status = 'succeeded'` only, and none checks
  generation status (see the table below). `ReplayDomain` has no production
  caller on origin/main.
- A live projector keeps enqueueing for at most one heartbeat interval after
  phase 1: Heartbeat refuses a superseded generation
  (`projector_queue_scan.go:414`), the service cancels the Run context on
  heartbeat failure (`projector/service_heartbeat.go:41-66`), and the interval
  is lease/3 (`cmd/projector/runtime_wiring.go:121-125`). With heartbeats off it
  keeps going for the whole Run. The path is bounded, not closed.

## Decision

1. **Quiesce the reducer queue under the same fence as phase 1.** 2q (below)
   marks every reducer row of the scopes `superseded`, including `succeeded`
   rows, under the bounded `EXCLUSIVE` fence. Unfenced marking cannot make the
   "0 marked and 0 live" recheck atomic against a concurrent claim or enqueue.
2. **Phase 1's 7d is a mark, not a delete.** Deleting a row frees its id and
   re-opens the enqueue path. Marking keeps `DO NOTHING` inert for every id that
   has ever existed from phase 1 on. The design never deletes a reducer row
   before 2d's cascade.
3. **No marker gate on enqueue or reopen.** Both are hot paths, nothing has
   measured a gate there, and a gate on the listings still leaves the fresh-id
   path unless the INSERT itself is gated.

## The 7d Statement

```sql
UPDATE fact_work_items AS stale
SET status = 'superseded',
    container_image_identity_v2_authorized_status = 'superseded',
    container_image_identity_v3_authorized_status =
        CASE WHEN stale.container_image_identity_v3_required THEN 'superseded' ELSE '' END,
    lease_owner = NULL, claim_until = NULL, visible_at = NULL, next_attempt_at = NULL,
    updated_at = $now,
    failure_class = 'repository_retired',
    failure_message = 'reducer work superseded: repository retired',
    failure_details = (jsonb_build_object(
        'reason', 'repository_retired', 'retirement_id', $rid,
        'scope_id', stale.scope_id, 'work_item_id', stale.work_item_id,
        'generation_id', stale.generation_id, 'domain', stale.domain)
        || <priorFailureStaleSQL>)::text
WHERE stale.stage = 'reducer'
  AND stale.scope_id = ANY($scopes)
  AND stale.status IN ('pending','retrying','failed','dead_letter','claimed','running')
  AND NOT (stale.status IN ('claimed','running') AND stale.claim_until > clock_timestamp())
```

- Succeeded rows are not touched in phase 1 (the P1' bar). 2q adds
  `'succeeded'` to the status list.
- The statement embeds the prior-failure fold
  (`priorFailureStaleSQL`, `reducer_generation_filter_sql.go:234`), so
  `TestSupersedeStatementsFoldPriorFailure` holds.
- `AssertRetirementFenced` counts marked rows.

## Reducer Reopen And Replay Writers

Column `Closed by`: `2q (succeeded only)` means the writer touches succeeded
rows only, and 2q marked them `superseded`, so the writer finds none.
`Phase 1 (needs active generation)` means the writer requires an active
generation, which 7b removed. `Fresh-id residual` means an INSERT with an id that
does not exist yet, bounded by 2d and caught by 2z.

| Writer | Source | Touches | Closed by |
| --- | --- | --- | --- |
| Projector reducer-intent enqueue | `projector/runtime/projection.go:227` into `reducer_queue.go:25-35`, `:321` | New `pending` rows, any scope with a live projector Run | 2a + 2q |
| Deferred maintenance reopen | `ingestion_reopen_deployment_mapping.go:70-143`; floor `ingestion_reopen_correlation.go:104-121`, bound `:149-154` | `succeeded` to `pending`, `deployment_mapping`, `code_import_repo_edge`, and `CrossScopeCorrelationReopenDomains()`; no repo lock | 2q (succeeded only) |
| Targeted maintenance reopen | `ingestion_targeted_maintenance_write.go:145-177` | `succeeded` to `pending`, same domains, by partition | 2q (succeeded only) |
| Producer-settle reopen | `ingestion_producer_activation.go:298-306` | `ReopenSucceeded` of producer dependents, inside activation Finalize | 2q (succeeded only) |
| Admin reopen | `admin/store/reopen.go:75-85`; #7734 rollover fence `:199-213`; generation resolve `:28-39` | `succeeded` to `pending` for one domain; with the pointer NULL it resolves the newest generation, not an error | 2q (succeeded only) |
| `ReopenSucceeded`, `ReplayDomain` | `reducer_queue_replay.go:49-67` | `succeeded` to `pending`; `ReplayDomain` has no production caller | 2q (succeeded only) |
| Workload-materialization replay, UPDATE arm | `reducer_queue_replay.go:73-110` | Statuses `pending`, `claimed`, `running`, `retrying`, `succeeded`; never `superseded` | 2q (succeeded only) |
| Workload-materialization replay, INSERT arm | `reducer_queue_replay.go:279` (`ReplayWorkloadMaterializationOutcome`) | New row, fresh `work_item_id` | Fresh-id residual |
| Crossplane satisfied-by redrive | `reducer_queue_replay.go:368-375` | `ReopenSucceeded` (2q), then an enqueue at `:375` (fresh id) | 2q, then fresh-id residual |
| Cross-scope fanout reopen | `scope/completion/fanout.go:32-54` | Joins `generation.status = 'active'` through the pointer | Phase 1 (needs active generation) |
| Projector zombie heal | `projector_queue_zombie_heal.go:97-99` | A projector row for the active generation; needs a non-NULL pointer | Phase 1 (needs active generation) |
| Shared-intent upsert from a reducer handler | `code_import_repo_edge_handler.go:157`; `shared_intent_acceptance_writer.go:77-95` | Shared intents plus acceptance, not reducer rows | 2q (no handler runs) then 2b |
| A projector that outlived its lease | The Run that called `Enqueue` | Reducer rows with ids derived from its intents | Fresh-id residual |

One correction to round 3: its draft listed admin reopen as closed by phase 1.
The #7734 rollover fence resolves the generation inside the transaction, and
`reopenActiveGenerationQuery` returns the scope's pinned pointer "else the
newest generation row" (`admin/store/reopen.go:28-39`). With the pointer NULL it
resolves a superseded generation and reopens its succeeded rows, the same
shape as the maintenance floor. 2q closes it. The fence takes the scope row
first with a 5 s `lock_timeout` (`:196-211`), so it cannot join the lock cycle
(see the
[lock-order table](7766-repository-retirement-concurrency.md#lock-order-rule-and-the-fail-fix)).

Closed by phase 1 already: the cross-scope fanout and the zombie heal. A heal
whose statement snapshot predates phase 1 can still insert a pending projector
row for a generation phase 1 then supersedes. The #7130 claim branch supersedes
it (`projector_queue_claim_sql.go:158-179`), and 2a waits for any projector
lease.

## 2a And 2q

**2a, projector-lease wait** (`blocked/projector_lease_live`): no projector row
of the scopes has `status IN ('claimed','running') AND claim_until >
clock_timestamp()`. It converges within one lease TTL: the #7130 claim branch
supersedes pending, retrying, and expired rows of superseded generations
(`projector_queue_claim_sql.go:158-179`), Heartbeat refuses a superseded
generation and the service cancels the Run (`projector_queue_scan.go:414`,
`projector/service_heartbeat.go:65`), and Ack refuses
(`projector_queue_sql.go:108-116`). The projector `LeaseDuration` default, which
sets the bound in seconds, is NOT_CHECKED.

**2q, reducer quiesce.** It runs after 2a and before 2b.

- **Step A, unfenced**, in chunks of 10,000 by `work_item_id`: the 7d statement
  with `'succeeded'` added to the status list, bound by `scope_id = ANY`. It
  removes the bulk of the rows without the table lock. The scope-leading index
  is `fact_work_items_scope_generation_idx (scope_id, generation_id, status,
  updated_at DESC)` (`migrations/005_fact_work_items.sql:30-31`).
- **Step B, fenced**, through the scope-bound helper (below):
  1. `set_config('lock_timeout','1s',true)`;
  2. `LOCK TABLE fact_work_items IN EXCLUSIVE MODE`;
  3. the full 7d statement with `succeeded` included;
  4. `SELECT count(*) ... WHERE stage='reducer' AND scope_id = ANY AND status IN
     ('claimed','running') AND claim_until > clock_timestamp()`;
  5. commit.
- **Live count above 0:** `state=blocked`, `blocked_reason=reducer_lease_live`.
  The readback names the oldest `(work_item_id, domain, lease_owner,
  claim_until)`. The runner waits and repeats step B. The wait happens outside
  the fenced transaction.
- **`55P03`:** jittered 100 to 500 ms retry with a 30 s budget, then
  `blocked/claim_fence_busy` (a new `blocked_reason` value) and a retry on the
  next runner attempt.
- **Termination:** 2q ends when one fenced pass marks 0 rows and counts 0 live
  leases. It is idempotent and re-runs on every runner resume while the phase is
  before `purge`.

**The scope-bound helper.** Phase 1 and 2q step B share one new helper in
`rebuild/reset`. It binds the drain wait, the claim fence, and the recheck by
`scope_id = ANY` and applies `lock_timeout`. The existing
`WaitForReducerDrain`, `AcquireReducerClaimFence`, and `AssertRetirementFenced`
bind `(scope_id, generation_id)` pairs (`refinalize.go:148-150`), which is the
refinalize shape, so the design cannot reuse them as the bindings table
requires. The I2 guarantee is credited to the 2q recheck under `EXCLUSIVE`, not
to phase 1.

## Residual Risk

The fresh-id window between 2q and 2d stays open. A projector Run that
outlived its lease, a workload replay, or a Crossplane redrive can INSERT a row
with a new id after 2q's last pass. 2d bounds it (a deleted generation fails
`23503`) and 2z catches what lands in between.

If 2z ever finds residue after a clean 2c, the structural fix is a claim-side
supersede arm on `stale_generation.status = 'superseded'`. That changes general
queue semantics: today a superseded-never-active generation's reducer rows are
claimable, which is the P9 "after phase 1 processed 1" row. It gets its own
issue and its own prove-first, not this design. Whether
`ReplayWorkloadMaterialization` callers can in practice name a retired scope's
generation between 2q and 2d is reasoned structurally only (NOT_CHECKED).

## Interim Behavior Until The Runner Ships

Deliverable 1 has no 2q. After phase 1, a maintenance reopen, an admin reopen,
a producer-settle reopen, or a projector Run that outlives its lease can create
or reopen reducer rows for a retiring scope, and a reducer can claim them
because the pointer is NULL. The reducer then does work on a repository the
read surfaces already label `retiring`. The work is wasted and, because the
repository's data is intact, it is not wrong. It stops when 2q lands. The
tombstone does not hide this work, so an operator watching reducer queue depth
can see it.

## RED Tests

All run in the `reducer-contention` gate on real Postgres.

- `TestRetiredScopeReducerRowIsClaimableWithoutPointer` documents the hazard on
  origin/main: the generation is superseded, the pointer is NULL, and a pending
  row is claimed. It stays as the regression after 2q.
- `TestRetirementQuiesceStopsProjectorEnqueue`: hold a live projector lease
  through phase 1. `Enqueue` after phase 1 inserts pending rows. After 2q they
  are `superseded`, and `claimReducerWorkQuery` returns nothing for the scope.
- `TestRetirementQuiesceStopsMaintenanceReopen`: seed succeeded rows in
  `deployment_mapping`, `code_import_repo_edge`, and
  `CrossScopeCorrelationReopenDomains()`. After phase 1 alone,
  `RunDeferredRelationshipMaintenance` reopens them (RED against the current
  design). After 2q it and `ReopenCompletedWork` reopen 0.
- `TestRetirementQuiesceWaitsForLiveReducerLease`: a live-leased row survives
  the fenced pass and the row is `blocked/reducer_lease_live`. After release,
  the next pass marks it, including a row the ACK trigger returned to pending.
- `TestRetirementStep4RedrainDoesNotHoldRepoKey`: a reducer that needs the
  shared repo key finishes while phase 1 re-drains after a step 4 growth.

The bars for these paths are P1q (2q step B on the stalled shape) and P10' (the
runtime residue census); see the
[prove-first table](7766-repository-retirement-proof-and-rollout.md#prove-first-table).
