# Observability-coverage-materialization projector intents

## Purpose

This package recognizes AWS-native observability objects in one scope
generation and builds the reducer intent that asks the reducer's
`observability_coverage_materialization` domain to project that generation's
exact coverage decisions into canonical `COVERS` graph edges (issue #391 PR3).
One branch triggers it: an `aws_resource` fact whose decoded `resource_type`
is a CloudWatch alarm, composite alarm, dashboard, logs log group, or X-Ray
sampling rule/group. Without such an object no `COVERS` edge can exist, so no
intent is worth enqueuing.

This is the narrower half of the #391 pair. The sibling
[`observabilitycoverage`](../observabilitycoverage/README.md) package owns the
`observability_coverage_correlation` family, whose trigger also accepts
observability source facts (declared dashboards, alerts, log/trace sources)
and whose entity key is family-distinct.

## Ownership boundary

The package owns only the materialization trigger selection and the
reducer-intent value. The root `internal/projector` package validates
scope-generation boundaries, constructs and owns the immutable fact lookup,
preserves family order, and owns projection lifecycle, queue writes, retries,
and telemetry. The reducer's `DomainObservabilityCoverageMaterialization`
handler (`go/internal/reducer/obscoverage/observability_coverage_materialization.go`)
owns the readiness gate, the edge write, and the counters.

## Exported surface

- `BuildObservabilityCoverageMaterializationReducerIntent` builds the
  `observability_coverage_materialization` intent, anchored to the earliest
  triggering `aws_resource` fact observed in the generation.

That is the whole export surface: one function, no exported types. See
`doc.go` for the full godoc contract.

## Dependencies

The builder depends on `internal/projector/intent.FactLookup`,
`internal/projector/intent.ReducerIntent`, and
`internal/projector/intent.SourceSystem`; this package must not import the
root projector package — root already imports this package to dispatch to it,
so the reverse import would cycle. It decodes `aws_resource` payloads through
this package's own `factschema_decode_aws.go` seam (the `ec2` pattern). Root
had a classified `decodeAWSResource` wrapper; this family was its last caller,
so root's `factschema_decode_aws.go` was deleted in the same change rather
than left as dead code — the same disposition the `containerimageidentity`
and `iamcanassume` extractions applied to their root wrappers.

## Telemetry

No-Observability-Change: this package emits no signal directly. Root intent
enqueue remains covered by `eshu_dp_reducer_intents_enqueued_total`, and the
reducer handler retains `eshu_dp_observability_coverage_edges_total` and the
`reducer.observability_coverage_materialization` span. Moving the pure builder
adds no queue, storage, graph, span, metric, or log boundary. Both new files
carry rows in `docs/public/observability/telemetry-coverage.md`, and the row
for root's deleted `factschema_decode_aws.go` was removed with the file.

## Gotchas / invariants

- `observabilityResourceTypes` is a three-way mirror: this package's copy, the
  sibling correlation family's copy
  (`../observabilitycoverage/correlation_intents.go`), and the reducer's
  `observabilityResourceSignals`
  (`go/internal/reducer/obscoverage/observability_coverage_correlation_index.go`) must
  agree on what counts as an observability object. Add a resource type to all
  three or the triggers and the classifier diverge.
- The entity key is `aws_resource_materialization:<scope>`, deliberately NOT a
  family-distinct key. It is shared with the AWS node builders so the coverage
  edge handler's readiness gate resolves the exact
  `GraphProjectionPhaseCanonicalNodesCommitted` row those builders publish for
  the same acceptance unit — coverage edges never project before the
  CloudResource nodes commit.
- The source-system label is the shared two-tier
  `projectorintent.SourceSystem` (trimmed `SourceRef.SourceSystem`, else
  trimmed `CollectorKind`). Unlike the sibling correlation family there is no
  literal third `"observability"` tier here: a trigger fact with neither label
  yields an empty `SourceSystem`, and a package test pins that.
- An undecodable `aws_resource` payload is not a trigger and not an error —
  the decode error collapses to an empty resource type that never matches the
  set. Root's quarantine path owns flagging the invalid fact.
- The decode wrapper is named `decodeCoverageMaterializationAWSResource`
  because the payload-usage manifest attributes field reads by wrapper name
  across `factschema_decode*.go` seam files, and the sibling already holds
  `decodeObservabilityAWSResource`.

## Verification

Run the package tests, the root dispatcher fan-out and probe-count tests, the
projector package tree, package-doc and path mirrors, dirgate, the
payload-usage manifest gate, and the golden-corpus gates selected by the
changed paths.

No-Regression Evidence: this extraction moves one builder without changing its
trigger, value, or fan-out position. The reducer intent domain, entity key,
reason, anchor selection, and source-system derivation are identical to the
base commit, and the dispatcher's ordered fan-out is unchanged at 44 builder
probes with this probe still running immediately after
`awscloudimage.BuildAWSCloudImageMaterializationReducerIntent` and immediately
before
`observabilitycoverage.BuildObservabilityCoverageCorrelationReducerIntent`.
The pre-extraction builder took root's `*reducerIntentFactIndex` and called
`index.firstOfKindMatching`, a one-line delegate to
`projectorintent.FactLookup.FirstOfKindMatching`; calling the seam method
directly is behavior-identical by construction. It already used
`projectorintent.SourceSystem`, so no source-label helper moved or was
substituted. The child's decode calls the same `factschema.DecodeAWSResource`
through the same `factenvelope.FactSchemaFromInternal` adapter root's deleted
wrapper delegated to, and the sole caller discards the error, so the decode
substitution is behavior-identical for this trigger. This family is covered by
the root fan-out parity fixture: `reducer.DomainObservabilityCoverageMaterialization`
appears in both `fanOutParityExpectations` and `fanOutParityExpectedOrder` in
`../scope_generation_intents_fanout_parity_test.go`, pinning fact ID, entity
key, reason, source system, and position. The root `buildProjection` dispatch
cases stay at root in `../observability_coverage_materialization_intents_test.go`:
one of them asserts BOTH observability domains are absent from an
input-invalid generation, and the file's `observabilityAWSResourceEnvelope`
fixture is also used by `../scope_generation_intents_fanout_test.go`, so it is
not a single-family file the child could adopt.

Focused proof, run from the `go/` module root: `go build ./...`,
`go vet ./internal/projector/...`, and
`go test ./internal/projector/... -count=1` all green. The package tests were
mutation-checked: deleting `aws_xray_group` from `observabilityResourceTypes`
fails `TestBuildObservabilityCoverageMaterializationReducerIntentAcceptsEveryObservabilityType`,
and changing the entity key to a family-distinct one fails
`TestBuildObservabilityCoverageMaterializationReducerIntentAnchorsToObservabilityResource`;
restoring each body returns the package to green.

## Related docs

- [Projector architecture](../README.md)
- [Intent contract](../intent/README.md)
- [Sibling correlation family](../observabilitycoverage/README.md)
- [Package restructure](../../../../docs/internal/design/package-restructure.md)
