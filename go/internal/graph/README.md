# Graph

## Purpose

`graph` owns the source-local graph write contract, Cypher statement seam, batch
write builders, deletion mutations, and graph schema bootstrap statements used
by projectors, reducers, and schema bootstrap.

## Ownership boundary

This package owns source-local materialization values, safe Cypher builders, and
backend-aware schema statement selection. It does not own drivers, connection
pools, retry classification, query telemetry, or canonical shared-write
orchestration.

Backend differences belong in schema dialect helpers and label translation.
Callers should not branch on graph brand to build ordinary writes.

## Exported surface

Use `doc.go` and `go doc ./internal/graph` for the exported contract. The
durable surface covers `Writer`, `Materialization`, `Record`, `Result`,
`CypherStatement`, `CypherExecutor`, batch merge helpers, deletion mutations,
and schema helpers.

## Dependencies

`graph` imports only the Go standard library. `CypherStatement` and
`CypherExecutor` live here to avoid an import cycle with
`internal/storage/cypher`.

## Telemetry

This package does not register metrics or spans. Schema execution emits
structured progress logs with backend, phase, statement ordinal, total,
duration, bounded statement summary, and failure class. Backend executors own
query duration and error metrics.

## Gotchas / invariants

- Dynamic labels and property keys must pass safe identifier checks before
  generated Cypher is built.
- Entity batches split UID identity from name/path/line identity so each MERGE
  shape can use the intended indexes.
- Relationship batches require one source label, target label, and relationship
  type per call.
- NornicDB does not receive Neo4j-style composite uniqueness constraints from
  this package; supported UID and lookup indexes carry that path, including
  OCI registry and package-registry projection labels that keep deployment
  trace and package reads anchored.
- Strict schema bootstrap fails after non-context DDL failures and fails fast on
  context cancellation or deadline exhaustion.
- Repository reset preserves the `Repository` node; repository delete removes
  it.

## Related docs

- `docs/public/architecture.md`
- `docs/public/reference/graph-backend-operations.md`
- `docs/public/reference/nornicdb-tuning.md`
- `go/internal/storage/cypher/README.md`
