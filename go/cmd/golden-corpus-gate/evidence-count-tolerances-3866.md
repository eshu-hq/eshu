# Evidence — required node/edge count tolerances (#3866 criterion 3)

Scope: `go/cmd/golden-corpus-gate/graph.go` + `evaluate.go`. The
`verify-performance-evidence` gate flags `graph.go` because it contains the gate's
scalar-count Cypher (`MATCH (n:Label) RETURN count(n)` etc.). This change does
**not** touch any Cypher, query shape, write path, or concurrency.

## What changed

`evaluateNodeCount` / `evaluateEdgeCount` gained a `required bool`; `checkGraph`
passes `required=true` in the full-corpus (`-graph-required-only=false`) count
loops so the snapshot's node/edge count tolerances are asserted as **required**
instead of advisory. The Cypher executed by `boltGraphCounter` is byte-for-byte
unchanged — the same `CountNodes`/`CountEdges` scalar counts run; only the
*Finding.Required* flag on the result changed.

## No-Regression Evidence:

The query workload against the graph backend is identical: the gate already ran
every `node_counts`/`edge_counts` Cypher count in advisory mode; this change only
reclassifies the resulting findings as blocking. No new query, no extra round
trip, no change to cardinality, batch size, or transaction scope. Full
golden-corpus gate green with the required tolerances: **94 pass, 0 required-fail,
0 advisory-warn** (elapsed 37s, budget ceiling 1800s) — versus 85 pass / 9
required-fail before the snapshot ranges were calibrated to the real corpus.

## No-Observability-Change:

`golden-corpus-gate` is an offline CI assertion binary, not a runtime service; it
emits no metrics, spans, or status. Its operator-facing output is the
pass/required-fail report it already prints (`Report.Write`). This change adds no
runtime stage and alters no telemetry contract.
