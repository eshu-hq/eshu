# AGENTS.md — AWS cloud-image projector intent guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` and `../README.md` for projector-wide invariants.
3. `../intent/AGENTS.md` for the neutral builder contract.
4. `../scope_generation_intents.go` for root-owned assembly order; this probe
   runs after the AWS-relationship probe and before the
   observability-coverage materialization probe.
5. `docs/internal/aws-relationship-edge-materialization-design.md` (§12 and
   the retraction-safety fix note) for the node-before-edge readiness design
   and the trigger rationale this intent encodes.
6. `go/internal/reducer/awscloud/aws_cloud_image_materialization.go` for what the
   reducer does with the intent this package enqueues: retract-first edge
   lifecycle, `sourceNodesReady`, `target_not_materialized` reclassification,
   and the `CloudResourceContainerImageEdgeWriter` calls.

## Invariants

- Import `internal/projector/intent`, never the root projector package. Root
  imports this package to dispatch, so the reverse import cycles.
- `BuildAWSCloudImageMaterializationReducerIntent` triggers on the mere
  presence of an `aws_resource` fact and anchors to the earliest one in
  original input order (`FirstOfKind`). The trigger is deliberately NOT
  `lambda_function_uses_image` relationship presence: AWS is scanned as a
  whole every generation, so `aws_resource` presence is the persistent signal
  that keeps the handler's retract-first pass running in a generation whose
  image relationship disappeared (an Image-to-Zip Lambda switch). Gating on
  relationship presence reopens the #5450 stale-edge leak.
- The entity key is `aws_resource_materialization:<scope>` on purpose. It is
  NOT a family-distinct key: the handler's `sourceNodesReady` gate resolves
  the `GraphProjectionPhaseCanonicalNodesCommitted` row the AWS resource node
  builder publishes under exactly this key on the CloudResource keyspace, so
  the edge never projects before its source node commits. Renaming the key
  silently removes that gate — the intent still enqueues, but the handler can
  never see its source nodes as ready.
- `SourceSystem` is the shared two-tier `projectorintent.SourceSystem`: a
  trimmed `SourceRef.SourceSystem`, falling back to a trimmed
  `CollectorKind`. The pre-extraction root helper
  (`awsCloudRuntimeDriftSourceSystem`) had the identical two-tier body, and
  the child tests pin both tiers.
- Do not decode the payload, inspect `relationship_type`, resolve image URIs,
  or add a target-side readiness gate here. The reducer handler owns the
  join, the retract-first lifecycle, target existence, and the edge write; an
  unscanned target image is a graceful handler-side miss by design.
- Do not move lookup construction, assembly, queue writes, retries, graph
  writes, or telemetry into this package.

## Common changes

- **Changing the reason string or the entity key.** Both are asserted
  verbatim by the package tests and by the root fan-out parity fixture
  (`../scope_generation_intents_fanout_parity_test.go`); change them
  together, and remember the entity key is a cross-domain readiness contract,
  not a label.
- **Changing the trigger kind.** This is a correctness decision, not a
  cleanup: the aws_resource trigger is the #5450 retraction-safety fix and
  the root dispatcher tests in
  `../aws_cloud_image_materialization_intents_test.go` pin it. Update the
  design doc's retraction-safety section in the same change.

## Failure modes

- **Path citations outside this package.** The telemetry contract doc
  (`docs/public/observability/telemetry-coverage.md`) cites this package's
  `materialization_intents.go` by full path in its projector-fact-commit row,
  and `docs/internal/aws-relationship-edge-materialization-design.md` names
  the builder symbol and file path — a rename here breaks
  `scripts/verify-telemetry-coverage.sh` and dangles the design doc. No
  route-serves-data registry entry cites this family (verified with a
  positive control against the registry's other projector citations).
- **The root dispatcher test file name is pinned from the reducer side.**
  `go/internal/reducer/awscloud/aws_cloud_image_materialization_test.go` cites
  `internal/projector/aws_cloud_image_materialization_intents_test.go` and
  its retraction-safety test by name as the enqueue-side half of the #5450
  proof, which is why that root file kept its pre-extraction name when the
  builder moved here. Renaming that root test file or its tests dangles the
  reducer-side citation.
- **A trigger fact with a blank source ref AND blank collector kind** yields
  an empty `SourceSystem` rather than dropping the intent. That is the
  preserved pre-extraction behavior, not a bug to patch in passing.

## Anti-patterns

- Do not add a package-local source-system helper; the two-tier
  `projectorintent.SourceSystem` IS the pre-extraction behavior here (unlike
  the code-taint/interproc families, whose single-tier labels must not use
  it).
- Do not import the root `projector` package. Root imports this package to
  dispatch, and the reverse direction is an import cycle.
- Do not widen the export surface past
  `BuildAWSCloudImageMaterializationReducerIntent`. Every sibling family in
  this series exports exactly one builder and no types.

## Changes needing ADR review

- Adding a decode seam. This family triggers on fact presence alone; families
  that need one keep a local decode call against `sdk/go/factschema` rather
  than importing root's wrapper, and that split is a design decision rather
  than a local call.
- Changing `reducer.DomainAWSCloudImageMaterialization`, the aws_resource
  trigger, the shared `aws_resource_materialization:<scope>` entity key, or
  the two-tier source-system label. All are contract surface the reducer
  handler, the readiness gate, and the fan-out parity fixture assert against;
  the trigger and key are load-bearing halves of the #5450 and node-before-edge
  designs.

## Verification

Use TDD. Run the focused child tests, the root dispatcher tests in
`../aws_cloud_image_materialization_intents_test.go`, the root ordered
fan-out parity and probe-count tests, package-doc verification, the projector
package tree, telemetry coverage, and the golden-corpus gates selected by the
changed paths.
