# Fact Envelope

## Purpose
`factenvelope` centralizes adapter code between the public collector SDK fact
record, the durable internal fact envelope, and the factschema decode envelope.
It exists so extensionhost, reducer, and projector code do not each maintain a
separate field-by-field copy of the same envelope mapping.

## Ownership boundary
This package owns only in-memory conversion between envelope representations. It
does not validate payload schemas, write facts, claim workflow items, project
graph truth, or decide reducer behavior.

## Exported surface
See `doc.go` for the godoc contract. Callers use generated helpers to map a
validated `sdk/go/collector.Fact` into `facts.Envelope`, and to adapt
`facts.Envelope` into the `factschema.Envelope` accepted by Decode functions.

## Dependencies
The package imports `go/internal/facts`, `sdk/go/collector`, and
`sdk/go/factschema`. It intentionally stays below collector runtime packages so
it can be reused by extensionhost, reducer, and projector without pulling in
storage, graph, queue, or runtime dependencies.

## Telemetry
No telemetry is emitted. Runtime diagnostics remain owned by extensionhost,
projector, reducer, and storage callers that know the stage and failure class.

## Gotchas / invariants
The public SDK wire names are not the same as the durable internal names:
`kind` maps to `FactKind`, `stable_key` maps to `StableFactKey`, and
`tombstone` maps to `IsTombstone`. Host-owned fields such as `FactID`,
`ScopeID`, `GenerationID`, `CollectorKind`, and `FencingToken` come from the
claim boundary, not from the SDK fact.

The persisted `0.0.0` schema version means "the collector emitted no version."
Adapters normalize that sentinel and the empty string to `1.0.0` for the
factschema decode seam, but they preserve a genuine unsupported major so decode
classification can fail loudly.

## Related docs
- `docs/internal/design/contract-system-v1.md`
- `docs/public/reference/fact-schema-versioning.md`
- `sdk/go/collector/README.md`
- `sdk/go/factschema/README.md`
- `go/internal/facts/README.md`
