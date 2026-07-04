# AWS IAM Fact Payloads (schema version 1)

This package holds the schema-version-1 typed payload structs for the AWS
IAM fact family. A reducer handler never reads `Envelope.Payload["some_key"]`
for these kinds directly; it decodes through the parent `factschema`
package's kind-keyed seam (for example `factschema.DecodeAWSIAMPermission`)
and receives one of these structs, validated.

- Go import path: `github.com/eshu-hq/eshu/sdk/go/factschema/iam/v1`
- Module: `github.com/eshu-hq/eshu/sdk/go/factschema` (no `go/internal` imports)

## Purpose

Three AWS IAM fact kinds decode through this package:

| Fact kind | Struct | Decode function |
| --- | --- | --- |
| `aws_iam_permission` | `Permission` | `factschema.DecodeAWSIAMPermission` |
| `aws_resource_policy_permission` | `ResourcePolicyPermission` | `factschema.DecodeAWSResourcePolicyPermission` |
| `aws_iam_principal` | `Principal` | `factschema.DecodeAWSIAMPrincipal` |

Each struct is a normalized, metadata-only projection of one IAM policy
statement or one principal. None carries the raw policy JSON body or any
condition value — `HasConditions` (on `Permission` and
`ResourcePolicyPermission`) is a derived boolean flag only.

## Ownership boundary

This package owns the Go type definitions for these three fact kinds'
payloads. It does not own decode dispatch, schema-version routing, or
required-field validation — that lives in the parent `factschema` package
(`decode.go`, `decode_iam.go`). It does not own graph projection; the IAM
and secrets-trust-chain reducer handlers consume the decoded structs but
live outside this module.

## Exported surface

`Permission`, `ResourcePolicyPermission`, and `Principal`. See each struct's
godoc comment for its full field list; the required/optional split below is
the contract most callers need first.

## Dependencies

Standalone: this package imports nothing beyond the Go standard library and
carries no dependency on `go/internal/...` — see the module `AGENTS.md` for
the rule.

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
| `Permission` | `AccountID`, `Region`, `PrincipalARN`, `Effect`, `PolicySource` | The collector emitter (`awscloud.NewIAMPermissionEnvelope`) validates `principal_arn`, `effect`, and `policy_source` non-empty and always emits `account_id`/`region` from the scan boundary. `PrincipalARN` anchors every edge the statement can produce. |
| `ResourcePolicyPermission` | `AccountID`, `Region`, `ResourceARN`, `ResourceType`, `Effect` | The collector emitter (`awscloud.NewResourcePolicyPermissionEnvelope`) validates `resource_arn`, `resource_type`, and `effect` non-empty. `ResourceARN` is the `CAN_PERFORM` edge target identity; `ResourceType` gates that target resolution matches the correct node type. |
| `Principal` | `AccountID`, `Region`, `PrincipalARN`, `PrincipalType` | The collector emitter (`secretsiam.NewPrincipalEnvelope`) validates `principal_arn` and `principal_type` non-empty and always emits `account_id`/`region` from the scan context. |

Missing a required identity field dead-letters as `input_invalid` rather than
forming an empty-string graph identity or a malformed edge.

`Permission`'s and `ResourcePolicyPermission`'s list fields (`Actions`,
`NotActions`, `Resources`, `NotResources`, and `AssumePrincipals` /
`PrincipalARNs`) are always emitted by the collector as non-nil sorted
slices but are semantically optional — a statement may grant no actions on
no resources — so each carries `omitempty` and decodes to nil when absent.
`HasConditions`, `IsPublic`, and similar derived flags are `*bool` so nil
(unreported) stays distinct from an observed `false`.

## No Attributes pass-through

Unlike `aws/v1.Resource` and `aws/v1.Relationship`, every struct in this
package is fully typed. An IAM policy statement's fields — actions,
resources, principals, effect, source — are a closed, well-known set derived
directly from the IAM policy grammar, not a polymorphic per-resource-type
envelope, so there is no `Attributes map[string]any` pass-through field on
any struct here.

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

- No `Attributes` pass-through anywhere in this package — see "No Attributes
  pass-through" above. Do not add one without a design discussion.
- The reducer decodes only the latest struct per fact kind. Older-schema-major
  shims live in the parent package's `decodeLatestMajor`, never here.
- None of these structs ever carries the raw policy JSON body or a condition
  value; `HasConditions` / `IsPublic` are derived booleans only.

## Related docs

- `docs/internal/design/contract-system-v1.md` — §3.1 module layout, §3.2
  decode seam, §5 versioning, §7 migration plan (this family is step 2,
  "First family: AWS/IAM/security-group").
- `docs/internal/contract-system-contributor-summary.md`
- Parent module `README.md` (`sdk/go/factschema/README.md`) — decode seam,
  classified errors, schema generation.
