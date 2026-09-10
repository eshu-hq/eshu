# metrics

The call-graph metrics leaf of the code family (`codequery`).

## What lives here

One hot read: `CallGraphMetricsEdgesCypher` runs a single indexed
`(source:Function)-[CALLS]->(target:Function)` pass bounded by
`CallGraphMetricsEdgeScanLimit` (one extra row past it, so callers can
tell an exact scope from an overflowed one). Three readers share its
exact text: the hub-function metrics, the recursive-function metrics,
and the graph-summary packet's hot-entity ranking.

## What stays in `codequery`

`(*CodeHandler).CallGraphMetricsData` and `handleCallGraphMetrics` stay
in `codequery/graph_metrics.go` — methods must live in their type's
package, and the data method keeps its queryplan `source_sha256` pin.
Both call this leaf through thin forwarders. `codequery` also keeps the
`CallGraphMetricsEdgeScanLimit` const alias so every existing caller
(root seam, tests) keeps its spelling.

## Invariants

- One builder text for all callers. Every route binds the caller's
  grant before the read, so a grant predicate in the builder would be
  redundant and would fork the query plan. Keep `cypher_sha256` in
  `handler-hot-cypher.yaml` describing what production emits.
- This package never imports `codequery` or root `query` (import cycle).
- `CallGraphMetricsEdgeScanLimit` is the single source of truth for the
  bound; `codequery` and root alias it, never redeclare it.
