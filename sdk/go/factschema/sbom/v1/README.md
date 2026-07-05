# SBOM/Attestation Fact Payloads (schema version 1)

This package holds the schema-version-1 typed payload structs for the
`sbom_attestation` fact family. A reducer handler never reads
`Envelope.Payload["some_key"]` for these kinds directly; it decodes through
the parent `factschema` package's kind-keyed seam (for example
`factschema.DecodeSBOMDocument`) and receives one of these structs, validated.

- Go import path: `github.com/eshu-hq/eshu/sdk/go/factschema/sbom/v1`
- Module: `github.com/eshu-hq/eshu/sdk/go/factschema` (no `go/internal` imports)

## Purpose

Eight fact kinds decode through this package:

| Fact kind | Struct | Decode function | Consumed? |
| --- | --- | --- | --- |
| `sbom.document` | `Document` | `factschema.DecodeSBOMDocument` | yes |
| `sbom.component` | `Component` | `factschema.DecodeSBOMComponent` | yes |
| `sbom.dependency_relationship` | `DependencyRelationship` | `factschema.DecodeSBOMDependencyRelationship` | no (typed-but-deferred) |
| `sbom.external_reference` | `ExternalReference` | `factschema.DecodeSBOMExternalReference` | no (typed-but-deferred) |
| `sbom.warning` | `Warning` | `factschema.DecodeSBOMWarning` | yes |
| `attestation.statement` | `Statement` | `factschema.DecodeAttestationStatement` | yes |
| `attestation.signature_verification` | `SignatureVerification` | `factschema.DecodeAttestationSignatureVerification` | yes |
| `attestation.slsa_provenance` | `SLSAProvenance` | `factschema.DecodeAttestationSLSAProvenance` | no (typed-but-deferred, no emitter yet) |

## Ownership boundary

This package owns the Go type definitions and JSON codec for these eight fact
kinds' payloads. It does not own decode dispatch, schema-version routing, or
required-field validation — that lives in the parent `factschema` package
(`decode.go`, `decode_sbom.go`). It does not own graph projection; reducer
handlers under `go/internal/reducer` (the `sbom_attestation_attachment`
domain) consume the decoded structs but live outside this module.

## Exported surface

`Document`, `Component`, `DependencyRelationship`, `ExternalReference`,
`Warning`, `Statement`, `SignatureVerification`, and `SLSAProvenance`. See each
struct's godoc comment for its full field list; the required/optional split
below is the contract most callers need first.

## Dependencies

Standalone: none of these structs import anything beyond the standard
`encoding/json` tags on their own fields (no custom `UnmarshalJSON`/
`MarshalJSON`, unlike the AWS/GCP polymorphic structs). This package carries
no dependency on `go/internal/...` — see the module `AGENTS.md` for the rule.

## Required vs. optional fields, per struct

Field mutability encodes the contract, per Contract System v1 §3.1
(`docs/internal/design/contract-system-v1.md`):

- **Required**: a non-pointer field with no `omitempty` tag. The decode seam
  rejects a payload that omits a required field, or supplies an explicit JSON
  null for one, with a classified `input_invalid` error naming the field,
  never a zero-value struct. A present-but-empty value (for example the empty
  string) is a valid observed value and decodes normally.
- **Optional**: a pointer field, or a slice carrying `omitempty`. An absent
  optional field decodes to nil, not a defaulted zero value.

| Struct | Required identity fields | Why |
| --- | --- | --- |
| `Document` | `DocumentID` | Both collector document-envelope constructors always set it, and it is the reducer's sole index key for a document (`buildSBOMAttachmentIndex`). |
| `Component` | `DocumentID` | Always set by the collector; the reducer's join key from a component back to its document AND into the supply-chain impact index. |
| `DependencyRelationship` | `DocumentID` | Typed-but-deferred; matches the family's join-key convention. |
| `ExternalReference` | `DocumentID` | Typed-but-deferred; matches the family's join-key convention. |
| `Warning` | none | Two collector paths emit this kind with two mutually-exclusive identity keys (`document_id` from the SBOM path, `statement_id` from the attestation path); neither can be required without dead-lettering half of this kind's real traffic. |
| `Statement` | `StatementID` | The attestation collector always sets it, even on a parse-failure statement; it is the reducer's join key for verification/warning evidence. |
| `SignatureVerification` | `StatementID` | The attestation collector always sets it; the reducer's primary join key, with `DocumentID` as an optional fallback. |
| `SLSAProvenance` | `StatementID` | Typed-but-deferred (no emitter yet); matches the family's join-key convention for when one is added. |

Missing a required identity field dead-letters as `input_invalid` rather than
silently orphaning a component, warning, or verification result from its
document/statement — this is the accuracy fix this package exists to add.

## No polymorphic Attributes pass-through

Unlike `awsv1.Resource`/`gcpv1.Resource`, no struct here carries an untyped
`Attributes` map. Every sbom/attestation fact kind has one fixed field set
across both collector paths (`go/internal/collector/sbomdocument`,
`go/internal/collector/sbomruntime`) — none is a polymorphic multi-shape
envelope, so a flat struct with named fields covers the full payload.

## Typed-but-not-yet-consumed kinds

`DependencyRelationship`, `sbom.external_reference` (`ExternalReference`), and
`attestation.slsa_provenance` (`SLSAProvenance`) have no reducer or storage
read path today: `go/internal/reducer/sbom_attestation_attachment.go` loads
their fact kinds alongside their consumed siblings, but no code decodes their
payload fields. `SLSAProvenance` additionally has no collector emitter yet —
SLSA provenance is currently observed only as `Statement.PredicateType` on the
generic attestation statement. They are typed anyway so their join-key
identity is established ahead of a future consumer, mirroring how the
`terraform_state` family left `candidate`/`provider_binding`/`warning`
typed-but-deferred.

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

- `Warning` intentionally has zero required fields — see "Required vs.
  optional fields" above. Do not add one without re-verifying both collector
  paths (`sbomdocument.warningFact`, `sbomruntime.attestationWarningEnvelope`)
  still hold the mutually-exclusive-identity-key invariant.
- `sbomAttestationAttachmentFactKind` (`reducer_sbom_attestation_attachment`,
  `go/internal/reducer/sbom_attestation_attachment_writer.go`) is the
  REDUCER's own re-emitted synthetic evidence fact for its attachment
  decisions, consumed downstream by supply-chain impact correlation. It is
  NOT one of the eight collector-emitted wire kinds this package types, and
  it is out of scope for this package.
- The reducer decodes only the latest struct per fact kind. Older-schema-major
  shims live in the parent package's `decodeLatestMajor`, never here.

## Related docs

- `docs/internal/design/contract-system-v1.md` — §3.1 module layout, §3.2
  decode seam, §5 versioning, §7 migration plan.
- `docs/internal/contract-system-contributor-summary.md`
- Parent module `README.md` (`sdk/go/factschema/README.md`) — decode seam,
  classified errors, schema generation.
- `sdk/go/factschema/incident/v1/README.md` — a comparable flat, non-polymorphic
  family this package's struct shape mirrors.
