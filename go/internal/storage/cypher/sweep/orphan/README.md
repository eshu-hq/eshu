# Graph Orphan Sweep

## Purpose

This package detects and cleans up disconnected graph nodes the writers can
leave behind: relationship-created repository targets, inferred platform
nodes, repository relationship evidence, files, directories, and imported
modules. One sweep cycle per label runs two bounded reads (S1 candidates, S2
connected keys), computes orphans as a Go-side anti-join, then issues only
the key-anchored clear/mark/sweep writes whose key sets are non-empty.
Steady state issues two reads and zero writes per label.

## Ownership boundary

The leaf owns the sweep builders, the anti-join, paging cursors, and the
closed label set. The parent `cypher` package owns the `Executor` and
`Statement` types the store executes through. External wiring
(`cmd/reducer/graph_orphan_sweep_wiring.go`) owns scheduling, leases, and
the post-sweep fingerprint invalidation.

## Exported surface

- `OrphanSweepStore` / `NewOrphanSweepStore` — cycle runner
  (`SweepOrphanNodes`, `GraphOrphanNodeCounts`)
- `OrphanSweepReader` — bounded read seam (implemented by Bolt executors)
- `OrphanSweepPolicy` / `OrphanSweepResult` — per-cycle bounds and outcome
- `OrphanSweepLabel`, `DefaultOrphanSweepLabels` — closed label set
- `BuildCandidateOrphanNodesQuery`, `BuildConnectedKeysQuery`,
  `BuildClearOrphanMarkerStatement`, `BuildMarkOrphanNodesStatement`,
  `BuildSweepOrphanNodesStatement` — the S1–S5 builders

See `doc.go` for the package contract.

## Dependencies

- `internal/storage/cypher` (parent) — `Executor`, `Statement`
- Live-bolt tests gate on `ESHU_CYPHER_BOLT_DSN`/`ESHU_NEO4J_URI` and skip
  without a backend.

## Telemetry

None. The store executes through the caller's executor; cycle outcomes are
logged and metered by the reducer maintenance runner, not here.

## Gotchas / invariants

- EvidenceArtifact orphans are defined by the missing source edge, not full
  disconnection, so S5 detaches there and deletes plain elsewhere.
- Repository sweep is the only label whose swept keys are reported
  (`DeletedRepositoryIDs`) for fingerprint invalidation.
- The S2 connected-keys read is chunked at
  `defaultOrphanSweepConnectedKeysChunkSize` (500): the anchored UNWIND read
  scales super-linearly with key-list size on the pinned NornicDB backends
  (5,000 keys ~4.7s unchunked vs ~570ms chunked — see
  `../../evidence-5147-orphan-sweep-antijoin.md`).
- The TOCTOU guard re-verifies connectivity for exactly the keys about to be
  deleted, immediately before the delete.

## Performance Evidence

No-Regression Evidence: pure Pattern-B leaf move from the `cypher` root
(#6694 slice). All 30 emitted statements (S1/S2 reads and S3/S4/S5 writes
across the six labels, plus sanitize/profile allowlist probes) are
byte-identical to the pre-move root versions, proven by the RED/GREEN
template dump diff recorded in the move PR (empty diff). Backend execution
shape is unchanged.

No-Observability-Change: the store still executes through the caller's
`Executor`/`OrphanSweepReader`; this package adds no metric name, label,
worker, queue domain, knob, backend branch, or new graph-write route.

## Related docs

- `docs/public/reference/cypher-performance.md` (chunking measurement)
- `docs/public/reference/nornicdb-pitfalls.md` (relationship-existence predicates)
- `go/internal/storage/cypher/README.md`
