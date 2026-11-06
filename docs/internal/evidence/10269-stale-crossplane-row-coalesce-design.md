# #10269: Coalesce Stale Per-Generation Crossplane Rows — Design (Deferred)

Status: DESIGN + PROOF ONLY. Do not implement from this branch and do not
merge this note as an implementation. The arbiter ruled DEFER-AFTER-MERGE:
#10106 is unmerged, and this change is only meaningful stacked on its
claim-time exemption, so it must be implemented by re-reading this note
after #10106 lands, not by merging code from here. This branch changes
no production code.

Bases this design was grounded in:

- Current main: `origin/main` at `fcc196d1d1` (this worktree's HEAD
  before this note; verified clean `git status`).
- Pending #10106: branch `lane3/10106-quiet-delta-replay` at
  `12e87067df`, core commit `f700db8d0d` ("rebind superseded Crossplane
  SATISFIED_BY replays to the active generation"). It is NOT in
  `origin/main`: `git merge-base --is-ancestor f700db8d0d origin/main`
  fails, and no PR exists for the lane yet. Caution: `git log
  origin/main --grep="#10106"` returns `9794d1a9b7`, which is the #10094
  delta-base commit whose body merely references #10106. The claim-time
  sweep change this issue builds on exists only in `f700db8d0d`.

Root-Cause Evidence: #10106's fix exempts
`crossplane_satisfied_by_materialization` rows from the claim-time
inactive-generation sweep (`AND stale.domain <>
'crossplane_satisfied_by_materialization'` in
`supersedeInactiveReducerGenerationsPredicate`), so a stale row stays
claimable and `Runtime.execute` rebinds it onto the active generation.
The exemption is blanket: if G generations of one scope queued during
reducer lag, all G rows survive, claim oldest-first
(`reducer_queue_claim_query.go:101` `ORDER BY updated_at ASC`), and each
runs the full handler against the same active snapshot. That is the
redundant-work scaling #10269 bounds.

## Why per-generation rows for one scope are interchangeable

Verified at `fcc196d1d1`:

- At most one row per (scope, generation, domain): `reducerWorkItemID`
  embeds `GenerationID` (`reducer_queue_helpers.go:344`) and enqueue is
  `ON CONFLICT (work_item_id) DO NOTHING` (`reducer_queue.go:37`).
- The intent is scope-keyed: `EntityKey =
  "crossplane_satisfied_by_materialization:" + scopeID`
  (`satisfied_by_intents.go:51`), one intent per scope generation.
- The production handler path reads by scope, not by row generation:
  `ListActiveCrossplaneSatisfiedByFacts(ctx, intent.ScopeID)`
  (`satisfied_by_load.go:53`) returns the active snapshot (delta overlaid
  on base, plus every active XRD). The generation-keyed loader is only a
  fallback for stores that do not implement that interface.
- One execution costs: 1 active-snapshot fact load, 1 extract over those
  envelopes, 1 scope-wide retract (`satisfied_by_materialization.go:217`),
  1 edge write (`:231`), and a redrive-ledger confirmation read/write
  when edges land. Retract-then-MERGE on the same snapshot is
  idempotent, so executions 2..G reproduce execution 1's graph state
  while re-paying every cost and re-incrementing the edges counter.
- Executions for one scope already serialize: this domain falls to the
  default conflict fence `(scope, scope_id)`
  (`reducer_queue_conflict.go:209-210`), and the live-lease unique index
  allows one live lease per key.

Where rows DO differ, and why none of it blocks coalescing:

| Difference | Consumer | Effect under coalescing |
| --- | --- | --- |
| `generation_id` (in work_item_id) | Retract/write attribution, `PriorGenerationCheck` | Post-#10106 the executing intent is rebound to the active generation, so every survivor runs under the same id the newest row would |
| `payload.fact_id`, `source_system` | None in the handler: `rg intent.FactID go/internal/reducer/crossplane` (non-test) returns nothing at this base | Trigger-fact provenance differs per row but never reaches the materialization; implementer must re-run this grep at rebase time |
| `attempt_count` | `shouldSkipRetract` (skip only when no prior generation and attempt <= 1) | With >= 2 generations queued a prior generation exists, so retract runs regardless; the skip case is exactly the single-row case coalescing never touches |
| failure evidence on `failed`/`dead_letter` rows | Audit | Prune reuses the sweep's existing `priorFailureStaleSQL` fold, so nothing is lost |

## The contract that must be preserved (#10106 rebind guarantee)

For every scope, at every claim statement, at least one row carrying the
scope's SATISFIED_BY debt must remain executable until a run against a
snapshot at-or-after that debt was scheduled has had its chance to
execute. Concretely the three #10106 properties:

1. A sole stale row (quiet delta derived no successor) is never
   terminalized; it claims and rebinds to the active generation.
2. A stale row is retired only when another row that will still execute
   carries the same debt (same scope, same domain, newer generation).
   A `dead_letter` or `failed` newer row carries nothing: it cannot
   claim. #10106 explicitly preserves "a rebound predecessor may
   succeed where its successor dead-lettered".
3. No serialization is introduced: claim stays one statement, lock order
   stays `work_item_id COLLATE "C"`, worker/batch/lease settings are
   untouched, unrelated scopes and domains are unaffected.

## Recommended design: prune-in-sweep (Design A)

Change only the blanket exemption #10106 adds, turning "never supersede
this domain" into "supersede this domain's stale row only when a newer
carrier exists". Sketch (final SQL is the implementer's, after rebase):

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
                  AND sibling_generation.generation_id
                      > stale_generation.generation_id))))
```

Design decisions, each load-bearing:

- Group is (scope_id, domain), NOT the conflict key. Every default-fence
  domain shares `(scope, scope_id)`; grouping by conflict key would
  prune other domains' debt.
- "Newer" is generation activation order (`ingested_at`, then
  `generation_id`, the order the existing sweep and freshness checks
  already use), never `updated_at`. Retry/claim stamps rewrite
  `updated_at`, so it does not order generations.
- Carrier set is conservative: `pending`/`retrying` only.
  `claimed`/`running` are excluded on purpose: an in-flight carrier can
  still dead-letter, and `superseded` is terminal and unreplayable (only
  `succeeded` rows reopen, `reducer_queue_replay.go:55`), so pruning
  against an in-flight carrier could destroy the only fallback #10106
  preserves. `succeeded` is excluded: it executed against an older
  snapshot and carries no future run. Cost of conservatism: at most one
  redundant run per scope per in-flight overlap (see bound below).
- The change folds into the existing single lock step instead of adding
  a second locking CTE. One lock acquisition in global
  `work_item_id COLLATE "C"` order keeps the #8205 deadlock argument
  intact, and the `FOR NO KEY UPDATE OF stale` recheck (EvalPlanQual)
  re-evaluates the whole predicate, EXISTS included, on the newest row
  version. A sibling claimed between snapshot and lock stops being a
  carrier (`claimed` not in set), so the stale row is dropped from the
  lock set and survives — the safe direction.
- Snapshot transitivity collapses a chain in one statement: with
  G1<G2<G3 pending, G1 sees carrier G2 and G2 sees carrier G3 in the
  statement snapshot, so both are superseded and G3 alone executes.
- Everything else in the sweep is reused unchanged: status set
  (`pending`, `retrying`, `failed`, `dead_letter`), the $2 domain
  filter, the readiness OR-gate, the `prior_failure` fold, and the
  UPDATE by locked ids. A side benefit: stale `failed`/`dead_letter`
  Crossplane rows, which #10106's blanket exemption would leave
  un-superseded forever (they cannot claim), are retired once a carrier
  exists.
- Debt transfer is sound for `cross_scope_replay_required` (migration
  093): the flag is set on claimed/running rows, which the sweep never
  touches. A pending stale row's debt is "materialize the active
  snapshot", which the carrier discharges when it executes after the
  prune; cross-scope completions during the carrier's own run flag the
  carrier.
- Both claim statements inherit the behavior for free because both
  embed `supersedeInactiveReducerGenerationsCTE`
  (`reducer_queue_claim_query.go:8`, `reducer_queue_batch_query.go:63`).
  No candidate-CTE, runtime, wiring, or schema change.

Resulting bound: per scope, handler executions per drain epoch <= 1,
plus at most 1 extra when a row was already in flight while newer
generations queued. Independent of G, the generations queued during lag.

## Alternatives considered and rejected

| Design | Bound over a full drain | Why not |
| --- | --- | --- |
| B: newest-only candidate filter (hide stale rows while a newer claimable sibling exists) | None: still G executions (shim column `filter(B)`) | Non-terminalizing. Once the survivor succeeds, the next-stale row becomes newest-claimable and runs the full materialization anyway. It only reorders work, and unleased stale rows accumulate and keep being scanned by every claim |
| C: execute-time check in `Runtime.execute`/`generationCurrent` (probe for newer sibling, return superseded without handling) | Handler runs bounded, but claim+ack churn stays O(G) per scope | Adds a queue-state collaborator and a probe to the runtime, edits the exact `generationCurrent` function #10106 adds (worst conflict surface), and still pays a claim cycle per stale row. The sweep already holds the locks and the predicate |
| D: enqueue-time coalescing (drop generation from this domain's work_item_id so one row per scope exists) | 1 row ever | Work-item identity feeds Ack lease fencing, replay, and reopen paths across the queue; changing identity for one domain is the largest blast radius for a P2. Rejected unless A proves unworkable on real Postgres |

## Cheap proof (row counts, no wall time)

`10269-coalesce-shim.py` beside this note is a THROWAWAY model of the
semantics above (marked as such in its header; never production). It
counts handler executions — 1 unit = 1 active-snapshot load + extract +
retract/write — for backlog shapes S scopes x G queued generations, all
pending, drained under the per-scope conflict fence. Output at this
base (`python3 docs/internal/evidence/10269-coalesce-shim.py`, exit 0,
assertions included):

```
     S x G    rows  no-coalesce  prune(A)  sweep writes  filter(B)
   1 x 10        10           10         1             9         10
  10 x 10       100          100        10            90        100
 100 x 10      1000         1000       100           900       1000
 900 x 25     22500        22500       900         21600      22500
```

Read: without coalescing executions = rows (the #10269 problem). Design
A executions = scopes, paid for with (rows - scopes) one-time supersede
UPDATEs inside a claim statement that already performs that UPDATE
shape for other domains. Design B changes nothing over a full drain.
The 900x25 shape is the fixture shape from the #5426 reopen-bound
proof, reused for scale readability; it is not a production backlog
measurement.

Guard cases the shim asserts (the #10106 contract in miniature):
sole stale row survives and executes; newest `dead_letter` or `failed`
does not absorb an older pending row; stale `failed` prunes against a
newer pending carrier; in-flight overlap totals <= 2 executions;
`retrying` is a carrier; a newer generation in another scope is not.

What this proves and what it does not. It proves the counting bound and
the guard-case logic of the predicate as specified. It does NOT prove:
Postgres snapshot/EvalPlanQual behavior of the EXISTS under concurrent
claims, claim-statement plan cost of the EXISTS, or any wall-time or
latency effect (host was loaded; no timing was taken or claimed). Those
are the implementer's rungs below. Per-execution absolute cost scales
with the scope's active-snapshot fact count and the cross-scope XRD
set, which this note deliberately does not quantify from shape alone.

Performance Evidence: design-only; the shim's execution-count table
above is the measured artifact. No production statement changed, so
there is no claim/execute no-regression result yet — that is acceptance
work for the implementer, not a result recorded here.

Observability Evidence: no signal changes in this note. For the
implementation: pruned rows land in the existing `superseded` terminal
state with `failure_class =
reducer_superseded_by_newer_active_generation` and `domain` in
`failure_details`, so they are countable per scope/domain without a new
metric or label. Removing redundant runs also stops inflating
`eshu_dp_crossplane_satisfied_by_edges_total` G-fold per drain epoch.
If operators need coalesced-vs-superseded separation, add a `reason`
value inside `failure_details` JSON, not a new failure_class label.

## Concurrency analysis (for the implementer's proof)

- Conflict domain: `(scope, scope_id)` rows in `fact_work_items`; the
  graph side is the scope's SATISFIED_BY edge set, written
  retract-then-MERGE under one live lease at a time.
- Transaction scope: unchanged. One claim statement; the EXISTS lives
  in the existing lock-step predicate. Retry scope: unchanged; a
  survivor that fails retries as its own row exactly as today.
- Bad interleavings the implementer must test over the real queue (not
  row-set equivalence): (a) sibling claimed by a second connection
  between snapshot and lock — EvalPlanQual recheck must drop the stale
  row from the prune set; (b) sibling enqueued after the sweep snapshot
  — stale row survives this claim, pruned or executed next cycle, never
  lost; (c) carrier dead-letters after stale rows were pruned against
  it while pending — debt then rests on dead-letter replay and the
  redrive sweep's rediscovery, identical to a sole row dead-lettering
  today; (d) concurrent claimers on independent connections — exactly
  one execution winner per scope, extending #10106's case (3).
- Starvation: none introduced. Pruning only removes rows a newer
  carrier supersedes; the carrier keeps its claim position.

## Rebase readiness vs #10106

#10106 lane today rebases cleanly: `git merge-tree --write-tree
--name-only origin/main lane3/10106-quiet-delta-replay` at `fcc196d1d1`
exited 0 with no conflicted files (lane is 20 commits behind its
merge-base `17feac86e0`). Re-verify at implementation time; this decays.

Files #10106 touches (21): its evidence notes, `graph-delta-union.md`,
`cmd/reducer/{main.go,active_generation_wiring_test.go}`,
`reducer/{AGENTS.md,README.md,defaults.go,defaults_registry.go,runtime.go,generation_rebind_test.go}`,
`reducer/contract/{doc.go,generation_check.go}`,
`reducer/crossplane/satisfied_by_load.go`,
`storage/postgres/{README.md,generation_freshness.go,reducer_generation_filter_sql.go,reducer_queue_supersession_test.go}`,
`storage/postgres/crossplane/{README.md,satisfied_by_quiet_delta_replay_test.go,satisfied_by_redrive_behavior_live_test.go}`.

Files Design A touches: `reducer_generation_filter_sql.go` (the
predicate), `reducer_queue_supersession_test.go` (shape pins), the
#10106 quiet-delta live test (new backlog cases), folder README/AGENTS
docs, and a new evidence note. Nothing in `reducer/runtime.go`,
`contract`, `generation_freshness.go`, or wiring — that is the point of
choosing A over C.

Predicted conflict surface if #10269 were (incorrectly) branched now:
exactly one textual hunk — #10106 inserts its 7-line comment+exemption
at `reducer_generation_filter_sql.go:133-139` (lane numbering), the
same lines Design A rewrites — plus the two string pins #10106 adds in
`reducer_queue_supersession_test.go`. Semantic resolution is
mechanical: take #10106 first, then replace the blanket exemption with
the conditional form and update the pins. No other #10106 hunk is
within editing distance. Sequencing for the implementer: land #10106,
grep that the exemption text and `generationCurrent` match this note's
description, then implement; if #10106 changed shape in review, the
carrier-set and lock-step decisions above are what must be preserved,
not the sketch's exact text.

## Acceptance-criteria mapping for the implementer

| #10269 criterion | Design element | Proof the implementer must add |
| --- | --- | --- |
| Stale rows coalesce/prune without breaking superseded-replay rebind | Conditional exemption + conservative carrier set; sole-stale-row and dead-letter-newest cases never prune | RED-first live interleave over the real queue extending #10106's `quiet_delta_replay` cases: G>=3 queued generations drain to exactly 1 handler run with the edge written under the active generation; shim guard cases re-expressed as Go tests incl. EvalPlanQual interleave (a) and concurrent claimers (d); shape pins updated in `reducer_queue_supersession_test.go` for BOTH claim statements |
| Backlog-drain measurement showing the redundant-work bound | Executions per scope <= 1 (+1 in-flight overlap), replacing G | Extend the shim's counts to the real harness: seeded backlog (fixture S x G), count handler executions / fact-load rows / superseded rows before vs after on disposable Postgres; quiet-host wall-time comparison only under proof-plan timing rules (measured bound, live teardown), otherwise report counts only |
| No-regression evidence for claim/execute statements | EXISTS inside the existing lock step; execute path untouched | EXPLAIN (ANALYZE, BUFFERS) differential of single + batch claim statements with/without the EXISTS on a backlog fixture: must show the EXISTS is not evaluated per-row for non-Crossplane candidates at a cost that moves the claim plan, or restructure (literal domain in EXISTS, partial-index support) until it does not; `BenchmarkReducerQueueClaimDeepQueue` before/after on a quiet host; `scripts/verify-performance-evidence.sh` markers on the PR note |

Also required by repo gates for this surface: performance-evidence
scripts (`scripts/test-verify-performance-evidence.sh`,
`scripts/verify-performance-evidence.sh`) per concurrency-deadlock-rigor,
folder README/AGENTS updates for `storage/postgres` (and `crossplane`
if its fence list changes), and re-verification at rebase time that
(a) the handler still ignores `payload.fact_id`, (b) no
`reducer_claim_readiness_requirements` row has appeared for this domain
(none found in readiness SQL or migrations at this base; the table is
data-shaped, so recheck), and (c) the default conflict fence for this
domain is still `(scope, scope_id)`.

## Open questions for the arbiter / implementer

1. Carrier set: this note recommends excluding `claimed`/`running` to
   preserve the dead-letter fallback, accepting <= 1 redundant run per
   in-flight overlap. Including them tightens the bound to exactly 1
   but can destroy the only fallback when the in-flight carrier
   dead-letters. Needs an explicit ruling if reversed.
2. Should pruned rows get a distinct `failure_details.reason` (e.g.
   `coalesced_newer_sibling`) for operator counting, or is
   domain+scope in the existing details enough?
3. Observer visibility is unchanged (`activeFactWorkItemsCTE` already
   hides unleased older-generation rows, including the survivor until
   claimed — the window #10106's note accepts). Confirm no drain/read
   contract needs the pruned count surfaced.

## NOT_CHECKED

- Real-Postgres behavior of the proposed EXISTS: snapshot vs
  EvalPlanQual recheck under concurrent claims, plan shape and cost on
  deep queues, lock-order interaction with ordered replays (#8205
  class). Model-only in this note.
- Any wall-time, latency, or throughput number. None measured; the
  host was loaded and the shim counts executions only.
- Production backlog shape for this domain (how large G gets under
  real lag, fact counts per active snapshot). The S x G table is a
  fixture, not telemetry.
- #10106's final merged shape. Review may move the exemption or the
  rebind; the sequencing check in "Rebase readiness" is mandatory.
- Batch-claim fair-rank windows: pruned rows disappear before ranking
  because the sweep CTE precedes the candidate CTE in both statements
  (read, not executed); implementer to confirm with the batch live
  tests.
- NornicDB/graph-side cost of one execution. Unchanged by design and
  unmeasured here.
