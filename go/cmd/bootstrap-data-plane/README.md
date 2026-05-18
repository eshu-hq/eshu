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
- ESHU_GRAPH_SCHEMA_STATEMENT_TIMEOUT — per graph DDL statement timeout,
  default `2m`
- NEO4J_URI, NEO4J_USERNAME, NEO4J_PASSWORD
- DEFAULT_DATABASE

Invalid ESHU_GRAPH_BACKEND values fail with `unsupported graph backend for
schema`. Invalid or non-positive ESHU_GRAPH_SCHEMA_STATEMENT_TIMEOUT values fail
before any DDL runs.

## Telemetry

Uses the shared telemetry bootstrap and the structured logger scoped to
`bootstrap`/component `bootstrap-data-plane`. No OTEL metric or trace providers
are registered. Lifecycle events use `telemetry.EventAttr`:
`runtime.startup.failed`, `bootstrap.schema.started`,
`bootstrap.postgres.applied`, `bootstrap.graph.applied` (with a `graph_backend`
attribute). Graph DDL also emits one structured `graph schema statement
applying` and one terminal `graph schema statement applied` or
`graph schema statement failed` log per statement, including backend, phase,
statement index, statement total, duration, failure class, and a bounded schema
statement summary.

## Gotchas / invariants

- idempotent: every DDL statement is `CREATE ... IF NOT EXISTS`
- version probes are pre-startup checks; keep `buildinfo.PrintVersionFlag` at
  the top of `main` so deployment diagnostics do not run DDL
- each graph DDL statement runs under `ESHU_GRAPH_SCHEMA_STATEMENT_TIMEOUT`
  (default `2m`); context deadline or cancellation fails fast instead of
  continuing through the rest of the schema list
- graph driver close uses a 10-second timeout; close errors are joined into
  the run result via `errors.Join`
- exits non-zero if either Postgres or graph DDL fails; no partial apply
- `neo4jSchemaExecutor` runs DDL in a write session against the configured
  database name; do not point it at a read replica

No-Regression Evidence: focused unit coverage proves generic DDL errors still
log as warnings and continue, while context deadline/cancellation now stops
schema bootstrap after the failing statement instead of burning the whole schema
list.

Observability Evidence: focused schema-progress and statement-timeout tests
cover the structured
per-statement logs and the bootstrap-level timeout wrapper that operators use to
see which graph schema statement is slow or blocked.

## Related docs

- [Service runtimes — Schema Bootstrap](../../../docs/docs/deployment/service-runtimes.md#schema-bootstrap)
- [Docker Compose deployment](../../../docs/docs/deployment/docker-compose.md)
- [Helm deployment](../../../docs/docs/deployment/helm.md)
- [CLI reference](../../../docs/docs/reference/cli-reference.md)
