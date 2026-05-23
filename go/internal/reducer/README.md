# internal/reducer

## Purpose

`internal/reducer` owns cross-domain materialization after source-local facts
are committed. It admits evidence into reducer truth, writes canonical graph
rows through storage ports, publishes reducer-owned facts, coordinates shared
projection, and repairs graph phase publication.

## Ownership boundary

This package decides graph and reducer-fact truth for cross-source domains. It
does not collect sources, parse files, commit intake facts, serve query routes,
or call graph drivers directly. Graph writes go through
`internal/storage/cypher`; durable fact, queue, phase, and repair storage is
wired by command packages through narrow interfaces.

Before changing a domain, trace raw fact evidence to admitted candidate,
projected row, graph or fact write, phase publication, and API/MCP read truth.

## Exported surface

See `doc.go` and `go doc ./internal/reducer` for the contract. Use
`DefaultDomainDefinitions`, `NewDefaultRegistry`, and `NewDefaultRuntime` for
the wired domain set instead of duplicating a domain catalog in docs.

## Dependencies

Reducer code consumes fact envelopes and storage/query ports from
`internal/facts`, `internal/storage/postgres`, `internal/storage/cypher`, and
`internal/query`. Parser, collector, and projector packages feed evidence;
command packages provide concrete adapters.

## Telemetry

Reducer diagnostics use `SpanReducerRun`, `SpanCanonicalWrite`,
`SpanReducerDriftEvidenceLoad`,
`SpanReducerAWSRuntimeDriftEvidenceLoad`, queue wait/run metrics, shared
projection metrics, graph phase repair metrics, and domain counters. When a
path is slow, classify the cost as fact load, extraction, intent upsert, graph
write, phase publication, shared projection, or repair before changing worker
counts or timeouts.

## Gotchas / invariants

- Generation supersession is a write barrier. Stale intents must not write
  graph rows or reducer facts.
- Graph writes and `graph_projection_phase_state` publication are not atomic;
  keep graph phase repair wired.
- Shared projection only runs after the required readiness phase exists.
- Domains that consume `resolved_relationships` need a reopen or re-trigger
  after deployment mapping is reopened.
- Do not serialize worker lanes to hide non-idempotent writes. Fix the
  conflict key, retry path, or write design.
- Ambiguous code, SQL, package, image, service-catalog, and cloud evidence stays
  provenance-only until explicit evidence admits truth.
- Type/class references are `REFERENCES`, not `CALLS`; SQL trigger-bound
  functions keep `EXECUTES` reachability.

## Focused tests

```bash
cd go
go test ./internal/reducer -count=1
go test ./cmd/reducer -count=1
go run ./cmd/eshu docs verify ../go/internal/reducer --limit 1000 \
  --fail-on contradicted,missing_evidence
```

## Related docs

- `docs/public/architecture.md`
- `docs/public/reference/local-testing.md`
- `docs/public/reference/telemetry/index.md`
- `docs/public/reference/dead-code-reachability-spec.md`
- `docs/public/reference/relationship-mapping.md`
