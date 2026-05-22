# cmd/reducer

## Purpose

`cmd/reducer` builds `eshu-reducer`, the long-running resolution-engine
runtime. It opens Postgres and the configured graph backend, wires
`internal/reducer` with concrete storage adapters, runs reducer and shared
projection workers, and hosts the shared admin surface.

## Ownership boundary

This package owns process startup, configuration parsing, adapter wiring,
profile gates, and shutdown. It does not own reducer domain behavior, graph
truth decisions, queue schema, or public query handlers.

Invalid `ESHU_GRAPH_BACKEND` values fail startup. Backend-specific behavior
belongs in command wiring or storage/cypher seams, not in domain handlers.

`buildReducerService` wires reducer domain adapters, shared projection runners,
code-call and repo-dependency runners, graph phase repair, queue policy, claim
gates, and telemetry. Domain contracts live in `internal/reducer`; this command
only provides process wiring.

## Exported surface

The command package exports no API. `main`, `buildReducerService`,
`openReducerNeo4jAdapters`, configuration helpers, and graph adapter wrappers
are unexported process wiring. See `doc.go` for the binary contract.

## Dependencies

- `internal/reducer` for the service, runtime, handlers, shared projection,
  code-call projection, repo-dependency projection, and graph phase repair.
- `internal/storage/postgres` for queues, facts, relationship stores, shared
  intents, phase state, repair queues, status, and graph-drain checks.
- `internal/storage/cypher` for instrumented graph execution and edge writers.
- `internal/runtime`, `internal/app`, and `internal/telemetry` for datastore
  setup, pprof, hosted admin routes, logging, metrics, and tracing.
- `internal/query` for graph backend/profile parsing and query ports needed by
  wiring adapters.

## Telemetry

Startup uses `telemetry.NewBootstrap("reducer")`, service/component
`reducer`, the default tracer/meter, `postgres.InstrumentedDB{StoreName:
"reducer"}`, and instrumented Cypher executors. Runtime signals include
`/healthz`, `/readyz`, `/metrics`, `/admin/status`, reducer queue depth,
domain run spans, shared projection metrics, graph phase repair metrics, and
domain counters.

Use `ESHU_PPROF_ADDR` only for focused profiling. Port-only values bind to
`127.0.0.1`.

## Gotchas / invariants

- `--version` and `-v` must exit before telemetry, storage, graph, pprof, or
  HTTP setup.
- Set either `ESHU_REDUCER_CLAIM_DOMAIN` or `ESHU_REDUCER_CLAIM_DOMAINS`, not
  both.
- NornicDB defaults use more reducer concurrency than Neo4j. Lower worker
  counts only with queue, conflict-key, and graph-write evidence.
- The local-authoritative NornicDB profile gates semantic-entity and code-call
  projection so graph write lanes do not compete unnecessarily.
- Keep `GraphProjectionPhaseRepairer` and reducer graph-drain wiring in place;
  they are scheduling and recovery paths, not alternate truth sources.
- `ESHU_NORNICDB_CANONICAL_GROUPED_WRITES` is conformance-only unless a tracked
  benchmark and observability note promotes it.

## Focused tests

```bash
cd go
go test ./cmd/reducer -run 'Test.*Config|Test.*Wiring|Test.*Claim|Test.*Backend|Test.*WorkloadDependency' -count=1
go test ./cmd/reducer ./internal/reducer -count=1
```

Docs-only edits should also pass the package-doc verifier and `git diff --check`.

## Related docs

- `docs/public/deployment/service-runtimes.md`
- `docs/public/reference/local-testing.md`
- `docs/public/reference/telemetry/index.md`
- `go/internal/reducer/README.md`
- `go/internal/storage/postgres/README.md`
