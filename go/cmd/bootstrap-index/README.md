# cmd/bootstrap-index

## Purpose

`cmd/bootstrap-index` builds `eshu-bootstrap-index`, the one-shot runtime that
collects repositories, projects source-local graph/content rows, backfills
relationship evidence, materializes IaC reachability, reopens deployment
mapping, and enqueues Terraform config-vs-state drift work.

## Ownership boundary

This command owns bootstrap orchestration and wiring for an empty or recovered
environment. It is not a steady-state service, does not mount the shared admin
surface, and must keep the same collection, projector, graph writer, queue, and
fact contracts used by long-running runtimes.

## Exported surface

The package exports no API. The public process contract is `--version`/`-v`,
environment-driven configuration, one-shot execution, and exit after the
collector/projector queues and post-collection passes complete. See `doc.go`
for the godoc summary.

## Dependencies

Bootstrap wiring combines `internal/collector`, `internal/projector`,
`internal/relationships`, `internal/iacreachability`, `internal/runtime`,
`internal/storage/postgres`, `internal/storage/cypher`, and
`internal/telemetry`. Postgres remains the durable boundary between collection,
projection, and reducer follow-up.

## Telemetry

The runtime uses `telemetry.NewBootstrap("bootstrap-index")`, records
`SpanCollectorObserve` and `SpanProjectorRun`, reports queue-claim and
projector-run metrics, records GOMEMLIMIT, and can expose pprof through
`ESHU_PPROF_ADDR`. Post-collection failures log phase-specific failure classes
for relationship backfill, IaC reachability, deployment-mapping reopen, and
drift enqueue.

## Gotchas / invariants

- Bootstrap ordering is facts-first: collect/project, backfill relationship
  evidence, materialize IaC reachability, reopen deployment mapping, then
  enqueue drift follow-up.
- Projector work superseded by a newer same-scope generation must not ack stale
  graph truth.
- `ESHU_PROJECTION_WORKERS` controls projector parallelism; default is bounded
  by CPU and capped. Do not reduce it to hide write conflicts.
- The graph writer must use the same filtering and NornicDB phase-group policy
  as steady-state projection.
- `ESHU_DISCOVERY_REPORT` is diagnostic output, not a runtime truth source.

## Focused tests

```bash
cd go
go test ./cmd/bootstrap-index -run 'Test.*Version|Test.*NornicDB|Test.*Projector|Test.*Worker|Test.*Wiring' -count=1
go test ./cmd/bootstrap-index -count=1
```

Docs-only edits should also pass the package-doc verifier and `git diff --check`.

## Related docs

- `docs/public/deployment/service-runtimes.md`
- `docs/public/reference/local-testing.md`
- `docs/public/architecture.md`
- `go/internal/collector/README.md`
- `go/internal/projector/README.md`
