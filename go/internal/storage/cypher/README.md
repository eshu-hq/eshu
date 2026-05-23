# Cypher Storage

## Purpose

`internal/storage/cypher` owns backend-neutral graph write contracts. It builds
canonical node writes, semantic entity writes, edge writes, retries, statement
chunking, and instrumentation for the shared Cypher/Bolt path used by NornicDB
and Neo4j-compatible deployments.

## Ownership Boundary

This package owns graph write shape and write-side instrumentation. It does not
own reducer admission, source-local projection decisions, query handlers,
backend process configuration, or schema bootstrap orchestration. Backend
differences belong in narrow seams here or in backend adapters, not in reducers
or handlers.

## Exported Surface

See `doc.go` and `go doc ./internal/storage/cypher`. The main surface includes
canonical writers, entity writers, edge writers, retrying and timeout executors,
instrumented executors, statement chunking, group statement profiles, and
backend retry classification.

## Telemetry

Instrumentation records Cypher execution, canonical phase duration, graph write
rows, retry attempts, edge summaries, and writer failure classes through
`internal/telemetry`. High-cardinality node IDs, file paths, and statements stay
out of metric labels.

## Gotchas / Invariants

- Writes must be idempotent across retries, duplicate work claims, and partial
  failures.
- NornicDB is the default backend; Neo4j is compatibility only through the
  shared Cypher contract.
- Hot-path query shape changes require same-shape before/after or
  no-regression evidence.
- Do not serialize worker paths or lower batch sizes to hide write races.
- Canonical node and edge labels must stay aligned with graph schema and
  reducer/projector expectations.

## Focused Tests

```bash
cd go
go test ./internal/storage/cypher -count=1
go run ./cmd/eshu docs verify ../go/internal/storage/cypher --limit 1000 \
  --fail-on contradicted,missing_evidence
```

## Related Docs

- `docs/public/reference/cypher-performance.md`
- `docs/public/reference/telemetry/index.md`
- `docs/public/reference/graph-backend-operations.md`
