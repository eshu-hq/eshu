# AGENTS.md - internal/graphschemacompat guidance for LLM assistants

## Read first

1. `README.md` - package purpose and compatibility contract.
2. `compatibility.go` - marker query and startup refusal logic.
3. `go/internal/graph/schema_application.go` - fingerprint and compatibility
   source.
4. `go/cmd/bootstrap-data-plane/main.go` - normal marker writer.
5. `go/cmd/bootstrap-index/graph_schema.go` - direct bootstrap-index
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

## Anti-patterns specific to this package

- Do not query graph backend metadata from graph-writing pods.
- Do not accept any historical marker row after a newer incompatible marker
  exists.
- Do not log connection strings or graph URIs in compatibility errors.
