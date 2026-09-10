# Metrics Leaf — Evidence (#6060 naming follow-up)

Moves the call-graph metrics edge pass out of `codequery/` into the new
`metrics/` leaf per the #6618 naming rules (rule 3: split glued
compounds into nested directories): `call_graph_metrics.go` builder to
`metrics/edges.go`, with the thin `*CodeHandler` methods collected in
`codequery/graph_metrics.go` (a `metrics.go` there trips the dirgate
file/dir stutter rule) and the bound kept single-sourced as
`metrics.CallGraphMetricsEdgeScanLimit` with const aliases outward.

## Performance and observability

No-Regression Evidence: behavior-preserving by construction. The
builder body is byte-identical (digest calculator reproduces the
manifest `source_sha256` on the moved file); both methods keep
byte-identical bodies (coverage digest unchanged, suite green); both
hot-cypher entries repath file keys only with `cypher_sha256`
untouched. Anchor (`Function{repo_id}`), `LIMIT $edge_scan_limit`,
and the 50001-edge overflow sentinel are unchanged. Package tests
green: query, codequery, deadcode, metrics (no test files), queryplan.

No-Observability-Change: no span, metric, tracer, or pprof identifier
is added or renamed; span attribute names and the capability string
are untouched.
