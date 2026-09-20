# IAM CAN_PERFORM projector intents

## Purpose

This package recognizes AWS `aws_iam_permission` trustable identity
statements and `aws_resource_policy_permission` resource-policy statements for
one scope generation and builds the reducer intent that asks the reducer to
project them into conservative `CAN_PERFORM` effective-permission edges
between committed IAM `CloudResource` nodes (issue #1134, PR4a/PR4b of the
CAN_PERFORM design).

## Ownership boundary

The package owns only the qualifying-fact trigger selection, the typed decode
that selection needs, and the reducer-intent value. The root
`internal/projector` package validates scope-generation boundaries, constructs
and owns the immutable fact lookup, preserves family order, and owns
projection lifecycle, queue writes, retries, and telemetry. The reducer's
`DomainIAMCanPerformMaterialization` handler
(`IAMCanPerformMaterializationHandler`) owns the closed action catalog, the
bounded ARN join, the skip taxonomy, the canonical-nodes readiness check, the
backend-neutral edge write, and readiness publication.

## Exported surface

- `BuildIAMCanPerformMaterializationReducerIntent` builds the
  `iam_can_perform_materialization` intent, anchored to the earliest fact in
  the generation, across both candidate kinds in original input order, that
  either (a) is an `aws_iam_permission` fact whose payload decodes and carries
  `policy_source` `"inline"` or `"attached_managed"`, or (b) is an
  `aws_resource_policy_permission` fact whose payload decodes.

See `doc.go` for the full godoc contract.

## Dependencies

The builder depends on `internal/projector/intent.FactLookup`,
`internal/projector/intent.ReducerIntent`, and
`internal/projector/intent.SourceSystem`; this package must not import the
root projector package — root already imports this package to dispatch to it,
so the reverse import would cycle. The qualifying-fact predicate decodes the
`aws_iam_permission` and `aws_resource_policy_permission` payloads through
this package's own `factschema_decode_iam.go` (`sdk/go/factschema` plus
`internal/factenvelope` directly), rather than root's classified decode
wrapper or the sibling `cloud/aws/iam/trust` package's independent
`decodeAWSIAMPermission` copy. This package names its own wrappers
`decodeIAMCanPerformAWSIAMPermission` and
`decodeIAMCanPerformAWSResourcePolicyPermission` — distinct from `trust`'s
`decodeAWSIAMPermission` — because
`scripts/verify-payload-usage-manifest.sh` requires every decode seam under
`go/internal/projector` to have a globally unique function name; two packages
independently decoding the same fact kind for different trigger predicates is
expected, but the gate keys seams by bare identifier. The sole caller only
checks `err != nil`, discarding the decode error's classification, so the
direct `factschema.DecodeAWSIAMPermission` /
`factschema.DecodeAWSResourcePolicyPermission` calls plus
fact-kind-labeled wrapping are behavior-identical for that check. This mirrors
`ec2`'s `decodeEC2InstancePosture` and `trust`'s own independent
`decodeAWSIAMPermission` copy: the repo keeps decode wrappers per package
rather than shared. The file keeps the `factschema_decode_*.go` name so
`scripts/verify-payload-usage-manifest.sh` (issue #4573), which globs
`factschema_decode*.go` under `go/internal/projector` and AST-scans each
function for a `factschema.FactKindXxx` reference, still discovers it as a
decode seam.

## Telemetry

No-Observability-Change: this package emits no signal directly. Root intent
enqueue remains covered by `eshu_dp_reducer_intents_enqueued_total`; the
reducer handler retains the `reducer.iam_can_perform_materialization` span,
the `eshu_dp_iam_can_perform_edges_total` / `eshu_dp_iam_can_perform_skipped_total`
/ `eshu_dp_iam_can_perform_conditioned_total` counters, and its structured
completion log. A permission fact that fails decode is skipped as a candidate
with no operator-visible dead-letter. This package adds no queue, storage,
graph, span, metric, or log boundary.

## Gotchas / invariants

- The `EntityKey` is `aws_resource_materialization:<scope>` — NOT a
  family-distinct key. The edge handler's readiness gate resolves the exact
  `GraphProjectionPhaseCanonicalNodesCommitted` row the AWS resource node
  builders publish for the same scope generation, so CAN_PERFORM edges never
  project before the IAM principal and target `CloudResource` nodes commit.
  The trust (`CAN_ASSUME`), S3, RDS, workload-cloud, AWS relationship, and AWS
  cloud-image builders share the same key for the same reason.
- The builder anchors to the earliest qualifying fact across BOTH candidate
  kinds in original input order (`FirstAcrossKinds`), not "earliest of the
  first-checked kind" — an `aws_resource_policy_permission` fact earlier in
  the generation than any qualifying `aws_iam_permission` fact anchors the
  intent. A trust statement (`policy_source == "trust"`) never qualifies here:
  that is the sibling `trust` package's trigger.
  `iamCanPerformIdentityPolicySources` duplicates the reducer's
  `iamCanPerformPolicySourceInline` / `iamCanPerformPolicySourceAttachedManaged`
  constants (`internal/reducer/iamcan/iam_can_perform_catalog.go`) so the
  projector does not import the reducer package for two strings. Change both
  together.
- A candidate fact whose payload fails decode (for example a missing
  `principal_arn` or `resource_arn`) is skipped and the scan continues rather
  than failing the build. A generation with only trust statements, or no IAM
  permission evidence at all, enqueues nothing.
- `SourceSystem` is `SourceRef.SourceSystem` trimmed, falling back to a
  trimmed `CollectorKind`; a blank source ref does not drop the intent.
- The projector never writes a `CAN_PERFORM` edge, resolves a grant, or
  classifies a target resource type here; the reducer's
  `DomainIAMCanPerformMaterialization` handler owns the closed catalog, the
  bounded ARN join, and the `iam_can_perform_edge_writer` call.

## Verification

Run the package contract tests, ordered fan-out parity and probe-count tests,
package-doc verification, the payload-usage manifest gate, the projector
package tree, and the golden-corpus gates selected by the changed paths.

Focused proof: `go test ./internal/projector/cloud/aws/iam/perform/... -count=1`
and `go test ./internal/projector/... -count=1` green, whole-module `go build`
and `go vet` clean.

## Related docs

- [Projector architecture](../../../../README.md)
- [Intent contract](../../../../intent/README.md)
- [IAM CAN_ASSUME projector intents](../trust/README.md)
- [IAM CAN_PERFORM design](../../../../../../../docs/internal/design/1134-iam-can-perform.md)
