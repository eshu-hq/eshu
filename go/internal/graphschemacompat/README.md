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
- `ErrIncompatible` marks the one outcome that means "stop writing", so a caller
  can separate it from a marker it simply could not read.
- `WriteFence` / `NewWriteFence` / `NewWriteFenceForRuntime` re-check the marker
  while a writer is already running, for callers that want the decision to reach
  a write in flight rather than only a process starting up.
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
instrumented adapters. A `WriteFence` refusal surfaces where the write failed:
`CanonicalNodeWriter` returns it as a retryable projector error, so it lands on
the queue row with the full expected/applied fingerprint message and shows up in
existing queue and status telemetry.

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
- `RequireCompatible` decides admission for a writer that is *starting*. A
  writer already past it keeps writing across a marker recorded underneath it,
  which is the normal shape of a Helm upgrade: the schema-bootstrap Job is a
  pre-upgrade hook, so it records the new marker while the previous generation
  of pods is still serving. `WriteFence` is what carries the decision onto the
  write path.
- The fence covers less than "canonical writers", so check before an identity
  cutover leans on it. Only `CanonicalNodeWriter` accepts one, and only
  `cmd/ingester` and `cmd/projector` wire it; `cmd/bootstrap-index` skips it on
  purpose as a one-shot seeder. Every writer `cmd/reducer` builds runs unfenced
  — `EdgeWriter` (built twice), `SemanticEntityWriter`, `SecretsIAMGraphWriter`,
  the `OrphanSweepStore`, and every field of `cmd/reducer`'s
  `canonicalGraphWriters` struct — so a marker recorded under a running reducer
  does not stop its writes. `AGENTS.md` carries the constructor-level inventory,
  the command that regenerates it, the five labels those writers MERGE on a key
  that is not `uid`, and why the #6102 Module cutover is unaffected.
- Deployment ordering does not substitute for the fence. It decides when a
  writer may start, not whether one already running stops: the Helm schema Job
  is a pre-upgrade hook, so it records the marker while the outgoing
  resolution-engine pods are still serving, and Compose's `depends_on` gate is
  likewise a start condition. The stop procedure is manual and lives in
  `docs/public/deployment/service-runtimes-bootstrap.md`.
- Two gaps stay open. A writer from a release built before the fence never calls
  it, and an unfenced writer has no call to make. An identity cutover still
  needs both stopped before bootstrap records the marker.
- A `WriteFence` refuses only on a marker it read successfully that says no. An
  unreadable marker holds the previous decision, because failing closed there
  would turn one Postgres blip into a graph-write outage across every fenced
  writer at once.
- Direct bootstrap-index startup may recover from `ErrMissingMarker` by applying
  the strict graph schema. Long-lived graph writers should return the error and
  let deployment ordering run `eshu-bootstrap-data-plane`.

## Related docs

- `docs/public/deployment/service-runtimes-bootstrap.md`
- `docs/public/reference/environment-runtime-storage.md`
- `go/cmd/bootstrap-data-plane/README.md`
