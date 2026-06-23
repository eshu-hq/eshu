# Evidence: Dual-Write Reconciliation (#3559)

Scope: `generation_reconciliation.go` (dual-write Postgres↔graph drift
classifier, convergence anchors, convergence metric/log) and the
`ReconciliationConvergence` counter in `go/internal/telemetry/instruments.go`.
Design narrative: `docs/internal/design/3559-dual-write-reconciliation.md`.

No-Regression Evidence: this change adds a backend-neutral, side-effect-free
classifier (`ClassifyReconciliationDrift`), one `Int64` counter
(`eshu_dp_reconciliation_convergence_total`), and one log emitter
(`LogReconciliationReport`). It adds no hot-path Cypher, graph write, worker
claim, lease, or batch change. Classification is `O(edges)` over a prebuilt
`map[AcceptanceIdentity]AuthoritativePostgresGeneration` lookup (one map build,
one map probe per edge) with no graph scan and no Cypher. Convergence reuses the
existing `EdgeWriter.RetractEdges` path unchanged via the domain-specific
`RepairAnchors` (repository id / scope id / file path) the retract builder
already keys on, so the projection write path keeps its current cost and the
existing retract Cypher shape is untouched. Backend/version: backend-neutral
(no backend call in this code; NornicDB/Neo4j unchanged). Input shape: per
reconciliation pass, one `AuthoritativePostgresGeneration` per exact
`(scope_id, acceptance_unit_id, source_run_id)` identity and one
`GraphDenormalizedEdge` per denormalized edge under audit; row/terminal counts
are the caller's audited edge set, bounded by the caller. Measurement:
`go test ./internal/storage/cypher ./internal/telemetry -count=1 -race` →
694 passed (20 reconciliation tests), including exact-identity no-collapse and
RepairAnchors-drives-real-retract proofs; `golangci-lint run
./internal/storage/cypher/...` → no issues. Why safe: the classifier cannot
mutate the graph; only `RepairAnchors` output (validated, non-empty,
domain-correct retract keys) reaches the existing retract path.

Observability Evidence: `eshu_dp_reconciliation_convergence_total` with bounded
labels `domain` and `drift_kind` (`in_sync` / `stale_generation` /
`orphan_resolved_id`) lets an operator query stranded-edge counts per drift
class after a generation swap; in-sync is recorded so a pass that ran and
converged is distinguishable from a pass that never ran.
`LogReconciliationReport` emits one structured line per pass — info on
convergence, warn (alert-surfacing) with a bounded per-domain sample of drifted
edge keys when stranded edges remain. Both are covered by
`generation_reconciliation_test.go` (counter values per `drift_kind`; warn vs
info log level; converged flag). The existing
`eshu_dp_reconciliation_drift_retractions_total` continues to count the actual
graph deletes performed when the convergence retract runs.
