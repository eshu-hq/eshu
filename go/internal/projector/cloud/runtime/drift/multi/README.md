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
(`go/internal/reducer/multicloudruntimedrift/multi_cloud_runtime_drift.go` and its sibling files)
owns the bounded `cloud_resource_uid` join, provider partitioning
(`excludeAWSOwnedRows`), and the `reducer_multi_cloud_runtime_drift_finding`
write; none of that happens here.

## Exported surface

- `BuildReducerIntent` builds the
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
  `../../../../scope_generation_intents_fanout_parity_test.go`, and the shared fixture
  in `../../../../scope_generation_intents_fanout_test.go` carries both a
  `gcp_cloud_resource` fact (`gcp-resource-1`) and an `azure_cloud_resource`
  fact (`azure-resource-1`), so the parity fixture is a second safety net for
  this family's reason string, entity key, and source-system derivation, on
  top of this package's own tests.
- `aws_resource` facts alone must never trigger this domain. That exclusion is
  deliberate (see Purpose above), not an oversight to "fix" by adding
  `cloud.AWSResourceFactKind` to `candidateFactKinds`.

## Related docs

- [Projector architecture](../../../../README.md)
- [Intent contract](../../../../intent/README.md)
- [Reducer multi-cloud-runtime-drift architecture](../../../../../reducer/multi-cloud-runtime-drift.md)
- [Package restructure](../../../../../../../docs/internal/design/package-restructure.md)

During the earlier #6057 extraction, the family's private
`multiCloudRuntimeDriftSourceSystem` helper was compared
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
`../../../../scope_generation_intents_fanout_parity_test.go` and
`../../../../scope_generation_intents_fanout_test.go` directly rather than assumed
from a sibling family's finding.
