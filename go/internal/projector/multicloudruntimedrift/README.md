# Multi-cloud runtime drift projector intents

## Purpose

This package recognizes provider-neutral cloud-inventory evidence in one
scope generation and builds the reducer intent that asks the reducer to run
its `multi_cloud_runtime_drift` join. The trigger fires on the earliest
`gcp_cloud_resource` or `azure_cloud_resource` fact (issue #5759, closing the
"registered but never enqueued" gap left since #1997/#1998). `aws_resource`
facts alone never trigger this intent: `DomainAWSCloudRuntimeDrift` already
publishes AWS runtime-drift findings end-to-end
(`reducer_aws_cloud_runtime_drift_finding`), so an AWS-only scope generation
must not enqueue this domain at all — doing so would load evidence and
evaluate candidates purely to filter every one of them away.

A scope that carries AWS facts alongside GCP/Azure facts still enqueues here
for its GCP/Azure coverage. The shared `cloud_resource_uid` evidence loader
(`PostgresMultiCloudRuntimeDriftEvidenceLoader`) joins all three providers'
inventory facts into one keyspace for implementation reuse, so it can still
return AWS rows in that case; the reducer's `MultiCloudRuntimeDriftHandler.Handle`
`excludeAWSOwnedRows` is the publish-time filter that drops them before any
finding is written, so the two domains never disagree about the same AWS
resource.

## Ownership boundary

The package owns only trigger selection and the reducer-intent value. The
root `internal/projector` package validates scope-generation boundaries,
constructs and owns the immutable fact lookup, preserves family order, and
owns projection lifecycle, queue writes, retries, and telemetry. The
reducer's `DomainMultiCloudRuntimeDrift` handler
(`go/internal/reducer/multi_cloud_runtime_drift.go` and its sibling files)
owns the bounded `cloud_resource_uid` join, provider partitioning
(`excludeAWSOwnedRows`), and the `reducer_multi_cloud_runtime_drift_finding`
write; none of that happens here.

## Exported surface

- `BuildMultiCloudRuntimeDriftReducerIntent` builds the
  `multi_cloud_runtime_drift` intent, anchored to the earliest accepted
  `gcp_cloud_resource`/`azure_cloud_resource` fact.

See `doc.go` for the full godoc contract.

## Dependencies

The builder depends on `internal/projector/intent.FactLookup`,
`internal/projector/intent.ReducerIntent`, and
`internal/projector/intent.SourceSystem`; this package must not import the
root projector package — root already imports this package to dispatch to
it, so the reverse import would cycle. It reads `internal/reducer` for the
domain constant and `internal/facts` for the two candidate fact-kind
constants.

This family carries no decode seam: `triggerFact` accepts every envelope
`FirstAcrossKinds` hands it; `candidateFactKinds` alone restricts the scan to
`gcp_cloud_resource` and `azure_cloud_resource`. No payload field is read.

## Telemetry

No-Observability-Change: this package emits no signal directly. Root intent
enqueue remains covered by `eshu_dp_reducer_intents_enqueued_total`; the
reducer handler retains its own `reducer_multi_cloud_runtime_drift_finding`
write telemetry. Moving the pure builder adds no queue, storage, graph,
span, metric, or log boundary.

## Gotchas / invariants

- The candidate-kind list (`candidateFactKinds`) must stay in sync with what
  the reducer's evidence loader treats as GCP/Azure inventory — it exists so
  `FirstAcrossKinds` can skip kinds the generation does not carry, not to
  change admission.
- `SourceSystem` is the shared two-tier `projectorintent.SourceSystem`
  (trimmed `SourceRef.SourceSystem`, else trimmed `CollectorKind`). The
  pre-extraction root helper (`multiCloudRuntimeDriftSourceSystem`) had the
  identical two-tier body, so this is NOT a behavior change — do not
  reintroduce a package-local copy.
- **The root fan-out parity fixture DOES cover this domain**, unlike some
  sibling families in this series.
  `reducer.DomainMultiCloudRuntimeDrift` appears in both
  `fanOutParityExpectations` and `fanOutParityExpectedOrder` in
  `../scope_generation_intents_fanout_parity_test.go`, and the shared fixture
  in `../scope_generation_intents_fanout_test.go` carries both a
  `gcp_cloud_resource` fact (`gcp-resource-1`) and an `azure_cloud_resource`
  fact (`azure-resource-1`), so the parity fixture is a second safety net for
  this family's reason string, entity key, and source-system derivation, on
  top of this package's own tests.
- `aws_resource` facts alone must never trigger this domain. That exclusion is
  deliberate (see Purpose above), not an oversight to "fix" by adding
  `facts.AWSResourceFactKind` to `candidateFactKinds`.

## Related docs

- [Projector architecture](../README.md)
- [Intent contract](../intent/README.md)
- [Reducer multi-cloud-runtime-drift architecture](../../reducer/multi-cloud-runtime-drift.md)
- [Package restructure](../../../../docs/internal/design/package-restructure.md)

No-Regression Evidence: this extraction moves one builder without changing its
trigger, value, or fan-out position. The reducer intent domain
(`DomainMultiCloudRuntimeDrift`), the `multi_cloud_runtime_drift:<scope>`
entity key, the `gcp or azure cloud resource facts observed` reason string,
and the fact-id selection are identical to the base commit; only the scope
and generation identifiers changed from struct-field reads to parameters,
carrying the same values from the call site. The dispatcher's ordered fan-out
is unchanged at 44 builder probes on both sides, with this probe still
running immediately after `buildAWSCloudRuntimeDriftReducerIntent` and
immediately before `buildAWSResourceMaterializationReducerIntent`.

The family's private `multiCloudRuntimeDriftSourceSystem` helper was compared
body-for-body against `projectorintent.SourceSystem` and found identical --
two tiers, both trimmed, no third literal fallback -- so it was dropped in
favour of the shared seam rather than moved. The package's own
`...SourceSystemPrefersSourceRef` test sets the two tiers to different values
(`azure_live` against `azure_scanner`), so a regression that swapped the tier
order fails it; a test giving both tiers the same value would pass either way
and prove only that a label was produced. The companion
`...SourceSystemFallsBackToCollectorKind` test leaves `SourceRef.SourceSystem`
unset and asserts the trimmed `CollectorKind` alone, proving the fallback
fires when the first tier is genuinely blank.

Unlike some sibling families, the root fan-out parity fixture DOES cover this
domain: `DomainMultiCloudRuntimeDrift` appears in both
`fanOutParityExpectations` and `fanOutParityExpectedOrder`, and the shared
fixture carries both a `gcp_cloud_resource` and an `azure_cloud_resource`
fact. Verified by reading
`../scope_generation_intents_fanout_parity_test.go` and
`../scope_generation_intents_fanout_test.go` directly rather than assumed
from a sibling family's finding.

No-Observability-Change: no metric, span, log, or quarantine counter is added,
moved, or renamed by this extraction. Root assembly and
`eshu_dp_reducer_intents_enqueued_total` are untouched, the
`multi_cloud_runtime_drift` domain keeps its existing reducer-side write
telemetry, and the two files under this package emit no signal of their own
-- `multi_cloud_runtime_drift_intents.go` is a pure trigger-and-value builder
with no I/O. The telemetry-coverage row for it was updated in place from the
row already covering the pre-move root file, repointed to this package's
path.
