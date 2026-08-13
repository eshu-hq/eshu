# AGENTS.md - internal/graphschemacompat guidance for LLM assistants

## Read first

1. `README.md` - package purpose and compatibility contract.
2. `compatibility.go` - marker query and startup refusal logic.
3. `write_fence.go` - the same decision re-checked on the write path of a
   process that is already running.
4. `go/internal/graph/schema_application.go` - fingerprint and compatibility
   source.
5. `go/cmd/bootstrap-data-plane/main.go` - normal marker writer.
6. `go/cmd/bootstrap-index/graph_schema.go` - direct bootstrap-index
   marker-missing initializer.

## Invariants this package enforces

- **Postgres-only startup check** - do not add live graph schema inspection to
  writer startup. Graph object inspection belongs to schema bootstrap adoption.
  Direct bootstrap-index missing-marker recovery applies strict DDL; it does
  not inspect graph metadata.
- **Latest marker wins** - compatibility is evaluated against the latest marker
  row for the selected backend, not any historical row.
- **Explicit compatibility** - only an exact fingerprint match or a latest row
  listing the writer's fingerprint in `compatible_fingerprints` may pass.
- **A read failure is not a refusal** - `WriteFence` stops a write only on a
  marker it read successfully that says no. Do not make it fail closed on a
  query error: every canonical writer would stop together on one Postgres blip,
  and the startup check already admitted the process.

## Common changes and how to scope them

- **Add additive compatibility** - update the compatibility map in
  `internal/graph/schema_application.go`, add a test that the older writer
  fingerprint passes against the newer marker, and document the rolling-upgrade
  rule.
- **Change marker SQL** - update `compatibility.go`, the Postgres schema DDL,
  and `cmd/bootstrap-data-plane` marker writes together.

## Failure modes and how to debug

- Startup refusal with `graph schema marker missing` means
  `eshu-bootstrap-data-plane` has not completed against the same Postgres
  database. Direct `bootstrap-index` may recover by applying strict graph schema
  and writing the marker before it opens the projection writer.
- Startup refusal with `graph schema incompatible` means the latest schema
  marker is not exact and did not declare the writer fingerprint compatible.
- The same message on a *retrying* projector queue row, from a process that
  started fine, is the `WriteFence` refusing a write mid-flight: a schema
  application landed underneath this pod. The work stays queued for the pod that
  replaces it. The fence reaches only writers built with it, so a release older
  than the fence still has to be stopped before bootstrap records the marker.

## Anti-patterns specific to this package

- Do not query graph backend metadata from graph-writing pods.
- Do not accept any historical marker row after a newer incompatible marker
  exists.
- Do not log connection strings or graph URIs in compatibility errors.
