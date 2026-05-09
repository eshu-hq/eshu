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
  `SchemaVersion`, `CollectorKind`, `FencingToken`, `SourceConfidence`,
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
- Documentation fact payloads — source-neutral payload structs and stable-ID
  helpers for documentation sources, documents, sections, links, entity
  mentions, non-authoritative claim candidates, owner references, ACL
  summaries, and evidence references.

Documentation fact kinds use schema version `1.0.0`:

- `documentation_source`
- `documentation_document`
- `documentation_section`
- `documentation_link`
- `documentation_entity_mention`
- `documentation_claim_candidate`

Terraform state fact kinds also use schema version `1.0.0` for the first
collector contract:

- `terraform_state_snapshot`
- `terraform_state_resource`
- `terraform_state_output`
- `terraform_state_module`
- `terraform_state_provider_binding`
- `terraform_state_tag_observation`
- `terraform_state_warning`

Use `TerraformStateFactKinds` when callers need the full accepted set, and
`TerraformStateSchemaVersion` when building envelopes. That keeps reader code
from copying string literals.

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
- `CollectorKind` and `SourceConfidence` are part of the durable collector
  contract. `CollectorKind` says which collector family emitted the fact.
  `SourceConfidence` says how Eshu learned it: direct observation, external
  report, inference, or derived materialization. New collector code should set
  both fields explicitly instead of relying on storage defaults.
- `Envelope.Payload` is a `map[string]any`. Callers must not mutate the map
  after passing the envelope to a downstream stage. Use `Clone` when branching
  or replaying.
- Documentation claim candidates are evidence about what documentation says.
  They are not operational truth and must not override source-code, deployment,
  runtime, or graph truth.
- Documentation ACL and owner fields are source-reported context. They help
  explain provenance and visibility, but they do not become authorization
  policy inside the facts package.
- `StableID` panics if `json.Marshal` fails on the identity map. Callers must
  not pass identity maps containing non-serializable values.
- `IsTombstone` is set by the collector to signal deletion. Projectors and
  reducers must check this flag before writing graph nodes.

## Related docs

- `docs/docs/architecture.md` — pipeline and ownership table
- `docs/docs/deployment/service-runtimes.md` — ingester and projector runtime
  lanes
