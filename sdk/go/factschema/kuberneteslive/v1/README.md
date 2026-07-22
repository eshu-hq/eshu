# Kubernetes Live Fact Payloads (schema version 1)

This package holds the schema-version-1 typed payload structs for the
`kubernetes_live` fact family. A reducer handler never reads
`Envelope.Payload["some_key"]` for these kinds directly; it decodes through
the parent `factschema` package's kind-keyed seam (for example
`factschema.DecodeKubernetesLivePodTemplate`) and receives one of these
structs, validated.

- Go import path: `github.com/eshu-hq/eshu/sdk/go/factschema/kuberneteslive/v1`
- Module: `github.com/eshu-hq/eshu/sdk/go/factschema` (no `go/internal` imports)

## Purpose

Four Kubernetes live fact kinds decode through this package:

| Fact kind | Struct | Decode function |
| --- | --- | --- |
| `kubernetes_live.pod_template` | `PodTemplate` | `factschema.DecodeKubernetesLivePodTemplate` |
| `kubernetes_live.relationship` | `Relationship` | `factschema.DecodeKubernetesLiveRelationship` |
| `kubernetes_live.warning` | `Warning` | `factschema.DecodeKubernetesLiveWarning` |
| `kubernetes_live.namespace` | `Namespace` | `factschema.DecodeKubernetesLiveNamespace` |

## Ownership boundary

This package owns the Go type definitions and JSON codec for these three
fact kinds' payloads. It does not own decode dispatch, schema-version
routing, or required-field validation — that lives in the parent
`factschema` package (`decode.go`, `decode_kuberneteslive.go`). It does not
own graph projection; reducer handlers under `go/internal/reducer` consume
the decoded structs but live outside this module.

## Exported surface

`PodTemplate`, `PodTemplateContainer`, `Relationship`, `Warning`, and
`Namespace`. See each struct's godoc comment for its full field list; the
required/optional split below is the contract most callers need first.

## Dependencies

Standalone: no struct in this package imports anything beyond the Go
standard library's implicit `encoding/json` struct-tag contract (there is no
explicit import — every field decodes/encodes through plain JSON tags, no
custom `UnmarshalJSON`/`MarshalJSON`). This package carries no dependency on
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
  absent optional field decodes to nil, not a defaulted zero value.

| Struct | Required identity fields | Why |
| --- | --- | --- |
| `PodTemplate` | `ObjectID` | The collector emitter (`kuberneteslive.NewPodTemplateEnvelope`) builds it from the validated `ObjectIdentity` before the envelope exists, and the reducer's node-row gate (`kubernetesWorkloadNodeRow`) already drops a pod template lacking it rather than fabricating a `KubernetesWorkload` node. |
| `Relationship` | `RelationshipType`, `FromObjectID`, `ToObjectID` | The collector emitter (`kuberneteslive.NewRelationshipEnvelope`) rejects a blank type or an invalid endpoint identity before the envelope is built, and the reducer's edge classifier (`kubernetesCorrelationIndex.ingestRelationship`) already drops an edge missing any of the three. |
| `Warning` | `Reason`, `ClusterID` | The collector emitter (`kuberneteslive.NewWarningEnvelope`) rejects a blank reason or cluster id before the envelope is built, and the reducer's ingest gate (`kubernetesCorrelationIndex.ingestWarning`) already drops a warning missing `Reason`. |
| `Namespace` | `ObjectID` | The collector emitter (`kuberneteslive.NewNamespaceEnvelope`) builds `ObjectID` from the validated `ObjectIdentity` before the envelope exists, mirroring `PodTemplate`. |

Missing a required identity field dead-letters as `input_invalid` rather than
forming an empty-string graph identity or a partial edge — this is the
accuracy fix this package exists to add.

Every other field is optional even though the collector emitter
unconditionally writes most of them (for example `PodTemplate.ClusterID`,
`Namespace`, `ServiceAccount`): the emitter can validly write an empty string
for a cluster-scoped or unlabeled object, and the reducer's existing read
path already tolerates an absent or empty value for every one of them.
Requiring a field the emitter can validly leave empty would dead-letter a
valid fact — the reverse of the accuracy goal, and the reason these fields
stay optional rather than mirroring "always emitted" as "always required."

## No Attributes pass-through

Unlike `awsv1.Resource`/`gcpv1.Resource`, none of `PodTemplate`,
`Relationship`, `Warning`, or `Namespace` is a polymorphic generic envelope:
each fact kind in this family describes one fixed observation shape (a pod
template, a directed object relationship, a collection warning, or a
namespace's label evidence), so the
reducer-consumed payload keys are modeled as named fields rather than an opaque
map. The collector also emits boundary and context keys (for example
`collector_instance_id`); the generated schemas are open
(`additionalProperties: true`), so those extra keys are permitted and ignored
on decode. None of these structs carries an `Attributes map[string]any`
catch-all.

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

- The reducer decodes only the latest struct per fact kind. Older-schema-major
  shims live in the parent package's `decodeLatestMajor`, never here.
- `PodTemplateContainer.Ports` carries declared container ports only; no
  runtime port-binding evidence. `EnvKeys` are environment variable NAMES
  only — values are never collected, matching the collector's redaction
  contract.

## Related docs

- `docs/internal/design/contract-system-v1.md` — §3.1 module layout, §3.2
  decode seam, §5 versioning, §7 migration plan.
- `docs/internal/contract-system-contributor-summary.md`
- Parent module `README.md` (`sdk/go/factschema/README.md`) — decode seam,
  classified errors, schema generation.
- `sdk/go/factschema/incident/v1/README.md` — the closed-schema (no
  Attributes pass-through) pattern this package's structs mirror.
