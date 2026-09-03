# AWS cloud-runtime-drift projector intents

## Purpose

This package recognizes AWS runtime observation for one scope generation and
builds the reducer intent that asks the reducer to run its bounded AWS ARN
join against active Terraform-state and Terraform-config facts and
re-classify runtime drift for the scope (issue #6053 epic). The trigger is
`aws_resource` fact presence: the projector stays source-local and never
joins AWS resources to Terraform evidence itself, so any AWS resource
observation is reason enough to ask the reducer to re-run its own join.

## Ownership boundary

The package owns only the AWS cloud-runtime-drift trigger selection and
reducer-intent value. The root `internal/projector` package validates
scope-generation boundaries, constructs and owns the immutable fact lookup,
preserves family order, and owns projection lifecycle, queue writes, retries,
and telemetry. The reducer's `DomainAWSCloudRuntimeDrift` handler
(`go/internal/reducer/aws_cloud_runtime_drift.go`) owns the bounded ARN join
against `AWSCloudRuntimeDriftEvidenceLoader`, drift classification through the
`aws_cloud_runtime_drift` correlation rule pack, and durable publication of
admitted candidates through `AWSCloudRuntimeDriftFindingWriter`.

## Exported surface

- `BuildAWSCloudRuntimeDriftReducerIntent` builds the
  `aws_cloud_runtime_drift` intent, anchored to the first `aws_resource` fact
  observed in the generation.

See `doc.go` for the full godoc contract.

## Dependencies

The builder depends on `internal/projector/intent.FactLookup`,
`internal/projector/intent.ReducerIntent`, and
`internal/projector/intent.SourceSystem`; this package must not import the
root projector package — root already imports this package to dispatch to it,
so the reverse import would cycle. The builder triggers on fact presence only
and never decodes the payload, so it carries no `factschema_decode_*` seam.

## Telemetry

No-Observability-Change: this package emits no signal directly. Root intent
enqueue remains covered by `eshu_dp_reducer_intents_enqueued_total`; the
reducer's evidence load keeps the `reducer.aws_runtime_drift_evidence_load`
span (`telemetry.SpanReducerAWSRuntimeDriftEvidenceLoad`). Moving the pure
builder adds no queue, storage, graph, span, metric, or log boundary.

## Gotchas / invariants

- The trigger is `aws_resource` presence, anchored to the earliest such fact
  in original input order so the reducer claim is stable across
  reprojections. It is not gated on any Terraform-state or Terraform-config
  fact — the reducer, not the projector, decides whether a drift finding
  exists.
- The entity key is `aws_cloud_runtime_drift:<scope>`, distinct from the
  `aws_resource_materialization:<scope>` key the AWS node and cloud-image
  builders share. This domain has no canonical-nodes readiness dependency on
  another AWS builder's phase publication.
- `SourceSystem` is the shared two-tier `projectorintent.SourceSystem`: a
  trimmed `SourceRef.SourceSystem`, falling back to a trimmed
  `CollectorKind`. The pre-extraction root helper
  (`awsCloudRuntimeDriftSourceSystem`) had the identical two-tier body, and
  the child tests pin both tiers. That helper still has two other root
  callers after this extraction (`aws_resource_materialization_intents.go`
  and `observability_coverage_materialization_intents.go`, since extracted to
  `../observabilitycoveragematerialization/materialization_intents.go`); both
  were
  repointed to `projectorintent.SourceSystem` directly rather than left
  calling a now-deleted root function.
- Do not decode the payload, run the ARN join, or classify drift here. The
  reducer handler owns the evidence load, correlation-rule classification,
  and the durable write; this package only decides whether to ask for it.
- Do not move lookup construction, assembly, queue writes, retries, graph
  writes, or telemetry into this package.

## Verification

Run the package contract tests, ordered fan-out parity and probe-count tests,
the projector package tree, package-doc and path mirrors, dirgate, telemetry
coverage, and the golden-corpus gates selected by the changed paths.

No-Regression Evidence: this extraction moves one builder without changing
its trigger, value, or fan-out position. The reducer intent domain, entity
key, reason, anchor selection, and source-system derivation are identical to
the base commit, and the dispatcher's ordered fan-out is unchanged at 44
builder probes with this probe still running immediately after
`packagesource.BuildPackageSourceCorrelationReducerIntent` and immediately
before `multicloudruntimedrift.BuildMultiCloudRuntimeDriftReducerIntent`. The root
`awsCloudRuntimeDriftSourceSystem` helper it called was compared body-for-body
against its `projectorintent.SourceSystem` replacement (both trim
`SourceRef.SourceSystem` and fall back to a trimmed `CollectorKind`, with no
third literal fallback in either), so the substitution used both here and at
this helper's two remaining root call sites is behavior-identical by
construction, and the child pins both tiers. Focused proof, run from the `go/`
module root:
`go test ./internal/projector/awscloudruntimedrift/... -run TestBuildAWSCloudRuntimeDriftReducerIntent -count=1`
and `go test ./internal/projector/... -count=1` green, whole-module `go build`
and `go vet` clean.

## Related docs

- [Projector architecture](../README.md)
- [Intent contract](../intent/README.md)
- [Package restructure](../../../../docs/internal/design/package-restructure.md)
