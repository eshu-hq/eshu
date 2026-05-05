# bootstrap-data-plane

## Purpose

`eshu-bootstrap-data-plane` applies all Postgres and graph-backend schema
DDL then exits. It decouples schema migration from data population so the
API, MCP, ingester, and reducer come up against an empty-but-valid schema
while `bootstrap-index` or `ingester` populates data.

## Ownership boundary

This binary owns DDL orchestration only. Postgres table definitions live in
`internal/storage/postgres/`. Graph schema bootstrap lives in
`internal/graph/` and is applied through `graph.EnsureSchemaWithBackend`.
The binary writes no application data and does not stay resident.

## Entry points

- `main` and `run` in `go/cmd/bootstrap-data-plane/main.go`
- single-process binary; no subcommands
- `eshu-bootstrap-data-plane --version` and `eshu-bootstrap-data-plane -v` print
  the build-time version through `buildinfo.PrintVersionFlag` before opening
  Postgres or the graph backend

## Configuration

Resolved through `runtime.OpenPostgres`, `runtime.OpenNeo4jDriver`, and
`runtime.LoadGraphBackend`:

- ESHU_POSTGRES_DSN
- ESHU_GRAPH_BACKEND — `neo4j` or `nornicdb`
- NEO4J_URI, NEO4J_USERNAME, NEO4J_PASSWORD
- DEFAULT_DATABASE

Invalid ESHU_GRAPH_BACKEND values fail with `unsupported graph backend for
schema`.

## Telemetry

Uses the shared telemetry bootstrap and the structured logger scoped to
`bootstrap`/component `bootstrap-data-plane`. No OTEL metric or trace
providers are registered. Lifecycle events use `telemetry.EventAttr`:
`runtime.startup.failed`, `bootstrap.schema.started`,
`bootstrap.postgres.applied`, `bootstrap.graph.applied` (with a
`graph_backend` attribute).

## Gotchas / invariants

- idempotent: every DDL statement is `CREATE ... IF NOT EXISTS`
- version probes are pre-startup checks; keep `buildinfo.PrintVersionFlag` at
  the top of `main` so init-container diagnostics do not run DDL
- graph driver close uses a 10-second timeout; close errors are joined into
  the run result via `errors.Join`
- exits non-zero if either Postgres or graph DDL fails; no partial apply
- `neo4jSchemaExecutor` runs DDL in a write session against the configured
  database name; do not point it at a read replica

## Related docs

- [Service runtimes — DB Migrate](../../../docs/docs/deployment/service-runtimes.md#db-migrate-schema-init-container)
- [Docker Compose deployment](../../../docs/docs/deployment/docker-compose.md)
- [Helm deployment](../../../docs/docs/deployment/helm.md)
- [CLI reference](../../../docs/docs/reference/cli-reference.md)
