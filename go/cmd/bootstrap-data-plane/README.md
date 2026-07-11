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
which records that the backend/fingerprint pair completed successfully and
which older writer fingerprints, if any, remain explicitly compatible. The
binary writes no application data and does not stay resident.

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
- ESHU_DEFER_CONTENT_SEARCH_INDEXES — defaults to false. When true, applies the
  Postgres schema without the two content substring trigram indexes so a
  guaranteed subsequent `bootstrap-index` run can build them after the cold
  content load. It never drops existing indexes and must not be enabled for a
  deployment topology that omits bootstrap-index finalization.
- ESHU_GRAPH_BACKEND — `neo4j` or `nornicdb`
- ESHU_GRAPH_SCHEMA_STATEMENT_TIMEOUT — per graph DDL statement timeout,
  default `2m`
- ESHU_GRAPH_SCHEMA_ADOPT_EXISTING — controls existing-schema adoption when the
  Postgres graph schema marker is missing. Unset defaults to opportunistic
  adoption for NornicDB and disabled adoption for Neo4j. Truthy values require
  inspection support for either backend. False values disable adoption. Adoption
  inspects `SHOW CONSTRAINTS` and `SHOW INDEXES`; if every current schema object
  already exists, it marks the backend/fingerprint as applied and skips the DDL
  pass. The inspection uses the ESHU_GRAPH_SCHEMA_STATEMENT_TIMEOUT budget.
- NEO4J_URI, NEO4J_USERNAME, NEO4J_PASSWORD
- DEFAULT_DATABASE

Invalid ESHU_GRAPH_BACKEND values fail with `unsupported graph backend for
schema`. Invalid or non-positive ESHU_GRAPH_SCHEMA_STATEMENT_TIMEOUT values fail
before any DDL runs. When existing-schema adoption runs, schema inspection
errors fail closed instead of falling back to live DDL.

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
  fingerprint in Postgres. A later run with the same fingerprint skips graph
  DDL instead of asking NornicDB to re-check every constraint/index against a
  populated graph. Graph-writing runtimes read the latest marker before opening
  writer adapters and refuse when their compiled fingerprint is neither exact
  nor listed in `compatible_fingerprints`.
- rolling upgrades are exact-match by default. An additive schema change may
  populate `compatible_fingerprints` with older writer fingerprints that remain
  safe. A destructive schema change leaves that list empty so stale writers
  stop at startup instead of failing during graph writes.
- existing-schema adoption is automatic for NornicDB when the Postgres marker is
  missing. It is for deployments that already have the complete graph schema but
  lack the Postgres fingerprint marker, such as upgrades from older chart/image
  combinations. It compares the expected schema object names to
  `SHOW CONSTRAINTS` and `SHOW INDEXES` before writing the marker. Set
  `ESHU_GRAPH_SCHEMA_ADOPT_EXISTING=false` only when an operator intentionally
  wants to force the live DDL pass.
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
'TestRun(SkipsGraphSchemaWhenLatestFingerprintAlreadyApplied|SkipsGraphSchemaWhenLatestMarkerListsFingerprintCompatible|AppliesAndMarksGraphSchemaWhenFingerprintMissing|DoesNotMarkGraphSchemaAfterApplyFailure|AdoptsExistingNornicDBGraphSchemaByDefaultWhenMarkerMissing|CanDisableDefaultNornicDBGraphSchemaAdoption|DoesNotDefaultAdoptNeo4jGraphSchema|AdoptsExistingGraphSchemaWhenMarkerMissing|AppliesGraphSchemaWhenAdoptionFindsMissingObjects|FailsWhenAdoptionInspectionFails)'`
proves same-fingerprint restarts skip graph DDL, missing markers still apply and
record graph schema completion with compatibility metadata, and failed graph DDL
never records a successful marker. The adoption cases prove complete existing
NornicDB graph schema marks and skips by default, operators can disable that
default, Neo4j keeps its existing default DDL path, missing graph schema falls
back to DDL, and failed schema inspection does not blindly run DDL against a
live graph. `go test ./internal/graphschemacompat -count=1` proves writer
startup rejects missing or incompatible latest markers and accepts exact or
explicitly compatible fingerprints. `go test ./internal/graph -run
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

Performance Evidence: on 2026-05-23, a hosted EKS schema bootstrap against a
retained NornicDB graph without a matching Postgres marker completed
successfully but took 3h52m because repeated `CREATE CONSTRAINT ... IF NOT
EXISTS` statements spent roughly 77s-102s each refreshing existing constraint
state. Default NornicDB adoption changes that marker-missing retained-graph path
to two bounded schema metadata reads plus one marker write when all expected
objects already exist.

Observability Evidence: `bootstrap.graph.skipped` tells operators that a
preserved-volume restart reused the recorded graph schema fingerprint.
`bootstrap.graph.adopted` and `bootstrap.graph.adoption_incomplete` expose the
backend, schema fingerprint, expected object count, actual object count, and a
bounded missing-object sample before any adoption decision is made.
Graph-writing startup refusals surface through the existing
`runtime.startup.failed` log with the backend, expected fingerprint, latest
applied fingerprint, and operator guidance. Focused schema-progress and
statement-timeout tests cover the structured per-statement logs and the
bootstrap-level timeout wrapper that operators use to see which graph schema
statement is slow or blocked.

## Related docs

- [Service runtimes — Schema Bootstrap](../../../docs/public/deployment/service-runtimes.md#schema-bootstrap)
- [Docker Compose deployment](../../../docs/public/run-locally/docker-compose.md)
- [Helm deployment](../../../docs/public/deploy/kubernetes/index.md)
- [CLI reference](../../../docs/public/reference/cli-reference.md)
