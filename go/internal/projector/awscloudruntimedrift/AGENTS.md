# AGENTS.md — AWS cloud-runtime-drift projector intent guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` and `../README.md` for projector-wide invariants.
3. `../intent/AGENTS.md` for the neutral builder contract.
4. `../scope_generation_intents.go` for root-owned assembly order; this probe
   runs first, immediately after the package-source-correlation probe and
   before the multi-cloud-runtime-drift probe.
5. `go/internal/reducer/aws_cloud_runtime_drift.go` and
   `aws_cloud_runtime_drift_writer.go` for what the reducer does with the
   intent this package enqueues: the bounded ARN join, correlation-rule
   classification, and the durable candidate write.

## Invariants

- Import `internal/projector/intent`, never the root projector package. Root
  imports this package to dispatch, so the reverse import cycles.
- `BuildAWSCloudRuntimeDriftReducerIntent` triggers on the mere presence of an
  `aws_resource` fact and anchors to the earliest one in original input order
  (`FirstOfKind`). It does not inspect any Terraform-state or
  Terraform-config fact — the reducer's evidence loader owns that join, not
  the trigger.
- The entity key is `aws_cloud_runtime_drift:<scope>`. It is deliberately NOT
  `aws_resource_materialization:<scope>` — this domain has no
  canonical-nodes readiness dependency on another AWS builder's phase
  publication, unlike the AWS cloud-image and workload-cloud builders.
- `SourceSystem` is the shared two-tier `projectorintent.SourceSystem`: a
  trimmed `SourceRef.SourceSystem`, falling back to a trimmed
  `CollectorKind`. The pre-extraction root helper
  (`awsCloudRuntimeDriftSourceSystem`) had the identical two-tier body.
- Do not decode the payload, run the ARN join, or classify drift here. The
  reducer handler owns the evidence load, correlation-rule classification,
  and the durable write; an unscanned or unmanaged resource is a reducer-side
  classification outcome, not a trigger-side decision.
- Do not move lookup construction, assembly, queue writes, retries, graph
  writes, or telemetry into this package.

## Common changes

- **Changing the reason string or the entity key.** Both are asserted
  verbatim by the package tests and by the root fan-out parity fixture
  (`../scope_generation_intents_fanout_parity_test.go`); change them
  together.
- **Changing the trigger kind.** This is a correctness decision, not a
  cleanup: gating on anything other than bare `aws_resource` presence changes
  when the reducer gets a chance to re-run its ARN join and re-classify
  drift, including retraction cases. Update the root dispatch tests in
  `../aws_cloud_runtime_drift_projection_test.go` in the same change.

## Failure modes

- **A route-serves-data registry citation was checked and found absent.**
  `go/internal/mcp/route_serves_data_registry.go` and
  `route_serves_data_registry_routes.go` cite several other projector files
  by full path and read them for a marker string; neither cites this file or
  its pre-extraction root path
  (`go/internal/projector/aws_cloud_runtime_drift_intents.go`) — verified
  with a positive control against the registry's `cloudinventory` citations,
  which do cite by path. `TestRouteServesDataRegistry` in
  `go/internal/mcp/` is still run on every change to this family as a
  regression guard, not because a citation was found.
- **`docs/public/observability/telemetry-coverage.md` carries no row citing
  the pre-extraction file path either** — checked before the move; nothing to
  repoint.
- **This family shared two test fixtures with 18 other root test files
  before the extraction.** `intentForDomain` (14 dependents) and
  `awsResourceEnvelope` (4 dependents) both lived in the pre-extraction root
  test file. They moved to a new root file,
  `../reducer_intent_test_helpers_test.go`, rather than into this package —
  they are dispatch-level fixtures for `buildProjection`, a root-only
  function this package cannot call. Do not re-introduce a copy of either
  helper here; the child's own tests build fixtures locally
  (`resourceEnvelope` in `reducer_intent_test.go`), matching the
  `awscloudimage` precedent.
- **The pre-extraction root test file also covered an unrelated family.**
  `aws_cloud_runtime_drift_intents_test.go` carried the only dispatch-level
  test coverage for `aws_resource_materialization_intents.go` (a builder
  that is NOT extracted and stays at root). That coverage moved into a new
  root file matching the builder's own name,
  `../aws_resource_materialization_intents_test.go`, which previously had no
  dedicated test file at all.
- **`awsCloudRuntimeDriftSourceSystem` had two other root callers at
  extraction time**, not one: `aws_resource_materialization_intents.go` and
  `observability_coverage_materialization_intents.go`. Both were repointed to
  `projectorintent.SourceSystem` in the same commit that moved this file, so
  the helper's definition could be dropped instead of duplicated. A future
  reader who finds a compile error referencing
  `awsCloudRuntimeDriftSourceSystem` in root should repoint the caller to
  `projectorintent.SourceSystem`, not resurrect the helper.

## Anti-patterns

- Do not add a package-local source-system helper; the two-tier
  `projectorintent.SourceSystem` IS the pre-extraction behavior here.
- Do not import the root `projector` package. Root imports this package to
  dispatch, and the reverse direction is an import cycle.
- Do not widen the export surface past
  `BuildAWSCloudRuntimeDriftReducerIntent`. Every sibling family in this
  series exports exactly one builder and no types.

## Changes needing ADR review

- Adding a decode seam. This family triggers on fact presence alone; families
  that need one keep a local decode call against `sdk/go/factschema` rather
  than importing root's wrapper, and that split is a design decision rather
  than a local call.
- Changing `reducer.DomainAWSCloudRuntimeDrift`, the `aws_resource` trigger,
  the `aws_cloud_runtime_drift:<scope>` entity key, or the two-tier
  source-system label. All are contract surface the reducer handler and the
  fan-out parity fixture assert against.

## Verification

Use TDD. Run the focused child tests, the root dispatcher tests in
`../aws_cloud_runtime_drift_projection_test.go`, the root ordered fan-out
parity and probe-count tests, `go test ./internal/mcp/ -run
TestRouteServesDataRegistry`, package-doc verification, the projector package
tree, telemetry coverage, and the golden-corpus gates selected by the changed
paths.
