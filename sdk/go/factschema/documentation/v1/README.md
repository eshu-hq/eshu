# Documentation Fact Payloads (schema version 1)

This package holds the schema-version-1 typed payload structs for the
`documentation` fact family. A reducer handler never reads
`Envelope.Payload["some_key"]` for these kinds directly; it decodes through
the parent `factschema` package's kind-keyed seam (for example
`factschema.DecodeDocumentationEntityMention`) and receives one of these
structs, validated.

- Go import path: `github.com/eshu-hq/eshu/sdk/go/factschema/documentation/v1`
- Module: `github.com/eshu-hq/eshu/sdk/go/factschema` (no `go/internal` imports)

## Purpose

Eight fact kinds decode through this package:

| Fact kind | Struct | Decode function | Consumed? |
| --- | --- | --- | --- |
| `documentation_source` | `Source` | `factschema.DecodeDocumentationSource` | no (typed-but-deferred) |
| `documentation_document` | `Document` | `factschema.DecodeDocumentationDocument` | yes |
| `documentation_section` | `Section` | `factschema.DecodeDocumentationSection` | no (typed-but-deferred) |
| `documentation_link` | `Link` | `factschema.DecodeDocumentationLink` | no (typed-but-deferred) |
| `documentation_entity_mention` | `EntityMention` | `factschema.DecodeDocumentationEntityMention` | yes |
| `documentation_claim_candidate` | `ClaimCandidate` | `factschema.DecodeDocumentationClaimCandidate` | no (typed-but-deferred) |
| `documentation_finding` | `Finding` | `factschema.DecodeDocumentationFinding` | no (typed-but-deferred; query-layer-only, raw SQL) |
| `documentation_evidence_packet` | `EvidencePacket` | `factschema.DecodeDocumentationEvidencePacket` | no (typed-but-deferred; query-layer-only, raw SQL) |

## Ownership boundary

This package owns the Go type definitions and JSON codec for these eight fact
kinds' payloads. It does not own decode dispatch, schema-version routing, or
required-field validation — that lives in the parent `factschema` package
(`decode.go`, `decode_documentation.go`). It does not own graph projection;
reducer handlers under `go/internal/reducer` (the
`documentation_materialization` domain, `documentation_edges` projection
domain) consume the decoded structs but live outside this module.

## Exported surface

`Source`, `Document`, `Section`, `Link`, `EntityMention`, `ClaimCandidate`,
`Finding`, `EvidencePacket`, plus the shared `OwnerRef`, `ACLSummary`, and
`EvidenceRef` value types (`shared.go`) and `EvidencePacket`'s own
`LinkedEntityRef` type (`finding.go` — uses the distinct `entity_type`/
`entity_id` key pair the query layer's target-match helper reads, not
`EvidenceRef`'s `kind`/`id` pair). See each struct's godoc comment for its
full field list; the required/optional split below is the contract most
callers need first.

## Dependencies

Standalone: none of these structs import anything beyond the standard
`encoding/json` tags on their own fields (no custom `UnmarshalJSON`/
`MarshalJSON`). This package carries no dependency on `go/internal/...` — see
the module `AGENTS.md` for the rule.

## Required vs. optional fields, per struct

Field mutability encodes the contract, per Contract System v1 §3.1
(`docs/internal/design/contract-system-v1.md`):

- **Required**: a non-pointer field with no `omitempty` tag. The decode seam
  rejects a payload that omits a required field, or supplies an explicit JSON
  null for one, with a classified `input_invalid` error naming the field,
  never a zero-value struct. A present-but-empty value (for example the empty
  string) is a valid observed value and decodes normally.
- **Optional**: a pointer field, or a slice/map carrying `omitempty`. An
  absent optional field decodes to nil, not a defaulted zero value.

| Struct | Required identity fields | Why |
| --- | --- | --- |
| `Source` | `SourceID`, `SourceSystem`, `ExternalID` | Form `facts.DocumentationSourceStableID`; typed-but-deferred, no reader yet. |
| `Document` | `DocumentID` | The reducer's delta-scope builder's sole join key for a document (`buildDocumentationDeltaScope`). |
| `Section` | `DocumentID`, `RevisionID`, `SectionID` | Form `facts.DocumentationSectionStableID`; typed-but-deferred, no reader yet. Carries its OWN schema version (`1.1.0`), not the shared `1.0.0`. |
| `Link` | `DocumentID`, `LinkID`, `TargetURI` | Form `facts.DocumentationLinkStableID`; typed-but-deferred, no reader yet. |
| `EntityMention` | `DocumentID`, `SectionID`, `ResolutionStatus` | `ExtractDocumentationEdgeRows`'s actual read-side identity gate: a mention missing any of these cannot be matched to its section or classified as exact. |
| `ClaimCandidate` | `DocumentID`, `SectionID`, `ClaimID`, `ClaimType`, `ClaimText`, `ClaimHash`, `Authority` | Form `facts.DocumentationClaimCandidateStableID`, or always emitted by the collector; typed-but-deferred, no reader yet. |
| `Finding` | `FindingID`, `FindingVersion` | Form `facts.DocumentationFindingStableID`; emitted by `go/internal/doctruth`, read only by the query layer's raw SQL (out of scope here). |
| `EvidencePacket` | `PacketID`, `FindingID` | Form `facts.DocumentationEvidencePacketStableID`; emitted by `go/internal/doctruth`, read only by the query layer's raw SQL (out of scope here). |

Missing a required identity field dead-letters as `input_invalid` rather than
silently producing an empty-identity edge or a fact that can never be joined
to its document/section — this is the accuracy fix this package exists to add
for `Document` and `EntityMention`, the two kinds with a real reducer read
today.

## Only two kinds have a reducer decode site

Unlike most prior Contract System v1 families, six of this family's eight
kinds have NO reducer or storage-loader field-level read at all:

- `documentation_source`, `documentation_section`, `documentation_link`,
  `documentation_claim_candidate`: the query read model
  (`go/internal/query/documentation_read_model.go`,
  `documentation_source_only.go`) filters on them only by `fact_kind` column
  or JSONB containment, never a decoded field.
- `documentation_finding`, `documentation_evidence_packet`: emitted by
  `go/internal/doctruth` (a different owning package, not a documentation
  collector) and read only by the query layer's raw
  `fact_records.payload->>'field'` SQL
  (`documentation_read_model.go`, `documentation_finding_aggregates.go`,
  `documentation_packet_read_model.go`) — out of scope for this migration
  (see the reducer `AGENTS.md`'s documentation-family caveat, mirroring the
  incident family's SQL-projected-fields precedent). The fields that SQL
  layer reads are still declared on `Finding`/`EvidencePacket` so a future
  schema change cannot silently drop a field the SQL layer depends on,
  matching `TestIncidentRoutingSQLProjectedFieldsAreSchemaDeclared`'s
  pattern.

Only `documentation_document` (`buildDocumentationDeltaScope`) and
`documentation_entity_mention` (`ExtractDocumentationEdgeRows`) are converted
to the typed decode seam this wave.

## No polymorphic Attributes pass-through

No struct here carries an untyped `Attributes` map. Every documentation fact
kind has one fixed field set across the documentation collector paths — none
is a polymorphic multi-shape envelope, so a flat struct with named fields
covers the full payload.

## Changing a struct

Any field change here is a payload-schema change.

- **Additive optional field** (new pointer/`omitempty` field): a minor schema
  bump. Add the field, regenerate, and commit the schema in the same change.
- **Remove, rename, or narrow a field**: a major schema bump. It needs a
  conversion shim in the parent package's decode seam (`decode.go`,
  `decodeLatestMajor`) — see the module `README.md` — never a silent edit
  here.

Regenerate after any struct change:

```bash
cd sdk/go/factschema
go generate ./...
```

`schema_gen_test.go`'s `TestSchemasHaveNoDrift` fails the build on drift. The
decode seam derives its required-field set reflectively from each struct's
tags (`../../fields.go`), so there is no separate map to update;
`TestDerivedKeySetsMatchGeneratedSchemas` fails if that reflective set ever
diverges from the generated schema, and `TestPayloadStructShapeConvention`
rejects a field shape that would make "required" ambiguous.

## Telemetry

None. This package has no runtime, network, queue, graph, or telemetry
emission path — see the module `README.md`'s no-observability-change note.

## Gotchas / invariants

- `Section` carries its OWN schema version
  (`facts.DocumentationSectionFactSchemaVersion`, `"1.1.0"`), distinct from
  every other kind in this family (`facts.DocumentationFactSchemaVersion`,
  `"1.0.0"`). Do not fold it into the shared version constant.
- `Finding` and `EvidencePacket` are emitted by `go/internal/doctruth` (the
  verifier), NOT a documentation collector. Their nested/computed sub-objects
  (`permissions`, `states`, `unified_evidence`, the embedded `finding`
  sub-map) are intentionally NOT modeled here — the query layer reads them
  through generic Go map helpers over the decoded JSON map, not a fixed
  sub-shape, so half-modeling them would be a hollow contract.
- The reducer decodes only the latest struct per fact kind. Older-schema-major
  shims live in the parent package's `decodeLatestMajor`, never here.

## Related docs

- `docs/internal/design/contract-system-v1.md` — §3.1 module layout, §3.2
  decode seam, §5 versioning, §7 migration plan.
- `docs/internal/contract-system-contributor-summary.md`
- Parent module `README.md` (`sdk/go/factschema/README.md`) — decode seam,
  classified errors, schema generation.
- `sdk/go/factschema/sbom/v1/README.md` — a comparable family with several
  typed-but-deferred kinds, whose precedent this package follows.
