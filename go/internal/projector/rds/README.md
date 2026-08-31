# RDS projector intents

## Purpose

This package recognizes the AWS `rds_instance_posture` fact for one scope
generation and builds the reducer intent that lets the reducer project bounded
posture metadata (encryption, backup, deletion-protection, IAM database auth,
and public-endpoint exposure) onto already materialized RDS `CloudResource`
nodes.

## Ownership boundary

The package owns only the RDS posture trigger selection and reducer-intent
value. The root `internal/projector` package validates scope-generation
boundaries, constructs and owns the immutable fact lookup, preserves family
order, and owns projection lifecycle, queue writes, retries, and telemetry.
Reducer handlers own payload validation, CloudResource readiness gating, graph
property writes, and readiness publication.

## Exported surface

- `BuildRDSPostureMaterializationReducerIntent` builds the
  `rds_posture_materialization` reducer intent from `rds_instance_posture`
  facts, anchored to the earliest matching fact.

See `doc.go` for the full godoc contract.

## Dependencies

The builder depends on `internal/projector/intent.FactLookup` and
`internal/projector/intent.ReducerIntent`; this package must not import the
root projector package -- root already imports this package to dispatch to
it, so the reverse import would cycle. The builder triggers on fact-kind
presence only and does not decode the posture payload, so this package carries
no `factschema_decode_aws.go` seam (unlike `ec2`'s `USES_PROFILE` builder or
`s3`'s `LOGS_TO` builder, which each decode one field to gate their trigger).

## Telemetry

No-Observability-Change: this package emits no signal directly. Root intent
enqueue remains covered by `eshu_dp_reducer_intents_enqueued_total`; reducer
handlers retain execution, readiness, and graph-write telemetry. Moving the
pure builder adds no queue, storage, graph, span, metric, or log boundary.

## Gotchas / invariants

- The builder's `EntityKey` is `aws_resource_materialization:<scope>` -- NOT a
  family-distinct key -- because it gates on the same generic AWS
  `CloudResource` canonical-nodes phase the RDS instance node commits under,
  matching the S3 builders rather than EC2's family-distinct entity keys.
- Both public and private RDS instances enqueue: encryption, backup,
  deletion-protection, IAM database auth, and operational posture remain
  queryable evidence even when no public endpoint exists. Do not add a
  publicly-accessible filter to the trigger.
- The builder anchors to the earliest matching `rds_instance_posture` fact so
  the reducer claim stays stable across reprojections of the same generation.

## Verification

Run the package contract tests, root RDS assembly tests, ordered fan-out
parity and probe-count tests, the projector package tree, package-doc and path
mirrors, dirgate, and telemetry coverage.

No-Regression Evidence: this extraction moves one builder without changing its
trigger, value, or position in the dispatcher's ordered fan-out.
`reducer.DomainRDSPostureMaterialization` intent emission is identical to the
base commit, and the dispatcher's ordered fan-out is unchanged at 44 builder
probes with the RDS probe at its original position: in
`scope_generation_intents.go` it still runs immediately after
`BuildExternalPrincipalGrantMaterializationReducerIntent` and immediately
before `BuildInstanceIdentityMaterializationReducerIntent`. That neighbour
pair is the property that matters, and it is cited instead of an absolute
line number because every later family extraction adds an import and shifts
the line. `projectorintent.SourceSystem` was
compared body-for-body against the root `awsCloudRuntimeDriftSourceSystem` it
replaces (both trim `SourceRef.SourceSystem` and fall back to
`CollectorKind`) rather than by name. Focused proof:
`go test ./internal/projector/... -count=1` green, whole-module `go build` and
`go vet` clean.

## Related docs

- [Projector architecture](../README.md)
- [Intent contract](../intent/README.md)
- [Package restructure](../../../../docs/internal/design/package-restructure.md)
