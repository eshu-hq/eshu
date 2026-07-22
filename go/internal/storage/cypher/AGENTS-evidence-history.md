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
