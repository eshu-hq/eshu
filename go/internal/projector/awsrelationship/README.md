# AWS relationship projector intents

## Purpose

This package recognizes AWS `aws_relationship` facts for one scope generation
and builds the reducer intent that asks the reducer to join those facts
against committed `CloudResource` nodes and write canonical AWS relationship
graph edges (issue #805, PR 2 of the node-then-edge design).

## Ownership boundary

The package owns only the AWS relationship trigger selection and
reducer-intent value. The root `internal/projector` package validates
scope-generation boundaries, constructs and owns the immutable fact lookup,
preserves family order, and owns projection lifecycle, queue writes, retries,
and telemetry. The reducer's `DomainAWSRelationshipMaterialization` handler
owns the bounded relationship join, the canonical-nodes readiness check, edge
label selection, the backend-neutral edge write, and readiness publication.

## Exported surface

- `BuildAWSRelationshipMaterializationReducerIntent` builds the
  `aws_relationship_materialization` intent, anchored to the first
  `aws_relationship` fact observed in the generation.

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
reducer handler retains the `reducer.aws_relationship_materialization` span
and the `eshu_dp_aws_relationship_edges_total` counter. Moving the pure
builder adds no queue, storage, graph, span, metric, or log boundary.

## Gotchas / invariants

- The `EntityKey` is `aws_resource_materialization:<scope>` — NOT a
  family-distinct key. The edge handler's readiness gate resolves the exact
  `GraphProjectionPhaseCanonicalNodesCommitted` row the AWS resource node
  builders publish for the same scope generation, so edges never project
  before nodes commit. The S3, RDS, workload-cloud, and AWS cloud-image
  builders share the same key for the same reason.
- The builder triggers on the mere presence of an `aws_relationship` fact and
  anchors to the earliest one in original input order so the reducer claim is
  stable across reprojections of the same generation. It does not inspect
  `target_type`, `relationship_type`, or either ARN.
- `SourceSystem` is `SourceRef.SourceSystem` trimmed, falling back to a
  trimmed `CollectorKind`; a blank source ref does not drop the intent.
- The projector never writes a relationship edge itself; a relationship whose
  endpoints are not yet canonical nodes is deferred by the reducer's
  readiness gate, not fabricated here.

## Verification

Run the package contract tests, ordered fan-out parity and probe-count tests,
the projector package tree, package-doc and path mirrors, dirgate, telemetry
coverage, and the golden-corpus gates selected by the changed paths.

No-Regression Evidence: this extraction moves one builder without changing
its trigger, value, or fan-out position. The reducer intent domain, entity
key, reason, anchor selection, and source-system derivation are identical to
the base commit, and the dispatcher's ordered fan-out is unchanged at 44
builder probes with this probe still running immediately after
`ec2.BuildInstanceNodeMaterializationReducerIntent` and immediately before
`buildAWSCloudImageMaterializationReducerIntent`. The root
`awsCloudRuntimeDriftSourceSystem` helper it called was compared body-for-body
against its `projectorintent.SourceSystem` replacement (both trim
`SourceRef.SourceSystem` and fall back to a trimmed `CollectorKind`), and the
root `firstOfKind` forwarder was a direct delegate to
`projectorintent.FactLookup.FirstOfKind`, so both substitutions are
behavior-identical by construction; the root helper stays at root because
seven other root builders still call it. Focused proof:
`../scripts/go-test-run-guard.sh 1 'TestBuildAWSRelationshipMaterializationReducerIntent' -- ./internal/projector/awsrelationship -count=1`
and `go test ./internal/projector/... -count=1` green, whole-module `go build`
and `go vet` clean.

## Related docs

- [Projector architecture](../README.md)
- [Intent contract](../intent/README.md)
- [AWS relationship edge materialization design](../../../../docs/internal/aws-relationship-edge-materialization-design.md)
- [Package restructure](../../../../docs/internal/design/package-restructure.md)
