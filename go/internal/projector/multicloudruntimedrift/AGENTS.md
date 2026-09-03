# AGENTS.md — multi-cloud runtime drift projector intent guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` and `../README.md` for projector-wide invariants, including
   the rule that the projector never makes cross-source admission decisions.
3. `../intent/AGENTS.md` for the neutral builder contract.
4. `../scope_generation_intents.go` for root-owned assembly order; this probe
   runs after `buildAWSCloudRuntimeDriftReducerIntent` and before
   `buildAWSResourceMaterializationReducerIntent`.
5. `go/internal/reducer/multi-cloud-runtime-drift.md` and
   `go/internal/reducer/multi_cloud_runtime_drift.go`: what the reducer does
   with the intent this package enqueues, including the bounded
   `cloud_resource_uid` join and the `excludeAWSOwnedRows` provider
   partitioning.

## Invariants

- Import `internal/projector/intent`, never the root projector package. Root
  imports this package to dispatch, so the reverse import cycles.
- `BuildMultiCloudRuntimeDriftReducerIntent` fires on the earliest
  `gcp_cloud_resource` or `azure_cloud_resource` fact. `aws_resource` facts
  alone must NEVER trigger this domain — `DomainAWSCloudRuntimeDrift` already
  owns AWS runtime-drift findings end-to-end, so enqueuing here for an
  AWS-only generation would be pure overhead (the reducer would load
  evidence, evaluate candidates, and filter every one away).
- `candidateFactKinds` must mirror every kind `triggerFact` is meant to admit.
  `FirstAcrossKinds` uses the list to skip kinds the generation does not
  carry before evaluating the predicate — the list exists for that pruning,
  not to change admission. `triggerFact` itself accepts every envelope of a
  candidate kind unconditionally.
- Keep the `Reason` string (`"gcp or azure cloud resource facts observed"`)
  and the `multi_cloud_runtime_drift:<scope>` entity key byte-identical. The
  reducer claims one intent per scope generation and reloads the
  generation's facts itself. **The root fan-out parity fixture
  (`../scope_generation_intents_fanout_parity_test.go` and
  `../scope_generation_intents_fanout_test.go`) DOES cover this domain** —
  `reducer.DomainMultiCloudRuntimeDrift` appears in both
  `fanOutParityExpectations` and `fanOutParityExpectedOrder`, and the shared
  fixture carries both a `gcp_cloud_resource` and an `azure_cloud_resource`
  fact — so a reason/entity-key edit that skips the parity fixture is still
  caught there, on top of this package's own tests.
- `SourceSystem` is the shared two-tier `projectorintent.SourceSystem`
  (trimmed `SourceRef.SourceSystem`, else trimmed `CollectorKind`). The
  pre-extraction root helper (`multiCloudRuntimeDriftSourceSystem`) had the
  identical two-tier body, so this is NOT a behavior change — do not
  reintroduce a package-local copy.
- Do not decode a payload field; this builder reads only envelope-level
  fields (`FactKind`, `FactID`, `SourceRef.SourceSystem`, `CollectorKind`).
- Do not move lookup construction, assembly, queue writes, retries, graph
  writes, or telemetry into this package.

## Common changes

- **Adding a third provider (e.g. a new cloud) to the trigger.** Add its
  fact-kind constant to `candidateFactKinds`, decide whether the reducer's
  evidence loader and `excludeAWSOwnedRows`-style partitioning need the same
  addition, and update this package's tests plus the root fan-out fixture
  (`../scope_generation_intents_fanout_test.go`) and parity expectations
  (`../scope_generation_intents_fanout_parity_test.go`) — this domain IS
  covered there, unlike some sibling families.
- **Changing the reason string or the entity key.** Update both this
  package's tests and the fan-out parity fixture's
  `reducer.DomainMultiCloudRuntimeDrift` entry; both currently assert the
  same literal values.

## Failure modes

- **Root dispatcher tests live outside this directory.** The `buildProjection`
  cases for this domain — GCP-only trigger, Azure-only trigger, AWS-only
  non-trigger, and no-cloud-facts non-trigger — stayed at root in
  `../multi_cloud_runtime_drift_projection_test.go` because they call the
  unexported root `buildProjection` dispatcher directly, which this package
  cannot import. A change here can break them without touching any file in
  this directory.
- **Route-serves-data registry.** No entry in
  `go/internal/mcp/route_serves_data_registry*.go` cited the pre-move root
  source file for the `multi_cloud_runtime_drift` domain (verified by `rg`
  across the registry files at extraction time). If a route is ever
  repointed to cite this package's file, run
  `go test ./internal/mcp/ -run TestRouteServesDataRegistry` before landing
  the rename — the registry test reads cited files by path and fails with
  `read ...: no such file` on a rename.
- **A trigger fact with no `SourceRef.SourceSystem` and no `CollectorKind`**
  yields an empty `SourceSystem` rather than a literal default. That is the
  preserved pre-extraction behavior, not a bug to patch in passing.

## Anti-patterns

- Do not add a package-local source-system helper that duplicates
  `projectorintent.SourceSystem`; the two-tier fallback IS the
  pre-extraction behavior here.
- Do not import the root `projector` package. Root imports this package to
  dispatch, and the reverse direction is an import cycle.
- Do not widen the export surface past
  `BuildMultiCloudRuntimeDriftReducerIntent`. Every sibling family in this
  series exports exactly one builder and no types.
- Do not add `facts.AWSResourceFactKind` to `candidateFactKinds`. AWS-only
  triggering for this domain was explicitly removed by issue #5759's
  partitioning fix; re-adding it reintroduces the duplicate-finding bug that
  fix closed.

## Changes needing ADR review

- Changing `reducer.DomainMultiCloudRuntimeDrift`, the candidate fact-kind
  set, the entity key, or the two-tier source-system label. All are contract
  surface the reducer handler and the root fan-out parity fixture both
  assert against.

## Verification

Use TDD. Run the focused child tests, the root dispatcher projection tests,
the root ordered fan-out parity and probe-count tests, the focused
`TestRouteServesDataRegistry` run, package-doc verification, the projector
package tree, and the golden-corpus gates selected by the changed paths.
