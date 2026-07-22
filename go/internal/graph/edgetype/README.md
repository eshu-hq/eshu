# edgetype

## Purpose

`edgetype` is the central registry of Cypher graph relationship (edge) types.
Every statically-named edge type Eshu writes or reads — `DEPENDS_ON`, `CALLS`,
`WRITES_TO`, `RUNS_IN`, `CAN_ASSUME`, and the rest — is named once here as a typed
`EdgeType` constant. Before this package, edge-type names were scattered as raw
string literals inside Cypher templates and as ad hoc per-writer Go constants,
with no compile-time guard against typos or write/read divergence.

## Ownership boundary

This package owns the canonical list of statically-known graph edge-type
strings and the membership/enumeration helpers over them. It does NOT own node
labels (those live in `internal/graph`), the relationship-resolution evidence
model (`internal/relationships`), or the data-driven cloud relationship
families `AWS_*` / `GCP_*` and the observability coverage family, which are
synthesized from collector row data at runtime and cannot be enumerated as
constants.

## Exported surface

- `EdgeType` — a string type whose value is the exact Cypher relationship type.
- One constant per registered edge type (e.g. `DependsOn`, `Calls`, `RunsIn`).
- `All() []EdgeType` — defensive copy of every registered edge type.
- `IsRegistered(string) bool` — O(1) membership check.

See `doc.go` for the godoc-rendered contract.

## Dependencies

None. This is a leaf package with no internal imports, so graph writers
(`internal/storage/cypher`), reducer materialization (`internal/reducer`),
query read paths (`internal/query`), and `internal/relationships` can all import
it without cycles.

## Telemetry

None. This package emits no metrics, spans, or logs; it is a pure constant
registry.

## Gotchas / invariants

- An `EdgeType` constant's string value is the graph-wire contract. Changing a
  value is a breaking change: it silently invalidates stored edges and every
  reader matching the old type. `TestEdgeTypeStringParity` pins each value.
- Cypher cannot parameterize a relationship type (`-[:$x]->` is illegal on both
  NornicDB and Neo4j), so inline literals still appear in Cypher templates.
  `TestNoUnregisteredEdgeLiteral` scans all production Go for relationship-type
  literals and fails CI if any names an edge type not registered here.
- Adding a new edge type means adding the constant, the `registered` entry, and
  the parity-table row together; the parity test enforces lockstep. Additive
  edge types also need their writer, reader, and replay/retraction contracts
  updated in the owning packages.

## Related docs

- `docs/public/reference/cypher-performance.md` — hot-path Cypher rules.
