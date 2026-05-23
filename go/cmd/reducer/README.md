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

## Exported surface

The command package exports no API. Its contract is the `eshu-reducer` process:
`--version` / `-v`, environment-derived configuration, reducer service wiring,
admin/status hosting, metrics, pprof opt-in, and clean shutdown.

## Dependencies

`internal/reducer` supplies service and domain logic. `internal/storage/postgres`
supplies queues, facts, relationship stores, shared intents, phase state,
repair queues, status, and graph-drain checks. `internal/storage/cypher`
supplies instrumented graph execution and edge writers. `internal/runtime`,
`internal/app`, and `internal/telemetry` supply datastore setup, pprof, hosted
admin routes, logging, metrics, and tracing. `internal/query` supplies backend
and profile parsing plus query ports needed by wiring adapters.

## Telemetry

Startup uses `telemetry.NewBootstrap("reducer")`, service/component
`reducer`, `postgres.InstrumentedDB{StoreName: "reducer"}`, and instrumented
Cypher executors. Runtime signals include `/healthz`, `/readyz`, `/metrics`,
`/admin/status`, reducer queue depth, domain run spans, shared projection
metrics, graph phase repair metrics, and domain counters. `ESHU_PPROF_ADDR` is
for focused profiling only; port-only values bind to `127.0.0.1`.

## Gotchas / invariants

- `--version` and `-v` exit before telemetry, storage, graph, pprof, or HTTP
  setup.
- Set either `ESHU_REDUCER_CLAIM_DOMAIN` or `ESHU_REDUCER_CLAIM_DOMAINS`, not
  both.
- NornicDB defaults use more reducer concurrency than Neo4j. Lower worker
  counts only with queue, conflict-key, and graph-write evidence.
- The local-authoritative NornicDB profile gates semantic-entity and code-call
  projection so graph write lanes do not compete unnecessarily.
- Keep graph phase repair and reducer graph-drain wiring in place; they are
  scheduling and recovery paths, not alternate truth sources.
- `ESHU_NORNICDB_CANONICAL_GROUPED_WRITES` remains conformance-only unless a
  tracked benchmark and observability note promote it.

## Focused tests

```bash
cd go
go test ./cmd/reducer -count=1
go run ./cmd/eshu docs verify ../go/cmd/reducer --limit 1000 \
  --fail-on contradicted,missing_evidence
```

## Related docs

- `docs/public/deployment/service-runtimes.md`
- `docs/public/reference/local-testing.md`
- `docs/public/reference/telemetry/index.md`
- `go/internal/reducer/README.md`
- `go/internal/storage/postgres/README.md`
