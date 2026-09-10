# Visualization Leaf — Evidence (#6060 naming follow-up)

Moves the graph-query visualization packet builder out of `codequery/` into
the new `visualization/` leaf per the #6618 naming rules (rule 3: split
glued compounds into nested directories): `packet.go` (with its
`packet_test.go`), reached from the single call site in
`codequery/cypher.go` through `visualization.BuildGraphQueryVisualizationPacket`.
The `(*CodeHandler).handleVisualizeQuery` handler stays in `codequery/`,
pinned by its `query-source-coverage.yaml` entry.

## Performance and observability

No-Regression Evidence: behavior-preserving by construction. The builder
body is byte-identical apart from its package clause; the call-site line is
a qualifier change only. Package tests green: query, codequery,
visualization, queryplan.

No-Observability-Change: no span, metric, tracer, or pprof identifier
is added or renamed.
