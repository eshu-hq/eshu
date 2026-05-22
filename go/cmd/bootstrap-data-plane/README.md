# bootstrap-data-plane

## Purpose

`cmd/bootstrap-data-plane` wires the `eshu-bootstrap-data-plane` binary. It
applies Eshu's Postgres schema and configured graph-backend schema, records the
graph schema fingerprint, and exits before long-running runtimes start.

## Ownership boundary

The command owns startup config, telemetry/bootstrap logging, Postgres
bootstrap invocation, graph backend opening, strict schema application,
per-statement graph DDL deadlines, schema fingerprint adoption/marking, and
idempotent one-shot exit behavior.

It does not ingest repositories, drain queues, project facts, or run API/MCP
services.

## Exported surface

This is a `main` package. Use `go doc -cmd ./cmd/bootstrap-data-plane` for the
package contract. Maintainer-facing surfaces are `run`, schema adoption helpers,
graph schema marker helpers, config parsing, and command tests.

## Dependencies

- `internal/storage/postgres` applies fact-store, queue, content, and audit DDL.
- Graph runtime/storage packages open Neo4j or NornicDB.
- `internal/graph` applies strict backend schema.
- `internal/runtime` supplies config helpers.
- `internal/telemetry` supplies startup logs and service identity.

## Telemetry

Structured logs report startup, Postgres schema applied, graph schema skipped,
graph schema applied, and startup failure events. Graph DDL statements run under
a per-statement deadline so failures name the stuck phase instead of waiting for
outer Compose or Kubernetes timeouts.

## Gotchas / invariants

- `--version` and `-v` must exit before opening stores.
- Postgres DDL runs before graph DDL; long-running services depend on both.
- Graph schema markers let preserved-volume restarts skip already-applied graph
  DDL only after the fingerprint has been verified or applied.
- Existing-schema adoption is automatic for NornicDB when the Postgres marker is
  missing. `ESHU_GRAPH_SCHEMA_ADOPT_EXISTING=false` forces live DDL.
- Adoption compares expected schema object names to `SHOW CONSTRAINTS` and
  `SHOW INDEXES`; failed adoption must not mark the schema as applied.
- DDL must remain idempotent. This command is safe as a Kubernetes Job or
  Compose `db-migrate` service.

## Evidence kept here

No-Regression Evidence: `go test ./cmd/bootstrap-data-plane -count=1` covers
Postgres and graph schema application, backend selection, marker skip/adoption
behavior, timeout validation, and error joining.

Observability Evidence: `bootstrap.graph.skipped`,
`bootstrap.graph.applied`, `bootstrap.graph.adopted`,
`bootstrap.graph.adoption_incomplete`, and startup-failure logs expose whether
startup skipped, adopted, applied, or failed graph schema work.

## Focused tests

```bash
go test ./cmd/bootstrap-data-plane -count=1
go doc -cmd ./cmd/bootstrap-data-plane
```

## Related docs

- `docs/public/deployment/service-runtimes.md`
- `docs/public/run-locally/docker-compose.md`
- `docs/public/deploy/kubernetes/helm-quickstart.md`
- `docs/public/reference/cli-reference.md`
