# storage/cypher evidence and change history

Dated evidence and change-history entries for `go/internal/storage/cypher`,
split out of [AGENTS.md](AGENTS.md) to keep the read-first guidance under the
repository's 500-line cap. Every entry below is preserved verbatim from the
prior single-file AGENTS.md. Add new dated entries here, not in AGENTS.md.

## Evidence

### 2026-06-22 — Canonical writer retryable-error propagation (#3483)

Issue #3483 reported NornicDB canonical writes dead-lettering under write
pressure ("NornicDB connection timeouts + 376 dead-letters"). Root cause:
`CanonicalNodeWriter.Write` was the only major graph writer in this package
that returned its executor errors bare (`fmt.Errorf("...: %w", err)`) without
`WrapRetryableNeo4jError`. Transient failures — driver retry-budget exhaustion
(`*neo4j.TransactionExecutionLimit`), `*neo4j.ConnectivityError`, and the
`retryableNeo4jCodes` set — therefore reached `ProjectorQueue.Fail` as
non-`projector.RetryableError` values and were classified `projection_failed`
(terminal dead-letter) at `projector_queue.go` instead of `projection_retryable`
(requeue with `retryDelay`, default 30s, bounded by `maxAttempts`, default 3).

The fix wraps all three `Write` dispatch return paths with
`WrapRetryableNeo4jError`. This is a correctness change to the error *type* on an
already-occurring failure path; it does not change Cypher shape, statement
batching, transaction scope, phase order, worker counts, leases, or the retry
classifier. The grouped-atomic conformance flag
(`ESHU_NORNICDB_CANONICAL_GROUPED_WRITES`) is intentionally left at its
documented default (`false`, phase-group path); enabling it would batch MERGE
and retract DELETE into one mixed group that `isRetryableGraphWriteGroupError`
correctly refuses to retry, which would make dead-lettering worse and would
require loosening retry classification — explicitly a non-goal of #2247.

No-Regression Evidence: backend NornicDB/Neo4j shared Cypher contract,
input shape = canonical materialization (repository + directory + file + entity
phases), conflict domain = canonical `uid` MERGE under concurrent projector
workers. `go test ./internal/storage/cypher ./internal/projector
./internal/storage/postgres -count=1` → 1771 passed (2026-06-22). New regression
`TestCanonicalNodeWriterWritePropagatesRetryable` drives all three dispatch
paths (atomic_group, phase_group, sequential) with a `*TransactionExecutionLimit`
and asserts `projector.IsRetryable(Write(...)) == true`; it fails before the fix
(bare error → dead-letter) and passes after.
`TestCanonicalNodeWriterWriteKeepsTerminalErrorsTerminal` proves a
`ConstraintValidationFailed` schema error stays non-retryable (no classifier
loosening). No graph-write throughput change: same statements, same batching,
same transactions; only the error wrapper on the failure return path changed.

Observability Evidence: the change preserves all existing canonical-write
telemetry — the `telemetry.SpanCanonicalWrite`/`SpanCanonicalRetract` spans,
  the `eshu_dp_neo4j_deadlock_retries_total` retry counter (with bounded
  `write_phase` and `reason` labels) in `RetryingExecutor`, the
  `recordAtomicWrite`/`recordAtomicFallback`
counters, and the per-phase failure `slog.WarnContext("canonical phase failed",
...)`. The operator-visible improvement is queue-side: a transient canonical
write now surfaces as queue `failure_class = projection_retryable` with a bounded
requeue rather than a terminal `projection_failed` dead letter, so dead-letter
count and `attempt_count` exposed by the projector queue now distinguish
transient backpressure from real terminal failures.

## #4893 — uid-anchored TAINT_FLOWS_TO edge and CodeTaintEvidence node retracts

`CodeInterprocEvidenceWriter.Retract*ByUIDs` and
`CodeTaintEvidenceWriter.Retract*ByUIDs` replace the unanchored
`(:Function)-[rel]->(:Function) WHERE rel.<prop>` / `MATCH (n:CodeTaintEvidence)
WHERE n.scope_id ... WITH n LIMIT` scans with `UNWIND $uids MATCH (…{uid})`
indexed point-lookup deletes. The caller enumerates uids from the reducer-owned
ledgers (see `go/internal/reducer/AGENTS.md` #4893) and passes them in; empty
uids is a no-op (existence guard). The retract WHERE predicate
(`scope_id`/`evidence_source`/`generation_id`) is preserved for correctness — the
uid anchor is only the fast seed.

Performance Evidence: NornicDB v1.1.10 `d97f02c1`, 511,825 Function nodes; the
unanchored edge retract read 18.57 s (count 0) vs 0.03 s (100 uids) / 1.6 s
(2000 uids) anchored; live stack NornicDB CPU 150–509% → 0.55–3.17% idle,
stale-cleanup cycle 13055.6 s → 0.05 s. Full evidence in
`go/internal/reducer/AGENTS.md` (#4893).

No-Observability-Change: the new `*ByUIDs` methods flow through the existing
`Executor`/`GroupExecutor` dispatch, `Statement` phase/label/summary metadata,
retry wrapping, and failure logging; no new metric, label, worker, queue domain,
lease, runtime knob, or graph-write route.

### #4893 retract dispatch route (NornicDB v1.1.9 bolt ExecuteWrite bug)

The five value-flow by-uid retract methods route their DELETE statements
through `dispatchRetract` (sequential `Executor.Execute`, i.e. the reducer's
`session.Run` autocommit path), NOT through `dispatch`/`ExecuteGroup`. NornicDB
v1.1.9 (the version `docker-compose.yaml` pins) has a bolt bug:
`session.ExecuteWrite`/`tx.Run` returns `rels-deleted=0` for an
`UNWIND ... MATCH (s {uid})-[rel:TYPE]->() ... DELETE rel` statement inside an
explicit transaction, while the identical statement deletes correctly via
`session.Run` (autocommit) and HTTP `tx/commit`. `UNWIND`, `IN`, and
`IN`-on-relationship-property all work in isolation over bolt; only
DELETE-via-matched-relationship inside `ExecuteWrite` is affected. The MERGE
write path keeps using `ExecuteGroup` (works, and needs the atomic batch). Do
NOT route these retracts back through `ExecuteGroup` — the CI guard
`TestCode(Interproc|Taint)EvidenceRetractByUIDsRoutesThroughAutocommitExecute`
fails if you do. Tracked upstream as the NornicDB bolt ExecuteWrite follow-up.

No-Regression Evidence: DSN-gated bolt integration tests
(`code_evidence_bolt_retract_test.go`, `ESHU_CYPHER_BOLT_DSN`) reproduce
red (post-retract edge/node count unchanged) against a live NornicDB v1.1.9
and prove green after the fix (count -> 0); a full two-generation reducer E2E
on v1.1.9 confirms a dropped cross-function taint flow's TAINT_FLOWS_TO edge is
retracted while the survivor is kept and the ledger is pruned. The no-backend
CI guard above catches a dispatch-route regression without a live backend.

No-Observability-Change: the fix only changes the dispatch route (Execute vs
ExecuteGroup) for retract statements; same Cypher, same parameters, same
`Statement` metadata, same retry wrapping (`WrapRetryableNeo4jError`). No metric,
label, worker, queue domain, lease, runtime knob, or graph-write route added.

## #4900 — Count-gated orphan sweep write skip

The reducer's orphan sweep (`GraphOrphanSweepRunner` → `OrphanSweepStore.SweepOrphanNodes`)
now gates every write statement (clear/mark/sweep) on a cheap count query whose
predicate mirrors that write's own `MATCH...WHERE` exactly, so a write is issued
only when it will mutate at least one row:

- `mark` is gated on `BuildCountUnmarkedOrphanNodesQuery` (evidence-bearing,
  unmarked, zero-relationship nodes) — NOT the total orphan count — so an
  already-marked orphan set does not reissue a zero-row mark write.
- `clear` is gated on `BuildCountMarkedRelinkedNodesQuery` (marked AND relinked)
  — NOT marker presence alone — so a freshly marked, still-disconnected orphan
  does not reissue a zero-row clear write until it ages out (codex #4955).
- `sweep` is gated on the existing `buildCountAgedOrphanNodesQuery` (aged marked
  orphans).

`BuildCountMarkedOrphanNodesQuery` (marker presence) is a cheap short-circuit:
when zero nodes carry the marker, clear and sweep cannot match, so both are
skipped without issuing their count reads. The steady no-orphan state therefore
runs exactly two cheap reads (marker-presence + total orphans) and issues zero
write transactions. The mark/sweep/clear Cypher write shapes are byte-identical.
A new `Skipped map[string]int64` field on `OrphanSweepResult` reports the number
of write statements skipped per label (0..3).

No-Regression Evidence: the failing-then-green tests prove output-preserving
correctness and every zero-row skip path:
- `TestOrphanSweepStoreSkipsAllWritesWhenNothingToDo` — all-zero counts → 0 executor calls
- `TestOrphanSweepStoreRunsMarkWhenOrphansPresentButSkipsClearSweepWhenNoMarkers` — mark only
- `TestOrphanSweepStoreRunsClearAndSweepWhenMarkersPresent` — clear+sweep only
- `TestOrphanSweepStoreSkipsClearWhenMarkedButNotRelinked` — codex #4955: marked-but-idle → 0 calls
- `TestOrphanSweepStoreSkipsMarkWhenOrphansAlreadyMarked` — already-marked orphans → mark skipped
- `TestBuildCountMarkedOrphanNodesQueryIsLabelScopedAndBounded`,
  `TestBuildCountUnmarkedAndMarkedRelinkedQueriesMirrorWritePredicates` — builder contracts
- `TestOrphanSweepStoreUsesInjectedClockAndBoundsMutations` — existing test still passes
All existing `OrphanSweep` tests green (including the bounded-batch convergence test).
`go test ./internal/storage/cypher ./internal/reducer ./cmd/reducer -count=1` green.
`golangci-lint run ./internal/storage/cypher/... ./internal/reducer/...` clean.

Performance Evidence: prove-theory-first + wall-clock, measured on the live
drained `e2e3586persist` full-corpus stack (NornicDB v1.1.10 `d97f02c1`, ~980k
nodes / 1.6M edges; queue `succeeded|13034`, nothing in flight, so no
concurrent-write contention). Cardinality on the live graph: File 137,402,
Directory 42,493, EvidenceArtifact 3,157, Repository 896, Module 316,
Platform 24. Measured over Bolt with the same `neo4j-go-driver/v5` the reducer
uses. The cost is a ~14s FIXED per-write-transaction overhead independent of
label size — a real `mark`/`sweep`/`clear` write on Module (316 nodes) and
Platform (24 nodes) each cost ~14–18s, the same as File (137k) — because a
label `MATCH` inside a NornicDB write/temporal transaction routes
`executeSet → executeMatch → loadNodesWithTemporalViewport →
GetNodesByLabelVisibleAt → iterateNodesVisibleAtInTxn` (`badger_mvcc.go:1429`),
a full-store MVCC visible-at iteration (live CPU pprof during a zero-row write:
47% cum in `GetNodesByLabelVisibleAt`/`iterateNodesVisibleAtInTxn`, 34% cum in
`gcBgMarkWorker`/`gcDrain`+`tryDeferToSpanScan` decoding each node). The count
queries run on the cheaper read path (File count ~2.2s). BEFORE: the live
reducer logged 8 consecutive orphan-sweep cycles at `duration_seconds` ~270s
each, all finding 0 orphans and issuing 18 write transactions (6 labels ×
clear+mark+sweep) × ~14s ≈ 252s. AFTER: the count-gated sequence (measured
end-to-end over Bolt on the same live graph, all labels markedCount=0 /
orphanCount=0) issues 0 write transactions and completes in 5.82s
(File reads 4.47s, Directory 1.21s, rest sub-0.1s) — ~46x faster.
Output-preserving: with 0 orphans and 0 marked nodes the OLD writes matched 0
rows (no-ops), so the graph is byte-identical (verified 0 nodes carry the
`eshu_orphan_observed_at_unix` marker before and after); when orphans exist the
writes run exactly as before. Result class: Wall-clock win. A full built-binary
hot-swap re-drain was not performed to avoid disrupting the live production
reducer; the Bolt-sequence measurement exercises the same backend, graph, and
driver and captures the entire NornicDB-side cost driver (the reducer's Go
orchestration around these statements is negligible relative to the ~270s).

Skipping a 0-row write is a provable no-op preserving graph truth: a clear with
no marked nodes changes nothing, a mark with no orphans changes nothing, and a
sweep with no aged marked nodes changes nothing. The existing count queries
(`BuildCountOrphanNodesQuery`, `buildCountAgedOrphanNodesQuery`) plus the new
`BuildCountMarkedOrphanNodesQuery` are all cheap read-path queries that avoid
the ~14s NornicDB write-path fixed cost.

Observability Evidence: the `Skipped` field is exposed in the existing
"graph orphan sweep cycle completed" log line via `slog.Int64("writes_skipped_total", ...)`
and `slog.Any("skipped_by_label", ...)`. These reuses the existing reducer run
spans and `eshu_dp_graph_orphan_nodes` metric; no new metric instrument, metric
label, span, route, graph query shape, queue table, worker, lease, or runtime
knob is added.

## #5652 — UNWIND bare-MATCH SET silent write-loss (posture writers)

Confirmed live on the pinned production NornicDB v1.1.11 image: an
`UNWIND $rows AS row MATCH (n:Label {uid: row.uid}) SET ...` statement with no
`MERGE` anywhere in it silently drops its `SET` — reports success, the
property never persists. All four AWS posture node writers
(`ec2_internet_exposure`, `ec2_block_device_kms_posture`, `rds_posture`,
`s3_internet_exposure`) shipped exactly this shape. Fixed by switching the
anchor to `MERGE` plus a separate existence read
(`posture_node_existence.go`, `PostureExistenceReader`,
`filterRowsToExistingCloudResourceUIDs`) that confirms a candidate uid exists
before the row ever reaches the write, preserving each writer's never-create
contract without relying on `MATCH` to enforce it. `unwind_bare_match_set_gate_test.go`
statically fails any future reintroduction of the bare shape. Live proof:
`posture_node_writers_live_test.go` (`ESHU_CYPHER_BOLT_DSN`-gated). Full
per-writer analysis, live before/after evidence, and two related-but-distinct
NornicDB write-loss shapes found but not fixed in the same change (a
`WITH`-chained multi-clause edge-MERGE no-op in `canonicalNodeFileUpdateExistingCypher`,
and an `UNWIND`-batched `DELETE` no-op in the retract/refresh statements) are
in `docs/internal/evidence/5652-nornic-bare-match-writeloss.md`.

## #5623 — MATCHES_STATE edge retract must run on delta cycles too

`terraformStateMatchesConfigEdgeRetractStatements`
(`tfstate_state_match_edge_retract.go`, #5443 P1) used to skip on
`mat.FirstGeneration || mat.DeltaProjection`, copying
`terraformStateResourceRetractStatements`' own `DeltaProjection` skip without
re-deriving whether that reasoning transferred. It does not: the node-level
retract is an ungated DETACH DELETE over the whole current-label population
minus this generation, which really would mass-delete every resource a delta
cycle did not touch if it ran unguarded. The edge-level retract's
`s.generation_id = $generation_id` anchor already restricts it to state
resources upserted THIS exact generation (the node upsert always runs before
this retract, on every cycle) — a resource a delta cycle did not touch keeps
an OLDER generation_id and never matches that anchor, so this retract has no
mass-deletion exposure at all and is safe on delta cycles.

The practical effect of the copied guard: `terraformStateMatchesConfigEdgeStatements`'
MERGE (the edge WRITE) has no `DeltaProjection` guard and runs every cycle. So
when a state resource's resolved `OwningRepoID` changed to a DIFFERENT repo on
a delta cycle, the new edge was written immediately but the retract that
would delete the old edge was skipped, leaving the state resource with
`MATCHES_STATE` edges to two different repos simultaneously until the next
full reconciliation generation (hours away per
`ESHU_REPO_RECONCILE_INTERVAL_HOURS`). `go/internal/query/infra_scope_grant.go`'s
scoped-token infra predicate (#5623, landed in the same PR that surfaced this
finding in review) admits a `TerraformStateResource` via a `MATCHES_STATE`
inline-map disjunct that assumes at most one such edge; during the leak
window it admitted the resource for EITHER repo's scoped grant, including the
repo that no longer owns it — a genuine tenant-visibility leak reproduced live
through the real `CanonicalNodeWriter.Write` pipeline (not a raw seeded
fixture), not merely a theoretical gap.

Fix: removed the `DeltaProjection` skip, keeping only the `FirstGeneration`
skip (nothing can be stale before any prior generation ever wrote an edge).

No-Regression Evidence: the Cypher statement itself
(`canonicalTerraformStateMatchesConfigEdgeRetractCypher`) is byte-identical;
only the Go condition deciding whether to emit it changed. The already-passing
non-delta (full reconciliation) path is unchanged.

Live proof (RED via `git apply -R` on the fix, confirmed FAIL for the right
reason; GREEN restored), both run through the real write pipeline across a
full generation then a delta-cycle ownership reassignment against an isolated
NornicDB (`timothyswt/nornicdb-cpu-bge:v1.1.11`):
- `TestCanonicalNodeWriterRetractsStaleMatchesStateEdgeOnDeltaCycleLive`
  (this package): after the delta-cycle reassignment, the stale edge is gone
  and the new edge exists; a same-generation retry (partial-failure
  simulation) is idempotent.
- `TestLiveInfraScopeShapeMatchesStateStaleEdgeExcludedAfterDeltaReassignment`
  (`go/internal/query`): after the same real pipeline sequence, the scoped
  predicate admits only the current owner's grant, not the former owner's.

Both tests deliberately run every write back-to-back with NO interleaved read
between them, matching this package's existing #5443 live-test precedent
(`TestCanonicalNodeWriterRetractsStaleMatchesStateEdgeLive`): the pinned local
NornicDB image can silently drop a write that follows an interleaved read on
the same node within one test process. An earlier draft of the delta-cycle
test read between generation 1 and generation 2's writes and produced a false
failure from this exact defect, not from the retract logic — moving all reads
to a single block after every write fixed it.

Unit proof: `TestTerraformStateMatchesConfigEdgeRetractStatementsRunsUnderDeltaProjection`
replaces the old `...SkipsUnderDeltaProjection` test (which pinned the buggy
behavior); `TestTerraformStateMatchesConfigEdgeRetractStatementsSkipsOnFirstGeneration`
and `...RunsOnNonDeltaGeneration` are unchanged and still pass.

No-Observability-Change: the retract is a Cypher WHERE/DELETE fragment with no
span, metric, label, or log surface; no new telemetry signal is added or
needed.

## #5623 P1 — the delta-cycle retract fix above wiped a still-valid edge on a resolver hiccup

Follow-up review finding on the #5623 fix immediately above. That fix's
`s.generation_id = $generation_id` anchor proves "this generation upserted the
node," which is NOT the same fact as "we know its correct owner this cycle."
`TerraformStateOwnershipResolver.ResolveOwningRepoID` fails closed on ANY
resolver error -- an ordinary transient Postgres timeout or pool exhaustion,
not only a genuine "no owner" -- and every `cmd/*` wiring site
(`cmd/bootstrap-index`, `cmd/ingester`, `cmd/projector`'s
`terraform_state_ownership.go`) treats that identically to "no owner,"
returning `row.OwningRepoID == ""`. The state resource's node still gets
upserted that cycle regardless (row presence in `mat.TerraformStateResources`
drives the upsert, independent of whether ownership resolved). So a resolver
hiccup on a delta cycle for a resource whose node was still upserted: the
retract's generation-only anchor could not distinguish "ownership genuinely
changed" from "we simply failed to learn it this cycle" and deleted the
existing, still-correct `MATCHES_STATE` edge either way; nothing rewrote it
(`terraformStateMatchesConfigEdgeStatements`' MERGE already excludes
`OwningRepoID == ""` rows for the identical reason). Fail-closed
(under-authorization, never a leak) but a real accuracy regression, on every
delta cycle instead of only full-reconciliation cycles.

Fix: `terraformStateMatchesConfigEdgeRetractStatements` now filters
`mat.TerraformStateResources` to rows with `OwningRepoID != ""`, collects
their uids, batches by `w.batchSize`, and adds `AND s.uid IN $uids` to
`canonicalTerraformStateMatchesConfigEdgeRetractCypher`'s WHERE clause. A row
whose ownership did not resolve this cycle is excluded from the uid set, so
its existing edge (correct or not) survives untouched -- symmetric with the
MERGE's own exclusion. Batching mirrors
`terraformStateResourceMigrationStatements`' existing uid-batching precedent
(same file family, `tfstate_canonical_writer_retract.go`) rather than
inventing a new shape.

No-Regression Evidence: narrows the retract's candidate set to a strict subset
of what it touched before (only rows with resolved ownership); never widens
it. The resolved-ownership path (the common case, and the #5623 P0 fix's own
delta-cycle-reassignment scenario) is unaffected in statement count or shape.
Two pre-existing package tests needed fixture updates because they
constructed rows with no `OwningRepoID` and asserted the OLD unconditional
retract count:
`TestTerraformStateMatchesConfigEdgeRetractStatementsRunsUnderDeltaProjection`
and `...RunsOnNonDeltaGeneration` now seed a resolved `OwningRepoID` and assert
the `uids` parameter; `TestCanonicalNodeWriterBuildsTerraformStateStatements`
(`tfstate_canonical_writer_test.go`) drops from 8 to 7 expected statements
(its fixture intentionally wires no ownership resolver) and its stale doc
comment claiming the edge retract was "unconditional...a harmless no-op" is
corrected -- that claim was exactly the P1 bug.

Live proof (RED via `git apply -R` on this fix alone, keeping the #5623 P0
delta-cycle fix applied; confirmed FAIL for the right reason; GREEN restored),
run through the REAL `CanonicalNodeWriter.Write` pipeline across a full
generation then a delta cycle where ownership resolution returns not-ok (not a
raw seeded fixture):
`TestCanonicalNodeWriterPreservesMatchesStateEdgeOnResolverHiccupDeltaCycleLive`.
Re-ran the P0 delta-cycle-reassignment live regression
(`TestCanonicalNodeWriterRetractsStaleMatchesStateEdgeOnDeltaCycleLive`) and
its `internal/query` counterpart
(`TestLiveInfraScopeShapeMatchesStateStaleEdgeExcludedAfterDeltaReassignment`)
alongside this fix to confirm both directions: P1 closed, P0 not reopened --
all pass together. Unit proof:
`TestTerraformStateMatchesConfigEdgeRetractStatementsExcludesUnresolvedOwnershipRows`
(all-unresolved emits nothing; mixed resolved/unresolved includes only the
resolved uid).

No-Observability-Change: the retract is a Cypher WHERE/DELETE fragment with no
span, metric, label, or log surface; no new telemetry signal is added or
needed.

## #5623 P1 follow-up — NoOwner/AmbiguousOwner must retract, not preserve

Follow-up review finding on the #5623 P1 fix immediately above. That fix
excluded any row whose `OwningRepoID` was empty this cycle from the retract's
uid set, reasoning it might be a transient resolver failure. But THREE
distinct outcomes all leave `OwningRepoID` empty, and only one of them is
transient:

- a genuine resolver hiccup (Postgres timeout, pool exhaustion) -- correctly
  excluded, preserve the edge;
- `tfstatebackend.ErrNoConfigRepoOwnsBackend` -- an AUTHORITATIVE "no owner"
  answer, wrongly excluded;
- `tfstatebackend.ErrAmbiguousBackendOwner` -- an AUTHORITATIVE "not uniquely
  owned" answer, wrongly excluded.

The `(string, bool)` shape `TerraformStateOwnershipResolver.ResolveOwningRepoID`
used could not distinguish these. A backend that previously resolved to a
repo and later became unowned or ambiguous kept that repo's `MATCHES_STATE`
edge indefinitely -- the #5623 P0 tenant-visibility leak, reintroduced through
a narrower door.

Fix: the interface now returns `(repoID string, outcome
projector.TerraformStateOwnershipOutcome)` -- a four-value enum (Resolved,
TransientFailure [zero value], NoOwner, AmbiguousOwner) defined in
`internal/projector/tfstate_ownership_outcome.go`.
`projector.TerraformStateResourceRow` gained a matching `OwnershipOutcome`
field, set by the same enrichment pass that sets `OwningRepoID`
(`resolveTerraformStateOwnership`, this package). The retract's uid filter
changed from `row.OwningRepoID == ""` to
`row.OwnershipOutcome == projector.TerraformStateOwnershipTransientFailure` --
only the truly-unknown case is excluded now; Resolved, NoOwner, and
AmbiguousOwner are all retract-eligible.

The classification logic (mapping a `*tfstatebackend.Resolver` result to the
outcome enum) is centralized in the NEW package
`internal/relationships/tfstatebackend/canonicalwriter`
(`ResolveOwningRepoIDOutcome`), not duplicated across
`cmd/{bootstrap-index,ingester,projector}`'s three near-identical adapters as
it was before -- each adapter is now a one-line delegate. The new package
exists specifically because `internal/projector` (owner of the outcome enum)
already transitively imports `internal/relationships/tfstatebackend` (via
`internal/reducer` -> `internal/correlation/drift/tfconfigstate`), so
`tfstatebackend` cannot import `projector` back without a cycle -- confirmed
by an actual `go build` failure while developing this fix, and independently
reproduced by the #5623 P1 follow-up review (a throwaway import reproduced
the identical `import cycle not allowed` chain). This keeps the
`TerraformStateOwnershipResolver` interface itself (`tfstate_state_match_edge.go`,
this package) scoped to `projector` types only -- it does not import
`tfstatebackend`, and `canonicalwriter` adds ZERO new transitive edge from
this package to `tfstatebackend` beyond what already existed.

Precision matters on that last point: `internal/storage/cypher` ALREADY
depends on `tfstatebackend` transitively through a DIFFERENT, PRE-EXISTING,
unrelated path this fix did not create -- `edge_writer.go` (this package)
imports `internal/reducer`, and `internal/reducer` imports `tfstatebackend`
directly (for example `terraform_config_state_drift.go`, wiring the
drift-correlation resolver `cmd/reducer/wiring_handlers.go` already uses).
`go list -deps ./internal/storage/cypher` shows
`github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend` in the
output for that reason, predating this branch entirely. Do not read that as
a regression this change introduced.

No-Regression Evidence: widens the retract's candidate set from "resolved
rows only" to "resolved, no-owner, or ambiguous-owner rows" (narrower than
the original pre-#5623-P1 "every row" set, since transient failures are still
excluded). Every `cmd/*` adapter, the fake test resolver
(`fakeTerraformStateOwnershipResolver`, this package), and every hand-built
`TerraformStateResourceRow` fixture across `internal/storage/cypher`,
`internal/query`, and `internal/replay/offlinetier` that sets `OwningRepoID`
directly needed a matching `OwnershipOutcome` value -- the zero value
(`TransientFailure`) would otherwise silently exclude a fixture row its test
intends to be retract-eligible. Proof (failing-first, RED via a temporary
one-line revert of the filter back to `row.OwningRepoID == ""`, confirmed
FAIL for the right reason with rows 1-2 unaffected; GREEN restored): unit
`TestTerraformStateMatchesConfigEdgeRetractStatementsIncludesAuthoritativeNonOwnerRows`
(NoOwner and AmbiguousOwner both produce a retract-eligible uid) plus
`internal/relationships/tfstatebackend/canonicalwriter`'s own four-outcome
unit suite. Live (real `CanonicalNodeWriter.Write` pipeline, no raw seeded
fixture, isolated NornicDB v1.1.11):
`TestCanonicalNodeWriterRetractsMatchesStateEdgeOnAuthoritativeNonOwnerDeltaCycleLive`
(both subtests) run together with the #5623 P0/P1 siblings
(`TestCanonicalNodeWriterRetractsStaleMatchesStateEdgeOnDeltaCycleLive`,
`TestCanonicalNodeWriterPreservesMatchesStateEdgeOnResolverHiccupDeltaCycleLive`)
and the #5443 originals -- all pass in one run. `internal/query`'s
`TestLiveInfraScopeShapeMatchesStateFormerOwnerExcludedOnAuthoritativeNonOwner`
proves the scoped-token predicate itself no longer authorizes the former
owner for both outcomes, run alongside
`TestLiveInfraScopeShapeMatchesStateStaleEdgeExcludedAfterDeltaReassignment`.
Also `go test ./internal/storage/cypher ./internal/query ./internal/projector
./internal/relationships/... ./internal/replay/... ./cmd/ingester
./cmd/projector ./cmd/bootstrap-index -count=1`.

No-Observability-Change: the retract is a Cypher WHERE/DELETE fragment with no
span, metric, label, or log surface; the new adapter package logs a warning
for a genuine transient failure only, reusing the exact log line every prior
per-adapter copy already emitted -- no new signal.

AmbiguousOwner can itself be a byproduct of eventually-consistent ingestion,
not just a genuinely contested backend, and this fix now retracts on it every
cycle instead of only at full reconciliation -- worth stating explicitly
rather than leaving it implicit (#5623 P1 follow-up review, third finding).
`PostgresTerraformBackendQuery.ListTerraformBackendsByLocator`
(`internal/storage/postgres/tfstate_backend_canonical.go`) joins each
candidate `terraform_backends` fact through
`scope.active_generation_id = fact.generation_id` -- i.e. it only sees a
repo's CURRENTLY-ACTIVE generation, and every repo re-ingests independently
and asynchronously. During a real backend-ownership migration (state moved
from repo A to repo B -- teams do this), if repo A's next ingestion (which
would drop its now-stale `terraform_backends` declaration) lags repo B's
(which picks up the new one), the resolver observes BOTH as active claimants
for one or more cycles and returns `ErrAmbiguousBackendOwner` -- not because
ownership is contested, but because ingestion has not yet converged. Under
this fix that transient ambiguity retracts the existing edge (repo A's
authorization to this resource is now under-authorized, not extended, and
repo B does not gain it yet either, since the MERGE never fires while
`OwningRepoID` is empty), and the edge -- and the scoped-token visibility it
gates -- flaps until both repos' ingestion converges and a single unambiguous
owner emerges.

This is judged acceptable, not a defect to fix here, for three reasons: (1) it
fails safe in the same direction every prior fix in this chain converged on --
under-authorization during the flap, never a cross-tenant leak, since neither
the stale nor the new owner is over-admitted at any point in the sequence; (2)
the flap window is bounded by ordinary delta-ingestion cadence for two
actively-developed repos (commit-triggered, typically minutes), not by
`ESHU_REPO_RECONCILE_INTERVAL_HOURS` (default 24h, the window the ORIGINAL
#5623 P0 leak was bounded by) -- the window this fix can introduce is tighter
than the window the fix it replaces left open; (3) it self-heals with no
operator action: the next cycle that reprocesses this state resource after
BOTH repos' active generations agree resolves cleanly to Resolved and the
correct edge reappears, the same self-healing property TransientFailure
already relies on. No code change is warranted for this alone -- if it proves
operationally noisy in practice (repeated retract-then-reappear on the SAME
backend across many cycles), that is a signal for a follow-up, not evidence
this fix is wrong.

## #6142 — three of a backend restart's transients were terminal

A restart's refused commit (`TransactionCommitFailed` / `...Writes are blocked,
possibly due to DropAll or Close`) and its two `Statement.SyntaxError` shapes
(`...reading node: DB Closed`, `UNWIND MERGE chain relationship create failed:
start node ... does not exist`) were all terminal, so one restart dead-lettered
three intents as `projection_bug` at attempt 1. Both `SyntaxError` shapes share
a code with a malformed query, so each guard takes code AND message. Record:
[evidence-6142-backend-restart-transient-classification.md](evidence-6142-backend-restart-transient-classification.md).
