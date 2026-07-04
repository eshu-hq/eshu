# Azure Fact Payloads (schema version 1)

This package holds the schema-version-1 typed payload structs for the `azure`
fact family. A reducer handler never reads `Envelope.Payload["some_key"]` for
these kinds directly; it decodes through the parent `factschema` package's
kind-keyed seam (for example `factschema.DecodeAzureCloudResource`) and
receives one of these structs, validated.

- Go import path: `github.com/eshu-hq/eshu/sdk/go/factschema/azure/v1`
- Module: `github.com/eshu-hq/eshu/sdk/go/factschema` (no `go/internal` imports)

## Purpose

Eight Azure fact kinds decode through this package:

| Fact kind | Struct | Decode function |
| --- | --- | --- |
| `azure_cloud_resource` | `CloudResource` | `factschema.DecodeAzureCloudResource` |
| `azure_cloud_relationship` | `CloudRelationship` | `factschema.DecodeAzureCloudRelationship` |
| `azure_tag_observation` | `TagObservation` | `factschema.DecodeAzureTagObservation` |
| `azure_identity_observation` | `IdentityObservation` | `factschema.DecodeAzureIdentityObservation` |
| `azure_resource_change` | `ResourceChange` | `factschema.DecodeAzureResourceChange` |
| `azure_dns_record` | `DNSRecord` | `factschema.DecodeAzureDNSRecord` |
| `azure_image_reference` | `ImageReference` | `factschema.DecodeAzureImageReference` |
| `azure_collection_warning` | `CollectionWarning` | `factschema.DecodeAzureCollectionWarning` |

## Ownership boundary

This package owns the Go type definitions and JSON codec for these eight fact
kinds' payloads. It does not own decode dispatch, schema-version routing, or
required-field validation — that lives in the parent `factschema` package
(`decode.go`, `decode_azure.go`). It does not own graph projection; reducer
handlers under `go/internal/reducer` consume the decoded structs but live
outside this module. It does not own the collector emitters that build these
payloads (`go/internal/collector/azurecloud`), which also live outside this
module.

## Exported surface

`CloudResource`, `CloudRelationship`, `TagObservation`, `IdentityObservation`,
`ResourceChange`, `DNSRecord`, `ImageReference`, `CollectionWarning`. See each
struct's godoc comment for its full field list; the required/optional split
below is the contract most callers need first.

## Dependencies

Standalone: this package imports only `encoding/json` from the standard
library (the two polymorphic structs). It carries no dependency on
`go/internal/...` — see the module `AGENTS.md` for the rule.

## Required vs. optional fields, per struct

Field mutability encodes the contract, per Contract System v1 §3.1
(`docs/internal/design/contract-system-v1.md`):

- **Required**: a non-pointer field with no `omitempty` tag. The decode seam
  rejects a payload that omits a required field, or supplies an explicit JSON
  null for one, with a classified `input_invalid` error naming the field,
  never a zero-value struct. A present-but-empty value (for example the empty
  string) is a valid observed value and decodes normally.
- **Optional**: a pointer field, or a slice/map carrying `omitempty`. An
  absent optional field decodes to nil, not a defaulted zero value. By this
  repo's flat-struct convention, EVERY slice/map field carries `omitempty`
  regardless of whether the emitter always populates it in practice (see
  `TagObservation.TagValueFingerprints` below).

| Struct | Required identity fields | Why |
| --- | --- | --- |
| `CloudResource` | `ARMResourceID`, `ResourceType`, `SubscriptionID`, `Location` | These are exactly the fields `cloudResourceUID(subscriptionID, location, resourceType, resourceID)` needs; the collector emitter (`azurecloud.NewResourceEnvelope`) always derives all four from the observation and its ARM identity parse. Missing any one previously produced a wrong-but-plausible CloudResource uid; the decode seam now dead-letters it as `input_invalid`. |
| `CloudRelationship` | `RelationshipType`, `SourceARMResourceID`, `TargetARMResourceID` | The collector emitter (`azurecloud.NewRelationshipEnvelope`) validates all three non-empty before emission. |
| `TagObservation` | `ARMResourceID`, `ResourceType` | Anchor the tag subject to the same CloudResource uid its resource fact resolves to. `TagValueFingerprints` cannot be required under the flat-struct convention (map fields always carry `omitempty`); the emitter independently refuses to build an envelope for a resource with zero usable tags. |
| `IdentityObservation` | `ARMResourceID`, `IdentityType` | The emitter validates ARMResourceID non-empty and IdentityType against a bounded enum before emission. None of the four principal fingerprint fields (Principal/Client/Object/Tenant) can be required alone: the emitter validates only that at least one is present, so requiring one would dead-letter a valid fact identified only by another. |
| `ResourceChange` | `TargetARMResourceID`, `ChangeType`, `ChangeTime` | The emitter validates a non-empty target, a change type against a bounded enum, and a non-zero change time before emission. |
| `DNSRecord` | `ZoneARMResourceID`, `RecordType`, `RecordNameFingerprint` | The emitter validates a non-empty zone, record type, and record name (which it always fingerprints) before emission. |
| `ImageReference` | `OwningARMResourceID`, `TagDigestConfidence` | The emitter validates a non-empty owning resource id and always derives a digest-vs-tag confidence value. `ImageReference` and `ImageDigest` cannot be required alone: the emitter validates only that at least one is present. |
| `CollectionWarning` | `WarningKind`, `Outcome` | The emitter rejects a blank warning kind and defaults a blank outcome to `"partial"` before emission, so both are always present once decode succeeds. |

## The Attributes pass-through boundary (CloudResource and CloudRelationship only)

`CloudResource` and `CloudRelationship` are **polymorphic generic
envelopes**: one fact kind carries every ARM resource type or relationship
verb the collector observes. Both structs type and validate only the shared
identity contract and the common fields the reducer reads today, and pass
every remaining top-level payload key through untyped in `Attributes
map[string]any`, with JSON type fidelity preserved by a custom
`UnmarshalJSON`/`MarshalJSON` pair — mirroring `awsv1.Resource` /
`awsv1.Relationship`.

**Unlike the aws family, the pass-through here is FLAT, not nested.** The
Azure collector emitter (`go/internal/collector/azurecloud`) writes its
remaining fields directly at the top level of the payload
(`payload["kind"] = ...`, `payload["sku_class"] = ...`,
`payload["extension"] = {...}`), never nested under a single `"attributes"`
key. So a consumer reads a service-specific field at `Attributes["kind"]`
directly — there is no `payloadAttributes(...)` unwrap helper needed the way
the aws family requires.

`TagObservation`, `IdentityObservation`, `ResourceChange`, `DNSRecord`,
`ImageReference`, and `CollectionWarning` are each scoped to one fact kind
with a known, closed field set, so none of them carries an `Attributes`
pass-through: the collector fingerprints or redacts every sensitive field
before emission, so the full payload shape is already known and stable.

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
`CloudResource`/`CloudRelationship` field without updating
`cloudResourceKnownKeys` / `cloudRelationshipKnownKeys` would silently leak
that field into `Attributes` instead of failing loudly — keep the known-keys
set and the struct's field tags in lockstep. Any fixture pack copy under
`../../fixturepack/schema/` must be refreshed in the same change
(`TestFixturePackSchemasMatchCanonical` locks the two).

## Telemetry

None. This package has no runtime, network, queue, graph, or telemetry
emission path — see the module `README.md`'s no-observability-change note.

## Gotchas / invariants

- Attributes is FLAT here, unlike the aws family's nested `"attributes"`
  object — read `Attributes["key"]` directly.
- A named field always wins over a same-named `Attributes` key on
  `MarshalJSON`; `cloudResourceKnownKeys` / `cloudRelationshipKnownKeys`
  entries are stripped from `Attributes` before merging back to a flat
  payload.
- The reducer decodes only the latest struct per fact kind. Older-schema-major
  shims live in the parent package's `decodeLatestMajor`, never here.

## Related docs

- `docs/internal/design/contract-system-v1.md` — §3.1 module layout, §3.2
  decode seam, §5 versioning, §7 migration plan.
- `docs/internal/contract-system-contributor-summary.md`
- Parent module `README.md` (`sdk/go/factschema/README.md`) — decode seam,
  classified errors, schema generation.
- Sibling `aws/v1/README.md` — the nested-Attributes polymorphic pattern this
  package's flat-Attributes variant is modeled on.
