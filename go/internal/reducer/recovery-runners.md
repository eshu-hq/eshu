# Reducer Recovery Runners

Split from `README.md` (issue #5786). Keep the package overview in
`README.md`; keep the liveness, dead-letter, orphan-sweep, and
cross-scope-ownership repair runner details here.

## Generation Liveness Recovery

`GenerationLivenessRunner` is the self-healing path for the generation
lifecycle. An `active` generation that reaches canonical-nodes-committed but
makes no forward progress and has no newer same-scope generation to supersede it
sits `active` indefinitely. The runner delegates to
`storage/postgres.GenerationLivenessStore`, which runs two bounded statements per
poll: it supersedes orphaned older `active` generations once a newer same-scope
generation is authoritative, and re-enqueues projector work for wedged actives
past the activation deadline (re-driving reduce -> readiness -> projection over
existing facts). Wedge detection gates on real downstream blockage, not age
alone: a generation is only wedged when it has an outstanding
`shared_projection_intents` row (`completed_at IS NULL`) and no unresolved
reducer fact-work remains for that same generation, and no source-local
projector work is already pending, claimed, running, or retrying. A
healthy quiet scope stays `active` and projected (the projected baseline is "has
been active") with every intent completed; a busy full-corpus bootstrap scope
still moving through reducer work is treated as progressing; and a pending
liveness recovery row is allowed to run before the sweep spends more budget.
Those cases are not re-driven, which prevents false `stuck` alarms on idle
installations and normal bootstrap backlog, and prevents tight budget burn while
recovery is already in flight. A succeeded source-local projector row is the
normal activation lifecycle state, so the bounded recovery upsert may reopen it
when downstream blockage still makes the generation wedged. A per-generation
re-drive budget (`liveness_recovery_attempts` on the work item payload) bounds
retries so a poison scope cannot loop. The conflict domain is `scope_id`; both
statements are idempotent under concurrent reducer workers.
The operator escape hatch is
`POST /api/v0/admin/recover-generations`, which durably re-drives a named scope
set and records the action in the `admin_replay_requests` ledger.

Observability Evidence: `eshu_dp_active_generations` gauges active generations by
`fresh`/`aging`/`stuck` age bucket (`stuck` requires age past the deadline,
outstanding `shared_projection_intents`, no unresolved reducer fact-work for the
same generation, and no source-local projector row already pending, in progress,
so it does not fire on healthy quiet aged scopes, reducer backlog, or in-flight
recovery);
`eshu_dp_generation_liveness_recovered_total`,
`eshu_dp_generation_liveness_superseded_total`, and
`eshu_dp_generation_liveness_failures_total` count sweep outcomes; completion and
failure logs carry the `reduction` phase attribute. No-Regression Evidence: the
runner adds no hot-path Cypher or worker-default change; it re-uses the existing
projector enqueue path under bounded `LIMIT` statements. Verify with
`go test ./internal/reducer -run 'GenerationLiveness' -count=1` and
`go test ./internal/storage/postgres -run 'GenerationLiveness|WedgedActive|OrphanedActive' -count=1`.

## Poison Dead-Letter Liveness Recovery (#4740)

`GenerationLivenessRunner` above only re-drives a scope whose newest generation
is `active`. It does not reach a scope whose newest generation is terminally
`dead_letter`: `dead_letter` is never re-claimed by the normal claim path, and
the wedge-detection NOT EXISTS guard treats same-generation `dead_letter`
reducer work as still "in progress," excluding it from the wedged re-drive
path. Such a scope can never self-heal without an operator or a dedicated
bounded arm.

`PoisonLivenessRunner` closes that gap. It delegates to
`storage/postgres.PoisonLivenessStore`, which runs two bounded, read-only-safe
statements: `CountPoisonDeadLetters` (an aggregate query) and, only when
bounded auto-retry is enabled, `RecoverPoisonDeadLetters` (the re-enqueue
UPDATE). The poison class is: a `fact_work_items` row with `status =
'dead_letter'` whose scope has no strictly-newer `scope_generations` row (same
"strictly newer" `(ingested_at, generation_id)` comparator the
generation-liveness supersede query uses). A scope with any newer generation —
regardless of that newer generation's own status — has already moved on, so the
dead-letter row is historical, not live poison.

The default operational posture is surface-only
(`ESHU_POISON_LIVENESS_AUTO_RETRY_ENABLED=false`): the stuck-gauge is wired
unconditionally in `cmd/reducer` so an operator always sees the poison-class
size, but `PoisonLivenessRunner` itself — and therefore any `dead_letter ->
pending` re-enqueue write — is only constructed when an operator opts in. When
enabled, the bounded arm re-enqueues a dead-letter row to `pending` under a
per-row `poison_recovery_attempts` budget carried in the work item's JSONB
payload (`ESHU_POISON_LIVENESS_MAX_RECOVER_ATTEMPTS`, default 1, mirroring the
`liveness_recovery_attempts` idiom above; no new schema column). A row at or
past the budget ceiling is excluded by the candidate CTE, so a genuinely poison
item stops looping and is left `dead_letter` for an operator to inspect. The
conflict domain is `work_item_id` (the `fact_work_items` primary key): the
recovery UPDATE re-verifies `status = 'dead_letter'` at write time
(`AND target.status = 'dead_letter'` on the target row, not only the read-time
candidate snapshot), so under Read Committed a concurrent reclaim of the exact
same row between the candidate CTE's snapshot and the UPDATE's row lock is
never clobbered — the write-time WHERE re-evaluates (EvalPlanQual recheck)
against the now-committed row and affects zero rows for it instead.

Detection is bounded by a dedicated partial index,
`fact_work_items_dead_letter_poison_idx` (migration `043`), on
`(scope_id, generation_id) WHERE status = 'dead_letter'`. Because `dead_letter`
is terminal, the index only grows on new dead-letters and shrinks when the arm
successfully re-enqueues a row (its status leaves `dead_letter`), so it stays
proportional to the live poison backlog rather than the full `fact_work_items`
table.

No-Regression Evidence: `go test ./internal/storage/postgres/...
./internal/reducer/... ./cmd/reducer ./cmd/ingester -count=1` passes
(4281 tests). Against a throwaway `postgres:16-alpine` instance (set
`ESHU_POISON_LIVENESS_PROOF_DSN`), `go test ./internal/storage/postgres -run
'TestPoisonLivenessIntegration|TestRecoverPoisonDeadLettersQueryDoesNotClobberConcurrentReclaim'
-count=1` proves all four required behaviors: (1) RED — the existing
generation-liveness gauge (`CountActiveGenerationsByAge`) counts 0 of 3 seeded
poison scopes because their newest generation is `failed`, not `active`; GREEN
— `CountPoisonDeadLetters` counts exactly the 3 poison scopes/items and excludes
a healed decoy (`dead_letter` with a newer `active` generation for the same
scope); (2) the bounded arm increments `poison_recovery_attempts` 0->1->2 across
two re-dead-lettered sweeps, then a third sweep at the ceiling affects 0 rows
and leaves the row `dead_letter` for an operator; (3)
`GaugeUsesPartialIndex` forces `enable_seqscan=off` and asserts the
`countPoisonDeadLettersQuery` EXPLAIN plan references
`fact_work_items_dead_letter_poison_idx`, proving the index is usable for this
exact query shape; (4)
`TestRecoverPoisonDeadLettersQueryDoesNotClobberConcurrentReclaim` races a real
concurrent reclaim transaction (`dead_letter -> claimed`) against the sweep's
blocked UPDATE and proves the write-time guard leaves the reclaimed row
untouched (`Recovered = 0`, final status `claimed`, `lease_owner` unchanged,
budget counter not incremented) instead of clobbering it back to `pending`.

Observability Evidence: `eshu_dp_poison_dead_letter_scopes`,
`eshu_dp_poison_dead_letter_items`, and
`eshu_dp_poison_dead_letter_oldest_age_seconds` are observable gauges reporting
the current poison-class size and oldest item age, registered unconditionally
in `cmd/reducer` (`poisonLivenessObserverFor` +
`telemetry.RegisterPoisonLivenessObservableGauges`) so the class is visible
regardless of the auto-retry flag, independent of whether
`PoisonLivenessRunner` itself is constructed. When the runner is constructed
(auto-retry enabled), it records through the shared `*telemetry.Instruments`
contract passed into its `Instruments` field — not an inline meter — via
`recordResult`/`recordFailure`: `eshu_dp_poison_liveness_recovered_total` and
`eshu_dp_poison_liveness_failures_total` count bounded sweep outcomes;
completion and failure logs carry the `reduction` phase attribute and, on
failure, the `poison_liveness_error` failure class.

## Graph Orphan Sweep

`GraphOrphanSweepRunner` runs beside reducer intent workers and shared
projection. It delegates to `storage/cypher.OrphanSweepStore`, which computes
orphan status as a Go-side anti-join between two bounded reads per label
(candidates with no relationship clause, and connected keys via a concrete
relationship-variable `MATCH` anchored on those candidates' identity keys --
see `storage/cypher/README.md` and `storage/cypher/evidence-5147-orphan-sweep-antijoin.md`
for why: every relationship-existence predicate is mis-evaluated on the pinned
NornicDB backends), then marks, clears, and deletes only disconnected nodes in
a closed label set: `Repository`, `Platform`, `EvidenceArtifact`, `File`,
`Directory`, and `Module`. The sweep uses static-label, key-anchored Cypher
writes, a single-owner Postgres partition lease, a per-label batch limit, a
per-label count cap, and a TTL marker (`eshu_orphan_observed_at_unix`) so one
transiently disconnected cycle cannot delete a node immediately; a TOCTOU
re-verify immediately before delete drops any key that reconnected mid-cycle.
Repository sweeps exclude `evidence_source='projector/canonical'`; empty but
active source-local repositories remain canonical-writer truth, not sweep
candidates.

This runner is cleanup, not truth ownership. Relationship retraction,
canonical-node replacement, and reducer-owned materialization still own their
normal invariants. The sweep removes only nodes that remain disconnected after
the TTL and clears the marker when a relationship returns.

No-Regression Evidence: `go test ./internal/storage/cypher -run
'TestDefaultOrphanSweepLabelsIncludesCodeStructureLabels|TestBuildCandidateOrphanNodesQueryUsesStaticLabelNoRelationshipPredicate|TestBuildConnectedKeysQueryUsesConcreteRelationshipVariable|TestBuildClearMarkSweepStatementsAreKeyAnchoredNoRelationshipPredicate|TestOrphanSweepStoreDelaysCodeStructureDeletionDuringProjectionRace|TestGraphOrphanNodeCountsUsesDefaultCodeStructureLabels|TestCanonicalCodeStructureNodesStampOrphanSweepMetadata|TestRepoRelationshipUpsertStamps|TestOrphanSweepStoreUsesInjectedClockForMarkAndCutoff|TestOrphanSweepStoreConvergesAcrossBoundedCyclesForAllDefaultLabels|TestOrphanSweepStoreTOCTOUGuardDropsReconnectedKeyBeforeSweep'
-count=1` proves the bounded static-label, no-relationship-predicate anti-join
read shapes, metadata stamping for relationship-created repositories and
platforms, default coverage for code structure labels, Directory and imported
Module metadata stamping, bounded convergence, newly observed code-structure
orphan retention before TTL expiry, telemetry observer counts, the TOCTOU
guard, and injected-clock TTL behavior. `go test
./internal/storage/cypher -run
TestRepositoryCandidateQueryExcludesSourceLocalCanonicalRepositories -count=1`
proves source-local canonical Repository nodes are outside the sweep predicate.
`go test ./cmd/reducer -run TestProductionWiringConsumesCapabilityDefaults
-count=1` proves the reducer runtime consumes the same closed label default
instead of a stale subset.
`go test ./internal/reducer -run
'TestGraphOrphanSweepRunner|TestServiceStartsGraphOrphanSweepRunner' -count=1`
proves the runner drains available delete batches without lowering worker
concurrency, skips graph mutation when another replica owns the sweep lease, and
starts as a side runner in `Service.Run()`.

Observability Evidence: `GraphOrphanSweepRunner` completion logs include total
and per-label counts, marks, deletes, duration, `phase=reduction`, and
`failure_class=graph_orphan_sweep_error` on failure. The reducer registers
`eshu_dp_graph_orphan_nodes` through `telemetry.RegisterGraphOrphanObservableGauge`;
the metric is labeled only by closed `node_label` values and uses the configured
per-label count cap.

## Cross-scope node ownership (#5007)

The AWS/GCP/Azure CloudResource, EC2-instance, and Kubernetes-workload node row
builders stamp `source_order_key` on every node row — a fixed-width
`(observed_at, source_fact_id)` encoding whose lexicographic order matches the
intended "latest observation, source_fact_id tie-break" order (`sourceOrderKey`
in `source_order_key.go`). Within a scope generation, duplicate-uid rows resolve
to the max order key (`preferMaxSourceOrderKey`) rather than last-fact-by-slice.
Across scopes, `cmd/reducer` wraps these three node writers in the
`internal/graphowner` owner-ledger gate, which resolves the shared node to the
max-order-key contributor via a Postgres advisory-lock + `graph_node_owner`
ledger (NornicDB cannot resolve the concurrent property-write conflict itself,
#5062). See `docs/internal/design/5007-cross-scope-node-ownership.md`.
