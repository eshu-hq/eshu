# graphschemacompat

## Purpose

`graphschemacompat` validates that a graph-writing runtime is starting against a
schema bootstrap marker that is safe for the writer compiled into the process.

## Ownership boundary

This package owns the marker read, marker write helper, and compatibility
decision. It does not apply graph DDL, inspect graph constraints, or open graph
drivers. Schema DDL stays in `internal/graph` and is normally applied by
`cmd/bootstrap-data-plane`; direct `bootstrap-index` startup may also apply the
same strict schema when the Postgres marker is missing. The durable marker is
stored in Postgres only after schema bootstrap succeeds.

## Exported surface

See `doc.go` for the godoc package contract.

- `RequireCompatibleForRuntime` loads `ESHU_GRAPH_BACKEND` and checks the
  latest Postgres marker for that backend.
- `RequireCompatible` checks a specific `graph.SchemaBackend`.
- `MarkApplied` records the successful bootstrap marker for a
  `graph.SchemaApplication`.
- `ErrMissingMarker` lets callers distinguish an absent marker from an
  incompatible latest marker without string matching.
- `Result` reports the expected fingerprint, latest applied fingerprint, and
  compatibility list used in the decision.

## Dependencies

- `internal/graph` provides the current schema fingerprint and compatibility
  policy.
- `internal/runtime` provides backend selection from environment.
- `internal/storage/postgres` provides the narrow `Queryer`, `Executor`, and
  `Rows` interfaces.

## Telemetry

This package emits no metrics or spans. Callers surface startup refusal through
their existing `runtime.startup.failed` structured log, and Postgres
instrumentation exposes marker-read and marker-write latency when callers pass
instrumented adapters.

## Gotchas / invariants

- Only the latest `graph_schema_applications` row for a backend is authoritative.
  Older successful fingerprints do not let stale writers keep running after an
  incompatible schema change.
- Compatibility is explicit. Additive schema changes may list older writer
  fingerprints as compatible only when the whole runtime remains version-safe;
  destructive changes and schema changes coupled to new reducer domains leave
  the list empty so old writers refuse before graph writes. The graph package
  pins the current fingerprint and compatibility decision in
  `TestSchemaApplicationsDeclareCompatibilityDecision` so future schema
  changes cannot silently roll without updating the compatibility contract.
- `MarkApplied` only records completion. Call it after strict graph DDL succeeds
  or after bootstrap adoption proves the existing graph schema is complete.
- The check reads Postgres only. It deliberately avoids `SHOW CONSTRAINTS` and
  `SHOW INDEXES` during steady-state pod startup.
- Direct bootstrap-index startup may recover from `ErrMissingMarker` by applying
  the strict graph schema. Long-lived graph writers should return the error and
  let deployment ordering run `eshu-bootstrap-data-plane`.

## Related docs

- `docs/public/deployment/service-runtimes-bootstrap.md`
- `docs/public/reference/environment-runtime-storage.md`
- `go/cmd/bootstrap-data-plane/README.md`
