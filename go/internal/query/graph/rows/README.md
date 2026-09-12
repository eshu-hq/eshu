# Graph-driver row and projection shaping

## Purpose

Decodes graph-driver result shapes and holds one shared Cypher projection
fragment for handler-family code that cannot import root or querycontract to
reach them:

- `GraphPathNodeProps` and `RouteToCallerEntityFromChain` decode a
  `nodes(path)` projection (a `neo4j.Node` on both backends, with a
  `map[string]any` fallback) into a plain property map or an entity-identity
  map.
- `GraphSemanticMetadataProjection` returns the shared Cypher RETURN-clause
  fragment for an entity's optional semantic metadata columns.

## Ownership boundary

This package owns exactly these three functions and their private
`lastChainNodeProps` helper. It does not own query orchestration, HTTP
handling, or the graph/content ports -- those stay in root and querycontract.

## Exported surface

`GraphPathNodeProps`, `RouteToCallerEntityFromChain`,
`GraphSemanticMetadataProjection`. See [doc.go](doc.go).

## Dependencies

The Go standard library, `github.com/neo4j/neo4j-go-driver/v5/neo4j` for the
`neo4j.Node` type, and `internal/query/querycontract` for `StringVal`/`IntVal`.

It is **not** in `querycontract`, for two different reasons:

- `GraphPathNodeProps` and `RouteToCallerEntityFromChain` type-switch on
  `neo4jdriver.Node`. `querycontract/AGENTS.md` forbids importing a graph
  driver there so the package stays dependency-neutral for every importer,
  including ones that never touch a driver.
- `GraphSemanticMetadataProjection` returns Cypher text. `querycontract`'s
  Cypher-fragment carve-out is narrow and deliberate: only
  `RepositoryAccessFilter`'s predicate builders and the inline-map grant
  primitives may emit query text there, because the grant bounds they encode
  are the contract. A field-projection fragment is not part of that seam.

Because this package imports the Neo4j driver, it is a named exception to
`internal/query`'s `query-no-graph-driver` depguard rule
(`go/.golangci.yml`), the same way `internal/query/impact/exposure_path_mapping.go`
already is.

## Move evidence (#6642 Part D)

This package renamed in place from `go/internal/query/querygraphrows` to
`go/internal/query/graph/rows` per `docs/internal/naming.md` rules 1 to 4:
`query` prefixed a subpackage already under `query/`, and the old
`graph_row_shape.go` repeated the directory chain in its file name, so it
is now `shape.go`. Nesting under a new `graph/` parent (rule 3) instead of
gluing a longer compound name gives the package's driver-aware seam a
directory of its own without inventing another fused identifier. What
differs from the old package: the package clause, the doc comment, this
doc trio, the file name, the eight importers' import paths and qualifiers,
and one local variable (`DirectionRows` in `codequery/routes/graph.go`
declared `rows` two lines before its only qualifier call, so that local is
now `records`; `listMostComplexFunctions` in `codequery/complexity_queries.go`
got the same rename for readability). The exported surface
(`GraphPathNodeProps`, `RouteToCallerEntityFromChain`,
`GraphSemanticMetadataProjection`) already led with `Graph`, not `Rows`, so
none of the three needed a rule-4 rename. No decoding, projection, or
Cypher-fragment behavior changed.

No-Regression Evidence (#6642 rename): the Cypher fragment
`GraphSemanticMetadataProjection` returns and the row-decoding helpers are
byte-identical to the `querygraphrows` originals; the four queryplan rows
whose recorded text carries the qualifier were re-pinned with class, count
and disposition unchanged, and `go test ./internal/query/... ./internal/queryplan/`
is green on the renamed tree. No query changed shape, so no benchmark delta
is claimed.

## Telemetry

No-Observability-Change (#6642 rename): the rename touches no span, metric,
or log name. This package emits no metric, span, or log of its own. It only
decodes rows and returns query text; the handlers that call it own their own
spans.

## Gotchas / invariants

**`GraphSemanticMetadataProjection`'s text is not tracked by the Cypher-hot
gate as a MATCH/RETURN statement of its own.** It contains no `MATCH`,
`RETURN`, or other statement keyword -- it is a bare column list meant for
interpolation into a caller's own `RETURN` clause -- so
`#6060 query-literals.py`'s tree-wide literal-multiset check does not select
it. Do not rely on that check to catch a change to this fragment; verify
callers directly instead.

**`GraphPathNodeProps`'s second return value distinguishes "no properties"
from "not a node shape."** A caller that ignores it and always uses the first
return value cannot fail closed on an unrecognized value.

## Related docs

- [Cypher performance](../../../../../docs/public/reference/cypher-performance.md)
- [Package restructure design](../../../../../docs/internal/design/package-restructure.md)
