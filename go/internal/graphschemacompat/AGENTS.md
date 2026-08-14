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
  query error: every fenced writer would stop together on one Postgres blip,
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
  replaces it.

## What the write fence actually covers

Narrower than "canonical writers", so check this list before an identity cutover
leans on it. Only `CanonicalNodeWriter` accepts a fence
(`CanonicalNodeWriter.WithSchemaWriteFence`), and only two binaries wire one:

- **Fenced** - `cmd/ingester` (`wiring_canonical_writer_open.go`) and
  `cmd/projector` (`runtime_wiring.go`).
- **Unfenced on purpose** - `cmd/bootstrap-index` (`wiring.go`), a one-shot
  seeder that applies or adopts the schema itself and exits.
- **Unfenced** - every writer `cmd/reducer` builds: `EdgeWriter`,
  `SemanticEntityWriter`, `SecretsIAMGraphWriter`, the specialized cloud,
  Kubernetes, and IAM writers in `cmd/reducer/canonical_graph_writers.go`, and
  the orphan sweep store. A marker recorded under a running reducer does not
  stop its writes.

The #6102 Module cutover is unaffected: no unfenced writer keys a Module node on
name. `MERGE (m:Module {name, lang})` belongs to `CanonicalNodeWriter`, the
reducer's only Module upsert MERGEs on `uid` (an identity this cutover did not
move, and one the uid uniqueness constraint records as real DDL), and the orphan
sweep already keys Module on `(name, lang)`.

A future cutover on a label a reducer writer MERGEs on gets no such protection -
those pods would be checked at startup only and keep writing through the
rollout. Two gaps stay open either way: a release older than the fence contains
no call to it, and an unfenced writer has none to make. Both need those pods
stopped before bootstrap records the marker.

## Anti-patterns specific to this package

- Do not query graph backend metadata from graph-writing pods.
- Do not accept any historical marker row after a newer incompatible marker
  exists.
- Do not log connection strings or graph URIs in compatibility errors.
