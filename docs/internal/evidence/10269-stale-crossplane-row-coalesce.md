# #10269: Stale Per-Generation Crossplane Rows Coalesce at Claim Time

Status: SUPERSEDED. The snapshot-EXISTS design below merged in PR
#10320, was proven unsound by the live EvalPlanQual interleave (a
sibling claimed mid-sweep), and is superseded by the lock-step carrier
membership design recorded in
[10269-stale-crossplane-row-coalesce-implementation.md](10269-stale-crossplane-row-coalesce-implementation.md)
for issue #10325. The rest of this file is preserved as the historical
record.

Root-Cause Evidence: the #10106 fix exempted
`crossplane_satisfied_by_materialization` from the claim-time
inactive-generation sweep with a blanket
`AND stale.domain <> 'crossplane_satisfied_by_materialization'` in
`supersedeInactiveReducerGenerationsPredicate`
(`go/internal/storage/postgres/reducer_generation_filter_sql.go`).
Every stale per-generation row therefore stayed claimable, claimed
oldest-first, and ran the full active-snapshot materialization
(fact load + extract + scope retract + edge MERGE) against the same
snapshot. The RED run of
`TestReducerClaimCoalescesStaleCrossplaneRowsLive` on base
`7f3a3e526a` exited 1 with the backlog case reporting
`row co-g0 status = "claimed", want "superseded"` and the carrier left
`pending`: the oldest stale row leased for a redundant execution
instead of retiring. The design note that derived this fix is
[10269-stale-crossplane-row-coalesce-design.md](10269-stale-crossplane-row-coalesce-design.md);
its throwaway shim (`10269-coalesce-shim.py`, exit 0) counts the same
bound in miniature (900 scopes x 25 generations: 22500 executions
without coalescing, 900 with).

## Design

The blanket exemption becomes conditional, inside the sweep's existing
lock step (both claim statements embed
`supersedeInactiveReducerGenerationsCTE`, so single and batch claim
inherit it with no second site):

```sql
AND (stale.domain <> 'crossplane_satisfied_by_materialization'
  OR EXISTS (
    SELECT 1
    FROM fact_work_items AS sibling
    JOIN scope_generations AS sibling_generation
      ON sibling_generation.scope_id = sibling.scope_id
     AND sibling_generation.generation_id = sibling.generation_id
    WHERE sibling.stage = 'reducer'
      AND sibling.domain = 'crossplane_satisfied_by_materialization'
      AND sibling.scope_id = stale.scope_id
      AND sibling.work_item_id <> stale.work_item_id
      AND sibling.status IN ('pending', 'retrying')
      AND (sibling_generation.ingested_at > stale_generation.ingested_at
        OR (sibling_generation.ingested_at = stale_generation.ingested_at
          AND sibling_generation.generation_id > stale_generation.generation_id))
  ))
```

Decisions, each load-bearing:

- Group is (scope_id, domain), never the conflict key: every
  default-fence domain shares `(scope, scope_id)`, so grouping by
  conflict key would prune other domains' debt.
- "Newer" is generation activation order (`ingested_at`, then
  `generation_id`), the order the sweep's generation guard already
  uses. `updated_at` is rewritten by claim/retry stamps and does not
  order generations.
- Carrier set is conservative: `pending`/`retrying` only.
  `claimed`/`running` are excluded because an in-flight carrier can
  still dead-letter and `superseded` is terminal and unreplayable, so
  the stale row is the fallback #10106 preserves. `failed`,
  `dead_letter`, and `succeeded` will not run again and carry nothing.
- Snapshot transitivity collapses a chain G1<G2<G3 in one statement:
  G1 sees carrier G2 and G2 sees carrier G3 in the statement snapshot,
  so only the newest carrier executes. Bound: handler executions per
  scope per drain epoch <= 1, plus at most 1 when a row was already in
  flight while newer generations queued.
- The probe reads the statement snapshot. The EvalPlanQual recheck of
  the lock step re-fetches only the locked stale row, so a carrier
  another worker leases between snapshot and lock still retires the
  stale row. That direction is safe: the leased carrier executes and
  discharges the debt, and if it later dead-letters the debt rests on
  dead-letter replay exactly as a sole row's does. Dead-letter writes
  require `status IN ('claimed', 'running')`
  (`failReducerWorkQuery`), so no production path moves a carrier from
  `pending` to `dead_letter` inside the window.
- Nothing else changes: one claim statement, lock order
  `work_item_id COLLATE "C"`, the readiness OR-gate, the
  `prior_failure` fold (a pruned `failed` row keeps its failure
  evidence), worker/batch/lease settings, and the execute path are all
  untouched. A side benefit: stale `failed`/`dead_letter` Crossplane
  rows, which the blanket exemption left un-superseded forever, retire
  once a carrier exists.

Rebase-time re-verifications from the design note, re-run at
`7f3a3e526a`: `rg intent.FactID go/internal/reducer/crossplane`
returns nothing (handler ignores `payload.fact_id`, exit 1); no
readiness requirement names this domain (the only
`crossplane_satisfied_by` migration hits are the redrive tables); the
domain still takes the default `(scope, scope_id)` conflict fence (no
case names it in `reducer_queue_conflict.go`).

Rejected alternatives are recorded in the design note: a newest-only
candidate filter bounds nothing over a full drain (rows become
claimable once the survivor succeeds), an execute-time probe still pays
a claim cycle per stale row, and enqueue-time identity coalescing has
the largest blast radius across Ack fencing and replay.

## Concurrency analysis

- Entry point: `cmd/reducer` builds the reducer service over
  `postgres.ReducerQueue`; `Claim`/`ClaimBatch` run the sweep CTE
  before candidate ranking in one statement; `Runtime.execute`
  rebinds a claimed stale row onto the active generation (#10106).
- Conflict domain: `fact_work_items` rows for one
  `(scope_id, crossplane_satisfied_by_materialization)`; graph side is
  the scope's SATISFIED_BY edge set, written retract-then-MERGE under
  one live lease at a time via the `(scope, scope_id)` fence.
- Transaction scope: unchanged — the EXISTS lives in the existing
  lock-step predicate of the single claim statement; no new lock is
  taken (the probe is a plain snapshot read), so the #8205 global
  `work_item_id COLLATE "C"` lock order is intact.
- Retry scope: unchanged — the surviving carrier retries as its own
  row; pruned rows are terminal `superseded` with the standard
  `reducer_superseded_by_newer_active_generation` failure class.
- Interleavings proven over the real queue on disposable PostgreSQL
  18 (`reducer_queue_crossplane_coalesce_test.go`):
  (a) carrier leased by another worker between snapshot and lock —
  stale retires, carrier stays `claimed` (debt holder survives);
  (b) carrier enqueued after a claim — the sole stale row executes that
  cycle (harmless idempotent duplicate), later stale generations prune
  on the next claim (backlog + quiet-delta cases compose this);
  (c) carrier dead-letters after pruning — identical resting state to
  a sole row dead-lettering today (dead-letter replay + redrive
  sweeper rediscovery), not re-proven here;
  (d) two claimers on independent connections drain a
  four-generation backlog: 3 superseded, exactly 1 claimed, no errors,
  run under `-race`.
- Starvation: none. Pruning only removes rows a newer carrier
  supersedes; the carrier keeps its claim position.
- Duplicate/stale replay matrix: duplicate delivery is unchanged
  (work-item identity still embeds generation, `ON CONFLICT DO
  NOTHING`); a succeeded carrier reopens through the existing
  succeeded-only replay paths; an empty queue claims nothing (every
  case ends with a no-work claim in the drain test).

## Regression: RED then GREEN over the real queue

`go/internal/storage/postgres/reducer_queue_crossplane_coalesce_test.go`
(seeds the isolated-schema fixture the supersede-readiness proofs use):

- `TestCrossplaneCoalesceSweepShape` (hermetic): pins the conditional
  arm, the carrier statuses, generation-order comparison, no
  `updated_at` ordering, and that the lock step carries the full
  predicate. The two blanket pins in
  `reducer_queue_supersession_test.go` now pin the conditional form
  for both claim statements.
- `TestReducerClaimCoalescesStaleCrossplaneRowsLive`: backlog drains
  to newest carrier; quiet-delta sole stale row survives; newest stale
  carrier survives when the active generation derives none;
  `dead_letter`/`failed` newest is not a carrier; stale `failed`
  prunes with the `prior_failure` fold asserted; `retrying` is a
  carrier; in-flight `claimed` sibling is not a carrier.
- `TestReducerClaimCoalesceCrossScopeIsolationLive`: a newer
  generation in another scope never carries this scope's debt.
- `TestReducerClaimCoalesceConcurrentClaimersLive` and
  `TestReducerClaimCoalesceCarrierClaimedBeforeLockLive`: interleavings
  (d) and (a) above.
- `TestReducerCoalesceBacklogDrainCountsLive`: the drain measurement
  below.

RED on `7f3a3e526a` (disposable PostgreSQL 18, DSN-gated):
prune cases failed (`claimed`/`pending`/`failed` where `superseded`
was wanted), every #10106 guard case passed. GREEN after the fix: all
cases pass, including under `-race`.

Commands (exit codes captured directly):

```text
python3 docs/internal/evidence/10269-coalesce-shim.py  # exit 0
ESHU_POSTGRES_DSN=postgres://postgres@127.0.0.1:55443/postgres?sslmode=disable \
  go test ./internal/storage/postgres/ -run \
  'TestCrossplaneCoalesceSweepShape|TestReducerClaimCoalesce|TestReducerCoalesceBacklogDrainCountsLive|TestReducerQueueClaimSupersedesInactiveGenerationReducerWork|TestReducerQueueBatchClaimSupersedesInactiveGenerationReducerWork' \
  -count=1 -race -v  # RED exit 1 before, GREEN exit 0 after
```

Performance Evidence: backlog-drain counts on the real queue
(`TestReducerCoalesceBacklogDrainCountsLive`, disposable PostgreSQL
18): 20 scopes x 5 generations = 100 pending Crossplane rows drain to
executions=20, superseded=80 — one handler execution (claim lease) per
scope, not per row. Before the fix the same drain leases all 100 rows
(RED observed the oldest stale row claimed first, the rest pending),
matching the shim's rows-vs-scopes table. Claim-statement
no-regression: `EXPLAIN (ANALYZE, BUFFERS)` of the shipped
`claimReducerWorkQuery` vs the blanket form (derived from the shipped
query by string surgery on the conditional arm, throwaway scratch
test, since deleted) on a mixed fixture of 100 scopes x 10
generations x (1 Crossplane + 1 workload_identity) = 2000 rows, 1800
stale. Shipped: lock step 9.12 ms retiring 1800 rows, statement
36.03 ms; blanket: lock step 3.79 ms retiring 900, statement
19.25 ms. The delta is the one-time retirement of 900 Crossplane
rows plus 900 carrier probes (SubPlan 2: 0.004 ms x 900 loops, index
scans on `fact_work_items_scope_generation_idx` and
`scope_generations_scope_latest_lookup_idx`, buffer hits only). The
probe executed exactly 900 times — once per stale Crossplane
candidate, never for the 900 workload_identity candidates (domain arm
short-circuits). After one real claim retired the backlog, the next
claim showed SubPlan 2 `never executed` and 3.03 ms total: steady
state pays nothing. Each retired row replaces one full handler
execution (active-snapshot load + extract + graph retract/MERGE), so
the trade is ~0.02 ms of in-statement work per avoided execution.
Worker, lease, and batch settings are unchanged (single claim,
`LeaseDuration` 1 minute in fixtures; production defaults untouched).
Wall times are single runs on a shared host and are indicative; the
load-bearing numbers are the execution/supersede counts and the plan
shapes.

Observability Evidence: no new signal. Pruned rows land in the
existing `superseded` terminal state with
`failure_class = 'reducer_superseded_by_newer_active_generation'` and
`domain` + `scope_id` in `failure_details`, countable per scope/domain
through the existing status surfaces; the #10269 shape is separable
from other domains by that domain value. Removing redundant runs also
stops inflating `eshu_dp_crossplane_satisfied_by_edges_total` G-fold
per drain epoch. Claim latency, contention, and retry behavior remain
visible on the existing reducer claim spans/metrics — the sweep is
still one statement in the same claim.

## NOT_CHECKED

- Production backlog shape for this domain (how large G gets under
  real lag): fixtures are proof shapes, not telemetry.
- Claim-plan behavior at production queue depth beyond the 2000-row
  fixture; the probe is index-only per stale Crossplane candidate, so
  cost scales with stale Crossplane rows retired, not queue depth.
- Batch-claim fair-rank interaction beyond the shared-CTE argument
  and the batch shape pin (sweep precedes ranking in both
  statements); no batch-specific live coalesce case was added.
- NornicDB/graph cost of one avoided execution (unchanged handler,
  unmeasured here; Neo4j is the canonical backend).
- `BenchmarkReducerQueueClaimDeepQueue` before/after on a quiet host:
  not run (shared host); the EXPLAIN differential above is the
  claim-statement evidence instead.
