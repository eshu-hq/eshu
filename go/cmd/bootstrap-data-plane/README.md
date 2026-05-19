# bootstrap-data-plane

## Purpose

`eshu-bootstrap-data-plane` applies all Postgres and graph-backend schema
DDL then exits. It decouples schema migration from data population so the
API, MCP, ingester, and reducer come up against an empty-but-valid schema
while `bootstrap-index` or `ingester` populates data.

## Ownership boundary

This binary owns DDL orchestration only. Postgres table definitions live in
`internal/storage/postgres/`. Graph schema bootstrap lives in
`internal/graph/` and is applied through `graph.EnsureSchemaWithBackendStrict`
so any rejected DDL keeps the graph marker unset for the next retry.
The only row this binary writes is the Postgres graph schema application marker,
which records that the exact backend/fingerprint pair completed successfully.
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
- ESHU_GRAPH_SCHEMA_ADOPT_EXISTING — when truthy, inspect `SHOW CONSTRAINTS`
  and `SHOW INDEXES` before applying graph DDL. If every current schema object
  already exists, mark the backend/fingerprint as applied and skip the DDL pass.
  The inspection uses the ESHU_GRAPH_SCHEMA_STATEMENT_TIMEOUT budget.
- NEO4J_URI, NEO4J_USERNAME, NEO4J_PASSWORD
- DEFAULT_DATABASE

Invalid ESHU_GRAPH_BACKEND values fail with `unsupported graph backend for
schema`. Invalid or non-positive ESHU_GRAPH_SCHEMA_STATEMENT_TIMEOUT values fail
before any DDL runs. When ESHU_GRAPH_SCHEMA_ADOPT_EXISTING is truthy, schema
inspection errors fail closed instead of falling back to live DDL.

## Telemetry

Uses the shared telemetry bootstrap and the structured logger scoped to
`bootstrap`/component `bootstrap-data-plane`. No OTEL metric or trace providers
are registered. Lifecycle events use `telemetry.EventAttr`:
`runtime.startup.failed`, `bootstrap.schema.started`,
`bootstrap.postgres.applied`, `bootstrap.graph.applied`, and
`bootstrap.graph.skipped` (with `graph_backend`, `schema_fingerprint`, and
`statement_count` when graph DDL is skipped). Existing-schema adoption emits
`bootstrap.graph.adoption_incomplete` when objects are missing and
`bootstrap.graph.adopted` when the backend schema is complete enough to mark.
Graph DDL also emits one
structured `graph schema statement applying` and one terminal
`graph schema statement applied` or `graph schema statement failed` log per
statement, including backend, phase, statement index, statement total, duration,
failure class, and a bounded schema statement summary.

## Gotchas / invariants

- idempotent: every DDL statement is `CREATE ... IF NOT EXISTS`
- after a successful graph schema apply, the binary marks the backend and schema
  fingerprint in Postgres. A later run with the same fingerprint skips graph DDL
  instead of asking NornicDB to re-check every constraint/index against a
  populated graph.
- existing-schema adoption is explicit opt-in. It is for deployments that
  already have the complete graph schema but lack the Postgres fingerprint
  marker, such as upgrades from older chart/image combinations. It compares the
  expected schema object names to `SHOW CONSTRAINTS` and `SHOW INDEXES` before
  writing the marker.
- version probes are pre-startup checks; keep `buildinfo.PrintVersionFlag` at
  the top of `main` so deployment diagnostics do not run DDL
- each graph DDL statement runs under `ESHU_GRAPH_SCHEMA_STATEMENT_TIMEOUT`
  (default `2m`); context deadline or cancellation fails fast instead of
  continuing through the rest of the schema list
- graph driver close uses a 10-second timeout; close errors are joined into
  the run result via `errors.Join`
- exits non-zero if either Postgres or graph DDL fails; no partial apply. The
  graph marker is written only after every graph DDL statement succeeds.
- `neo4jSchemaExecutor` runs DDL in a write session against the configured
  database name; do not point it at a read replica

No-Regression Evidence: `go test ./cmd/bootstrap-data-plane -run
'TestRun(SkipsGraphSchemaWhenFingerprintAlreadyApplied|AppliesAndMarksGraphSchemaWhenFingerprintMissing|DoesNotMarkGraphSchemaAfterApplyFailure|AdoptsExistingGraphSchemaWhenMarkerMissing|AppliesGraphSchemaWhenAdoptionFindsMissingObjects|FailsWhenAdoptionInspectionFails)'`
proves same-fingerprint restarts skip graph DDL, missing markers still apply and
record graph schema completion, and failed graph DDL never records a successful
marker. The adoption cases prove complete existing graph schema marks and skips,
missing graph schema falls back to DDL, and failed schema inspection does not
blindly run DDL against a live graph. `go test ./internal/graph -run
TestEnsureSchemaWithBackendStrictReturnsStatementFailures` proves the strict
data-plane path returns a non-context schema failure instead of warning and
continuing to the marker write. Existing focused unit coverage proves generic
DDL errors still log as warnings for non-strict callers, while context
deadline/cancellation stops schema bootstrap after the failing statement instead
of burning the whole schema list.
Remote Compose preserved-volume restart evidence on 2026-05-19 logged
`bootstrap.graph.skipped` with `statement_count=209`, restarted the stack in
about 2m38s, and left projector/reducer queues terminal with no failed,
retrying, or dead-letter rows.

Observability Evidence: `bootstrap.graph.skipped` tells operators that a
preserved-volume restart reused the recorded graph schema fingerprint.
`bootstrap.graph.adopted` and `bootstrap.graph.adoption_incomplete` expose the
backend, schema fingerprint, expected object count, actual object count, and a
bounded missing-object sample before any adoption decision is made. Focused
schema-progress and statement-timeout tests cover the structured per-statement
logs and the bootstrap-level timeout wrapper that operators use to see which
graph schema statement is slow or blocked.

## Related docs

- [Service runtimes — Schema Bootstrap](../../../docs/docs/deployment/service-runtimes.md#schema-bootstrap)
- [Docker Compose deployment](../../../docs/docs/deployment/docker-compose.md)
- [Helm deployment](../../../docs/docs/deployment/helm.md)
- [CLI reference](../../../docs/docs/reference/cli-reference.md)
