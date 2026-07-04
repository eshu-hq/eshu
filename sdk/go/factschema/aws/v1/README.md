# AWS Fact Payloads (schema version 1)

This package holds the schema-version-1 typed payload structs for the `aws`
fact family. A reducer handler never reads `Envelope.Payload["some_key"]` for
these kinds directly; it decodes through the parent `factschema` package's
kind-keyed seam (for example `factschema.DecodeAWSResource`) and receives one
of these structs, validated.

- Go import path: `github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1`
- Module: `github.com/eshu-hq/eshu/sdk/go/factschema` (no `go/internal` imports)

## Purpose

Five AWS fact kinds decode through this package:

| Fact kind | Struct | Decode function |
| --- | --- | --- |
| `aws_resource` | `Resource` | `factschema.DecodeAWSResource` |
| `aws_relationship` | `Relationship` | `factschema.DecodeAWSRelationship` |
| `aws_security_group_rule` | `SecurityGroupRule` | `factschema.DecodeAWSSecurityGroupRule` |
| `ec2_instance_posture` | `EC2InstancePosture` | `factschema.DecodeEC2InstancePosture` |
| `s3_bucket_posture` | `S3BucketPosture` | `factschema.DecodeS3BucketPosture` |

## Ownership boundary

This package owns the Go type definitions and JSON codec for these five fact
kinds' payloads. It does not own decode dispatch, schema-version routing, or
required-field validation — that lives in the parent `factschema` package
(`decode.go`, `decode_aws.go`). It does not own graph projection; reducer
handlers under `go/internal/reducer` consume the decoded structs but live
outside this module.

## Exported surface

`Resource`, `Relationship`, `SecurityGroupRule`, `EC2InstancePosture`,
`S3BucketPosture`, and `BlockDevice` (an `EC2InstancePosture` sub-struct).
See each struct's godoc comment for its full field list; the required/
optional split below is the contract most callers need first.

## Dependencies

Standalone: this package imports only `encoding/json` from the standard
library. It carries no dependency on `go/internal/...` — see the module
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
| `Resource` | `AccountID`, `ResourceID`, `Region`, `ResourceType` | The collector emitter (`awscloud.NewResourceEnvelope`) validates all four non-empty. Missing any one previously produced an empty-string graph uid; the decode seam now dead-letters it as `input_invalid` instead. |
| `Relationship` | `AccountID`, `Region`, `RelationshipType`, `SourceResourceID`, `TargetResourceID` | The collector emitter (`awscloud.NewRelationshipEnvelope`) validates all five non-empty (`source_resource_id` defaults to `source_arn`, `target_resource_id` to `target_arn`, so one identity is always present). |
| `SecurityGroupRule` | `AccountID`, `Region`, `GroupID`, `Direction`, `IPProtocol`, `SourceKind`, `SourceValue` | The collector emitter (`awscloud.NewSecurityGroupRuleEnvelope`) validates `group_id` non-empty and always emits the rest from a boundary and a normalized `(kind, value)` pair. `GroupID` anchors the `SecurityGroup` node the reachability edge hangs off. |
| `EC2InstancePosture` | `AccountID`, `Region` | The emitter validates `instance_id` OR `arn` non-empty as an either-or identity, so neither `InstanceID` nor `ARN` can be required on its own — requiring one would dead-letter a valid fact identified only by the other. |
| `S3BucketPosture` | `AccountID`, `Region` | Same either-or shape: the emitter validates `bucket_arn` OR `bucket_name` non-empty, so neither `BucketARN` nor `BucketName` is required on its own. |

Missing a required identity field dead-letters as `input_invalid` rather than
forming an empty-string graph identity — this is the accuracy fix this
package exists to add.

`Resource.Tags` is a pointer to a map, not a plain map, so the two "empty"
states stay distinct across a round trip: a nil pointer means the collector
did not observe tags (omitted from the payload), while a non-nil pointer to
an empty map means the collector observed the resource and found zero tags
(marshals as `"tags":{}` and round-trips back to a non-nil empty map). A
plain map with `omitempty` could not express "observed empty" — an empty map
would be omitted and decode back as nil. The same either-or-nil pattern
applies to every `*bool` posture field on `EC2InstancePosture` and
`S3BucketPosture`: nil means unreported, `false` means an observed negative.

## The Attributes pass-through boundary

`Resource` and `Relationship` are **polymorphic generic envelopes**: one fact
kind carries every AWS resource type (S3 buckets, IAM roles, EC2 instances,
RDS instances, ...) or every relationship verb. A single typed struct cannot
fully type either without the optional-everything anti-pattern (design §3.3
rejects it) or a per-resource-type redesign (out of scope for this issue).

Both structs instead type and validate the shared identity contract and the
common fields more than one consumer reads, and pass every remaining,
service- or verb-specific payload key through untyped in `Attributes
map[string]any`, with JSON type fidelity preserved by a custom
`UnmarshalJSON`/`MarshalJSON` pair.

**The pass-through is nested, not flat.** The collector emitter does not
write service-specific fields as top-level payload keys; it nests them one
level deep under a single `"attributes"` object
(`payload["attributes"] = {"engine": ..., "role_arns": ...}`). So a
service-specific field lands at `Attributes["attributes"][key]`, not
`Attributes[key]` directly. A consumer reads it through the decoded struct
with the reducer's `payloadAttributes(resource.Attributes)` helper (which
returns `resource.Attributes["attributes"]` as a map), for example
`payloadAttributes(resource.Attributes)["engine"]` — never
`env.Payload["attributes"]["engine"]` — so the "no raw payload key access"
contract holds while these fields are honestly not yet a typed contract.
(`Attributes` also captures the emitter's `collector_instance_id` boundary
key at the top level; that is boundary metadata, not a service attribute,
and no graph consumer reads it from here.)

Typing service-specific `Resource`/`Relationship` attributes per
`resource_type` (RDS engine and KMS fields, instance-profile role ARNs,
workload identifiers, CloudWatch alarm dimensions, and similar) is deferred,
tracked as a follow-up issue referencing design §7 ("remaining families",
epic #4566, tracked as #4631). It is a distinct, larger increment, not a gap
in this package's identity-accuracy goal, which is complete and uniform
across every `aws_resource` and `aws_relationship` consumer today.

`SecurityGroupRule`, `EC2InstancePosture`, and `S3BucketPosture` are each
scoped to one fact kind with a known field set, so none of them carries an
`Attributes` pass-through.

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

`schema_gen_test.go` fails the build on drift, and
`TestRequiredFieldsMatchStructShape` fails if `decode.go`'s `requiredFields`
map no longer matches a struct's shape. Removing a struct field without
updating `resourceKnownKeys` / `relationshipKnownKeys` (for `Resource` /
`Relationship`) would silently leak that field into `Attributes` instead of
failing loudly — keep the known-keys set and the struct's field tags in
lockstep.

## Telemetry

None. This package has no runtime, network, queue, graph, or telemetry
emission path — see the module `README.md`'s no-observability-change note.

## Gotchas / invariants

- Attributes nesting: reach service-specific fields through
  `payloadAttributes(resource.Attributes)["key"]`, never `Attributes["key"]`
  directly — see "The Attributes pass-through boundary" above.
- A named field always wins over a same-named `Attributes` key on
  `MarshalJSON`; `resourceKnownKeys` / `relationshipKnownKeys` entries are
  stripped from `Attributes` before merging back to a flat payload.
- The reducer decodes only the latest struct per fact kind. Older-schema-major
  shims live in the parent package's `decodeLatestMajor`, never here.

## Related docs

- `docs/internal/design/contract-system-v1.md` — §3.1 module layout, §3.2
  decode seam, §5 versioning, §7 migration plan.
- `docs/internal/contract-system-contributor-summary.md`
- Parent module `README.md` (`sdk/go/factschema/README.md`) — decode seam,
  classified errors, schema generation.
