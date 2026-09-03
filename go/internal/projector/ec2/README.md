# EC2 projector intents

## Purpose

This package recognizes EC2 `ec2_instance_posture` facts for one scope
generation and builds reducer intents for the EC2 instance CloudResource
node, its `ami_id` identity projection, block-device KMS posture, internet
exposure, and the `USES_PROFILE` edge to an IAM instance profile.

## Ownership boundary

The package owns only EC2 trigger selection and reducer-intent values,
including the one instance-profile-ARN presence check the USES_PROFILE
builder needs. The root `internal/projector` package validates
scope-generation boundaries, constructs and owns the immutable fact lookup,
preserves family order, and owns projection lifecycle, queue writes, retries,
and telemetry. Reducer handlers own payload validation, ARN/ENI/security-group
joins, graph materialization, and readiness publication.

## Exported surface

- `BuildInstanceNodeMaterializationReducerIntent` builds the canonical EC2
  instance `CloudResource` node intent.
- `BuildInstanceIdentityMaterializationReducerIntent` builds the `ami_id`
  identity-projection intent onto that node.
- `BuildBlockDeviceKMSPostureMaterializationReducerIntent` builds the
  block-device/EBS/KMS posture intent.
- `BuildInternetExposureMaterializationReducerIntent` builds the
  internet-exposure derivation intent.
- `BuildUsesProfileMaterializationReducerIntent` builds the `USES_PROFILE`
  edge intent to the instance's attached IAM instance profile.

See `doc.go` for the full godoc contract.

## Dependencies

Builders depend on `internal/projector/intent.FactLookup` and
`internal/projector/intent.ReducerIntent`; this package must not import the
root projector package. `BuildUsesProfileMaterializationReducerIntent` also
decodes the `ec2_instance_posture` payload through this package's own
`factschema_decode_aws.go` (`sdk/go/factschema` plus `internal/factenvelope`
directly), rather than importing root's classified decode wrapper
(`projectorDecodeError` in root's `factschema_quarantine.go`), because
importing root would create an import cycle (root already imports this
package to dispatch to it). The single caller here only checks `err != nil`,
discarding the decode error's classification, so the direct
`factschema.DecodeEC2InstancePosture` call plus fact-kind-labeled wrapping is
behavior-identical for that check. This mirrors `go/internal/reducer`'s own
independent `decodeEC2InstancePosture` copy — the repo already keeps AWS
decode wrappers per package rather than shared. The file is named
`factschema_decode_aws.go` to match that repo-wide convention so
`scripts/verify-payload-usage-manifest.sh` (issue #4573), which globs
`factschema_decode*.go` files and AST-scans each function for a
`factschema.FactKindXxx` reference, discovers it as a decode seam.

Four of the five builders — instance-node, instance-identity, block-device
KMS posture, and internet-exposure — trigger on the mere presence of an
`ec2_instance_posture` fact and do not decode its payload.

## Telemetry

No-Observability-Change: this package emits no signal directly. Root intent
enqueue remains covered by `eshu_dp_reducer_intents_enqueued_total`; reducer
handlers retain execution, reconciliation, and graph-write telemetry. Moving
the pure builders adds no queue, storage, graph, span, metric, or log
boundary.

## Gotchas / invariants

- `BuildInstanceNodeMaterializationReducerIntent` and
  `BuildInstanceIdentityMaterializationReducerIntent` share the SAME entity
  key (`ec2_instance_node_materialization:<scope>`) — NOT the
  `aws_resource_materialization:<scope>` key the generic AWS resource node
  domain uses, because `aws_ec2_instance` is excluded from that generic path.
  `BuildInternetExposureMaterializationReducerIntent` shares that key too.
- `BuildUsesProfileMaterializationReducerIntent` carries its OWN distinct
  entity key (`ec2_uses_profile_materialization:<scope>`) because the edge
  gates on TWO node phases under different keys (the EC2 instance node and
  the generic AWS instance-profile node); the durable claim gate references
  both entity-key prefixes directly rather than matching a single key.
- `BuildUsesProfileMaterializationReducerIntent` only enqueues when at least
  one posture fact has a non-blank, trimmed `instance_profile_arn`; an
  invalid (non-decodable) posture fact is treated as no match, not an error.
- All five builders trigger on `ec2_instance_posture`
  (`facts.EC2InstancePostureFactKind`) and anchor to the earliest matching
  fact so the reducer claim is stable across reprojections of the same
  generation.

## Verification

Run the package contract tests, root EC2 assembly tests, ordered fan-out
parity and probe-count tests, the projector package tree, package-doc and
path mirrors, dirgate, telemetry coverage, and the golden-corpus gates
selected by the changed paths.

## Related docs

- [Projector architecture](../README.md)
- [Intent contract](../intent/README.md)
- [Package restructure](../../../../docs/internal/design/package-restructure.md)
