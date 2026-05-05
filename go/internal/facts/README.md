# Facts

## Purpose

`facts` defines the durable Go representations that Eshu writes before graph
projection. An `Envelope` carries one parsed observation from a collector or
parser through the queue, into the projector, and on to the reducer. A `Ref`
identifies the source-local record that produced the fact. These types are the
contract between collection, queueing, projection, and reducer-owned
materialization.

## Ownership boundary

Owns the durable fact value types and the stable-ID function. Per the ownership
table in `CLAUDE.md`: `go/internal/facts/` — durable fact models and queue
contracts.

This package does not own queue row logic (`internal/queue`), scope identity
(`internal/scope`), graph writes, or Postgres persistence. Those packages
consume these types as their input or storage shape.

## Exported surface

- `Envelope` — the interchange unit that travels from collector to projector.
  Fields: `FactID`, `ScopeID`, `GenerationID`, `FactKind`, `StableFactKey`,
  `ObservedAt`, `Payload`, `IsTombstone`, `SourceRef`.
- `Ref` — the source-local provenance record embedded in `Envelope.SourceRef`.
  Fields: `SourceSystem`, `ScopeID`, `GenerationID`, `FactKey`, `SourceURI`,
  `SourceRecordID`.
- `Envelope.ScopeGenerationKey()` — returns the durable `scopeID:generationID`
  boundary string used by callers to group envelopes by scope generation.
- `Ref.ScopeGenerationKey()` — same boundary string on the ref side.
- `Envelope.Clone()` — deep-copies the envelope including nested `Payload` maps
  and slices; safe to pass to replay pipelines that must not share mutable
  state.
- `StableID(factType, identity)` — deterministic SHA-256 hex ID derived from
  `factType` and the normalized `identity` map; used to assign a stable fact
  key that survives re-ingestion of the same source record.

See `doc.go` for the full godoc contract.

## Dependencies

No internal package imports. `internal/facts` is a leaf contract package. It
depends only on the Go standard library.

## Telemetry

This package emits no metrics, spans, or logs. Telemetry around fact loading
and processing lives in `internal/projector` and `internal/storage/postgres`.

## Gotchas / invariants

- `Envelope` fields and their types are frozen on-disk contracts. New fields
  must be additive; removing or renaming a field breaks stored rows. The
  `doc.go` contract states this explicitly.
- `Envelope.Payload` is a `map[string]any`. Callers must not mutate the map
  after passing the envelope to a downstream stage. Use `Clone` when branching
  or replaying.
- `StableID` panics if `json.Marshal` fails on the identity map. Callers must
  not pass identity maps containing non-serializable values.
- `IsTombstone` is set by the collector to signal deletion. Projectors and
  reducers must check this flag before writing graph nodes.

## Related docs

- `docs/docs/architecture.md` — pipeline and ownership table
- `docs/docs/deployment/service-runtimes.md` — ingester and projector runtime
  lanes
