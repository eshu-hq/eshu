# cmd/ingester

## Purpose

`cmd/ingester` wires the `eshu-ingester` binary. It owns repository sync,
parsing, fact emission, and source-local projection into the configured graph
backend for the long-running ingester runtime.

## Ownership boundary

The command owns runtime config, telemetry bootstrap, Postgres and graph writer
wiring, collector/projector composition, admin/status hosting, recovery route
mounting, pprof startup, webhook trigger handoff, and Kubernetes StatefulSet
runtime shape.

It does not parse languages, decide reducer-owned correlation truth, implement
storage adapters, or define graph write contracts.

## Exported surface

This is a `main` package. Use `go doc -cmd ./cmd/ingester` for the package
contract. Maintainer-facing surfaces are runtime wiring helpers, NornicDB writer
options, canonical writer setup, and `compositeRunner`.

## Dependencies

- Collector packages own Git sync, discovery, parsing inputs, and fact
  emission.
- `internal/projector` owns source-local projection and retry behavior.
- `internal/storage/cypher` owns canonical graph writer contracts.
- Runtime, status, telemetry, Postgres, Neo4j, and NornicDB packages are wired
  here through narrow seams.

## Telemetry

The binary exposes `/healthz`, `/readyz`, `/metrics`, `/admin/status`, and
`/admin/recovery`. It registers queue gauges and passes OTEL providers into
collector, projector, storage, and hosted runtime paths. Optional pprof starts
only when `ESHU_PPROF_ADDR` is set and binds port-only inputs to `127.0.0.1`.

## Gotchas / invariants

- `--version` and `-v` must exit before runtime setup.
- Local-authoritative NornicDB projector workers default to `NumCPU` unless
  explicitly configured.
- NornicDB phase grouping keeps retractions outside matching upsert groups and
  keeps directory/file/entity phases bounded.
- Entity phase concurrency is controlled by
  `ESHU_NORNICDB_ENTITY_PHASE_CONCURRENCY`; chunks inside one label group must
  remain disjoint by entity identity.
- Webhook trigger handoff still sends selected repositories through the normal
  Git sync and snapshot path.
- This is the only long-running runtime that mounts the workspace PVC in
  Kubernetes.

## Focused tests

```bash
go test ./cmd/ingester -count=1
go doc -cmd ./cmd/ingester
```

## Related docs

- `docs/public/architecture.md`
- `docs/public/deployment/service-runtimes.md`
- `docs/public/reference/local-testing.md`
- `docs/public/reference/telemetry/index.md`
- `docs/public/reference/nornicdb-tuning.md`
