# AWS cloud-image projector intents

## Purpose

This package recognizes AWS runtime observation for one scope generation and
builds the reducer intent that asks the reducer to project the generation's
`lambda_function_uses_image` `aws_relationship` facts into canonical
`(:CloudResource)-[:AWS_lambda_function_uses_image]->(:ContainerImage)` graph
edges (issue #5450). The trigger is `aws_resource` fact presence — the same
persistent signal the AWS resource node builder uses — NOT
`lambda_function_uses_image` relationship presence: a generation where a
Lambda function switched from an Image package to Zip drops the relationship
fact yet must still enqueue so the handler's retract-first logic can retract
the prior edge (retraction-safety fix, issue #5450 follow-up review).

## Ownership boundary

The package owns only the AWS cloud-image trigger selection and
reducer-intent value. The root `internal/projector` package validates
scope-generation boundaries, constructs and owns the immutable fact lookup,
preserves family order, and owns projection lifecycle, queue writes, retries,
and telemetry. The reducer's `DomainAWSCloudImageMaterialization` handler
(`AWSCloudImageMaterializationHandler`, wired in
`go/internal/reducer/defaults_additive_domains_cloud_relationships.go`) owns
the retract-first edge lifecycle, the `sourceNodesReady` readiness check
against the `CloudResource`-keyspace canonical-nodes-committed phase, the
`target_not_materialized` reclassification through its
`ContainerImageExistence` lookup, and the backend-neutral edge write and
retract through `CloudResourceContainerImageEdgeWriter`.

## Exported surface

- `BuildAWSCloudImageMaterializationReducerIntent` builds the
  `aws_cloud_image_materialization` intent, anchored to the first
  `aws_resource` fact observed in the generation.

See `doc.go` for the full godoc contract.

## Dependencies

The builder depends on `internal/projector/intent.FactLookup`,
`internal/projector/intent.ReducerIntent`, and
`internal/projector/intent.SourceSystem`; this package must not import the
root projector package — root already imports this package to dispatch to it,
so the reverse import would cycle. The builder triggers on fact presence only
and never decodes the payload, so it carries no `factschema_decode_*` seam,
unlike `ec2`'s and `s3`'s decode-bearing builders.

## Telemetry

No-Observability-Change: this package emits no signal directly. Root intent
enqueue remains covered by `eshu_dp_reducer_intents_enqueued_total`; the
reducer handler retains the `reducer.aws_cloud_image_materialization` span
and the `eshu_dp_aws_cloud_image_edges_total` counter. Moving the pure
builder adds no queue, storage, graph, span, metric, or log boundary.

## Gotchas / invariants

- The `EntityKey` is `aws_resource_materialization:<scope>` — NOT a
  family-distinct key. The handler's `sourceNodesReady` gate resolves the
  exact `GraphProjectionPhaseCanonicalNodesCommitted` row
  `DomainAWSResourceMaterialization` publishes on the
  `GraphProjectionKeyspaceCloudResourceUID` keyspace for the same scope
  generation, so the edge never projects before its source Lambda-function
  `CloudResource` node commits. The AWS relationship, S3, RDS, and
  workload-cloud builders share the same key for the same reason.
- The trigger is `aws_resource` presence, anchored to the earliest such fact
  in original input order so the reducer claim is stable across
  reprojections. Do NOT "tighten" it to `lambda_function_uses_image`
  relationship presence: that reopens the #5450 stale-edge leak, because a
  relationship-less generation would enqueue nothing and the handler's
  retract-first pass would never run.
- There is no readiness phase for the target `:ContainerImage` node. OCI
  registry canonical nodes materialize through the source-local projector
  path, so an unscanned image is a graceful handler-side miss (reclassified
  as `target_not_materialized`), not a second gate here.
- `SourceSystem` is `SourceRef.SourceSystem` trimmed, falling back to a
  trimmed `CollectorKind`; a blank source ref does not drop the intent.
- The projector never writes the edge itself; retraction, readiness, target
  existence, and the MATCH-MATCH-MERGE all belong to the reducer handler.

## Verification

Run the package contract tests, ordered fan-out parity and probe-count tests,
the projector package tree, package-doc and path mirrors, dirgate, telemetry
coverage, and the golden-corpus gates selected by the changed paths.

No-Regression Evidence: this extraction moves one builder without changing
its trigger, value, or fan-out position. The reducer intent domain, entity
key, reason, anchor selection, and source-system derivation are identical to
the base commit, and the dispatcher's ordered fan-out is unchanged at 44
builder probes with this probe still running immediately after
`awsrelationship.BuildAWSRelationshipMaterializationReducerIntent` and
immediately before
`observabilitycoveragematerialization.BuildObservabilityCoverageMaterializationReducerIntent`.
The root `awsCloudRuntimeDriftSourceSystem` helper it called was compared
body-for-body against its `projectorintent.SourceSystem` replacement (both
trim `SourceRef.SourceSystem` and fall back to a trimmed `CollectorKind`,
with no third literal fallback in either), so the substitution is
behavior-identical by construction and the child pins both tiers
(mutation-tested: a single-tier CollectorKind-only substitute fails the child
pin, the two-tier body passes); the root helper stays at root for its four
remaining root callers. The root `firstOfKind` forwarder was a direct
delegate to `projectorintent.FactLookup.FirstOfKind`, so that substitution is
behavior-identical by construction too. Focused proof, run from the `go/`
module root:
`../scripts/go-test-run-guard.sh 1 'TestBuildAWSCloudImageMaterializationReducerIntent' -- ./internal/projector/awscloudimage -count=1`
and `go test ./internal/projector/... -count=1` green, whole-module `go build`
and `go vet` clean.

## Related docs

- [Projector architecture](../README.md)
- [Intent contract](../intent/README.md)
- [AWS relationship edge materialization design](../../../../docs/internal/aws-relationship-edge-materialization-design.md)
- [Package restructure](../../../../docs/internal/design/package-restructure.md)
