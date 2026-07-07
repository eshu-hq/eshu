# GCP Fact Payloads (schema version 1)

This package holds the schema-version-1 typed payload structs for the `gcp`
fact family. A reducer handler never reads `Envelope.Payload["some_key"]` for
these kinds directly; it decodes through the parent `factschema` package's
kind-keyed seam (for example `factschema.DecodeGCPCloudResource`) and
receives one of these structs, validated.

- Go import path: `github.com/eshu-hq/eshu/sdk/go/factschema/gcp/v1`
- Module: `github.com/eshu-hq/eshu/sdk/go/factschema` (no `go/internal` imports)

## Purpose

Seven GCP fact kinds decode through this package:

| Fact kind | Struct | Decode function |
| --- | --- | --- |
| `gcp_cloud_resource` | `Resource` | `factschema.DecodeGCPCloudResource` |
| `gcp_cloud_relationship` | `Relationship` | `factschema.DecodeGCPCloudRelationship` |
| `gcp_collection_warning` | `CollectionWarning` | `factschema.DecodeGCPCollectionWarning` |
| `gcp_dns_record` | `DNSRecord` | `factschema.DecodeGCPDNSRecord` |
| `gcp_iam_policy_observation` | `IAMPolicyObservation` | `factschema.DecodeGCPIAMPolicyObservation` |
| `gcp_tag_observation` | `TagObservation` | `factschema.DecodeGCPTagObservation` |
| `gcp_image_reference` | `ImageReference` | `factschema.DecodeGCPImageReference` |

`gcp_image_reference` and `gcp_tag_observation` share downstream reducer and
storage surfaces with AWS/Azure/OCI/CICD evidence. Those consumers still own
their domain behavior, but the GCP collector emit path and decode seam now use
these structs so missing required fields fail as classified `input_invalid`
errors instead of slipping through as zero values.

`gcp_iam_principal`, `gcp_iam_trust_policy`, and `gcp_iam_permission_policy`
are also **out of scope**: they belong to the `secrets_iam` fact family
(`go/internal/facts/secrets_iam.go`), a distinct family boundary this package
does not type.

## Ownership boundary

This package owns the Go type definitions and JSON codec for these seven
fact kinds' payloads. It does not own decode dispatch, schema-version
routing, or required-field validation — that lives in the parent
`factschema` package (`decode.go`, `decode_gcp.go`). It does not own graph
projection; reducer handlers under `go/internal/reducer` consume the decoded
structs but live outside this module.

## Exported surface

`Resource`, `Relationship`, `CollectionWarning`, `DNSRecord`,
`IAMPolicyObservation`, `TagObservation`, and `ImageReference`. See each
struct's godoc comment for its full field list; the required/optional split
below is the contract most callers need first.

## Dependencies

Standalone: `Resource` and `Relationship` import only `encoding/json` from
the standard library for their custom JSON codec; the rest have no imports.
This package carries no dependency on `go/internal/...` — see the module
`AGENTS.md` for the rule.

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
| `Resource` | `FullResourceName`, `AssetType` | The collector emitter (`gcpcloud.NewCloudResourceEnvelope`) validates both non-empty, and the reducer's own node-row gate already drops a resource lacking either rather than fabricating a node. |
| `Relationship` | `SourceFullResourceName`, `TargetFullResourceName`, `RelationshipType` | The collector emitter (`gcpcloud.NewCloudRelationshipEnvelope`) fails closed on any of the three being empty. |
| `CollectionWarning` | `WarningKind`, `Outcome` | The emitter validates both against bounded closed vocabularies before ever reaching the envelope builder. |
| `DNSRecord` | `ManagedZoneFullResourceName`, `RecordType`, `RecordNameFingerprint` | The emitter fails closed on a missing zone, record type, or record name (fingerprinted before this struct sees it). |
| `IAMPolicyObservation` | `FullResourceName`, `AssetType`, `Role`, `Members` | The emitter fails closed on any of the first three being empty AND on zero fingerprinted members. `Members` is the binding's only principal evidence, so it is required (a required slice carries no `omitempty`): an absent or null `members` key dead-letters as `input_invalid`. |
| `TagObservation` | `FullResourceName`, `AssetType`, `TagKeyFingerprint` | The emitter only builds tag evidence after deriving the owning resource identity and fingerprinting the tag key. |
| `ImageReference` | `FullResourceName`, `AssetType`, `ImageReference`, `TagDigestConfidence` | The emitter requires the owning resource, image reference, and bounded confidence class used by downstream image correlation. |

Missing a required identity field dead-letters as `input_invalid` rather than
forming an empty-string graph identity — this is the accuracy fix this
package exists to add.

`IAMPolicyObservation.Members` is required because the emitter
(`gcpcloud.NewIAMPolicyObservationEnvelope`) rejects an observation with zero
fingerprinted members before the envelope is ever built, so a fact with no
members cannot exist on the emit path. Requiring the key stops an external
collector or fixture from omitting the binding's sole principal evidence and
still passing decode plus schema conformance. The decode seam validates key
**presence**, not collection **non-emptiness**: a present-but-empty `[]` still
decodes (the emitter never produces one), but an absent or null `members` key
dead-letters. That is the meaningful contract boundary here — the key must be
present.

## The Attributes pass-through boundary (Resource and Relationship only)

`Resource` and `Relationship` are **polymorphic generic envelopes**, mirroring
`awsv1.Resource` / `awsv1.Relationship`: one fact kind carries every GCP asset
type or relationship type. Both structs type and validate the shared identity
contract and the common fields the node/edge projector reads, and pass every
remaining payload key through untyped in `Attributes map[string]any`, with
JSON type fidelity preserved by a custom `UnmarshalJSON`/`MarshalJSON` pair.

Unlike AWS, the GCP collector does not currently nest per-asset-type fields
under a single `"attributes"` payload key for every kind uniformly — but
`Resource`'s 1.1.0 schema bump added exactly that bounded typed-depth
extraction map, so a service-specific field on `Resource` lands at
`Attributes["attributes"][key]`, not `Attributes[key]` directly, matching the
AWS nesting contract. A consumer reads it through the decoded struct with the
reducer's `payloadAttributes(resource.Attributes)` helper (which returns
`resource.Attributes["attributes"]` as a map) — never
`env.Payload["attributes"][key]`.

`Attributes` also captures boundary/control-plane metadata not named as a
struct field (`collector_instance_id`, `parent_scope_kind`, ancestry, labels,
`extension`, `redaction_policy_version`, and similar); no graph consumer
reads these from here today.

Typing per-asset-type `Resource` attributes (mirroring the AWS
per-resource-type deferral, design §7, issue #4631) is deferred follow-up
work, not a gap in this package's identity-accuracy goal.

`CollectionWarning`, `DNSRecord`, `IAMPolicyObservation`, `TagObservation`,
and `ImageReference` are each scoped to one fact kind with a known field set,
so none of them carries an `Attributes` pass-through.

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
rejects a field shape that would make "required" ambiguous. Removing a
`Resource`/`Relationship` field without updating `resourceKnownKeys` /
`relationshipKnownKeys` would silently leak that field into `Attributes`
instead of failing loudly — keep the known-keys set and the struct's field
tags in lockstep.

## Telemetry

None. This package has no runtime, network, queue, graph, or telemetry
emission path — see the module `README.md`'s no-observability-change note.

## Gotchas / invariants

- `Resource`'s schema version is pinned at 1.1.0
  (`facts.GCPCloudResourceSchemaVersion`), one minor ahead of the rest of this
  family's 1.0.0 kinds. The decode seam still dispatches on the schema-version
  MAJOR only, so this is a version-artifact detail, not a second decode path.
- `gcp_iam_principal`, `gcp_iam_trust_policy`, and `gcp_iam_permission_policy`
  are the `secrets_iam` family's kinds, not this package's — do not add
  structs for them here.
- The reducer decodes only the latest struct per fact kind. Older-schema-major
  shims live in the parent package's `decodeLatestMajor`, never here.

## Related docs

- `docs/internal/design/contract-system-v1.md` — §3.1 module layout, §3.2
  decode seam, §5 versioning, §7 migration plan.
- `docs/internal/contract-system-contributor-summary.md`
- Parent module `README.md` (`sdk/go/factschema/README.md`) — decode seam,
  classified errors, schema generation.
- `sdk/go/factschema/aws/v1/README.md` — the AWS family this package's
  Resource/Relationship pattern mirrors.
