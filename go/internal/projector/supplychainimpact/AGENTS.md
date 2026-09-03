# AGENTS.md — supply-chain-impact projector intent guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` and `../README.md` for projector-wide invariants, including
   the rule that the projector never makes cross-source admission decisions.
3. `../intent/AGENTS.md` for the neutral builder contract.
4. `../scope_generation_intents.go` for root-owned assembly order; this probe
   runs after `secretsiam.BuildSecretsIAMTrustChainReducerIntent` and before
   the `security.BuildSecurityAlertReconciliationReducerIntent` probe.
5. `go/internal/reducer/supply_chain_impact.go` for what the reducer does
   with the intent this package enqueues: the cross-source
   vulnerability-to-package-to-deployment join and the durable finding write.

## Invariants

- Import `internal/projector/intent`, never the root projector package. Root
  imports this package to dispatch, so the reverse import cycles.
- `BuildSupplyChainImpactReducerIntent` fires on the earliest accepted fact
  across all twelve candidate kinds in original generation order, via
  `FirstAcrossKinds` — never "earliest fact of the first-checked kind."
  Reordering `candidateFactKinds` must not change which fact anchors the
  intent for a fixed generation.
- Keep every per-kind `Reason` string and the `supply_chain_impact:<scope>`
  entity key byte-identical. The reducer claims one intent per scope
  generation and reloads the generation's facts itself; this package's own
  tests pin these values, and the root fan-out parity fixture
  (`../scope_generation_intents_fanout_parity_test.go`) ALSO pins the
  package-identity case — unlike several sibling families, that fixture
  genuinely covers this domain, so a change to the package-identity reason
  or entity key breaks a root test too.
- `SourceSystem` is the shared two-tier `projectorintent.SourceSystem`
  (trimmed `SourceRef.SourceSystem`, else trimmed `CollectorKind`). The
  pre-extraction root helper (`supplyChainImpactSourceSystem`) had the
  identical two-tier body. Do NOT reintroduce a package-local copy — do not
  claim the two tiers are pinned unless a test sets them to DIFFERENT
  values; a test that sets both tiers to the same value passes even when the
  tiers are swapped and proves only that a label was produced.
- Do not decode a payload or check a schema version here. This builder reads
  only `FactKind`, `FactID`, `SourceRef`, and `CollectorKind`; the reducer
  handler owns typed decode where one exists and the cross-source join.
- Do not move lookup construction, assembly, queue writes, retries, graph
  writes, or telemetry into this package.

## Common changes

- **Adding a trigger kind.** Add it to `candidateFactKinds`, the `triggerFact`
  switch, and — if it needs a distinct label — the `reason` function. Update
  this package's own tests and the root dispatcher tests in
  `../supply_chain_impact_projection_test.go`.
- **Changing a reason string or the entity key.** Both are asserted verbatim
  by this package's own tests AND by the root fan-out parity fixture's
  package-identity case — check both before changing either.

## Failure modes

- **Route-serves-data registry path citations.** No entry in
  `go/internal/mcp/route_serves_data_registry_routes.go` cites this package;
  the `supply_chain_impact` domain's read surface is
  `go/internal/query/*.go`, not any projector source file.
- **A trigger fact with no `SourceRef.SourceSystem` and no `CollectorKind`**
  yields an empty `SourceSystem` rather than a literal default. That is the
  preserved pre-extraction behavior, not a bug to patch in passing.
- **Root dispatcher tests live outside this directory.** The `buildProjection`
  cases for this domain are at root in
  `../supply_chain_impact_projection_test.go`. A change here can break them
  without touching any file in this directory.

## Anti-patterns

- Do not add a package-local source-system helper that duplicates
  `projectorintent.SourceSystem`; the two-tier fallback IS the
  pre-extraction behavior here.
- Do not import the root `projector` package.
- Do not widen the export surface past
  `BuildSupplyChainImpactReducerIntent`. Every sibling family in this series
  exports exactly one builder and no types.

## Changes needing ADR review

- Adding a decode seam. This family triggers on fact presence alone; a
  family that needs one keeps its own local decode call against
  `sdk/go/factschema` rather than importing root's classified wrapper.
- Changing `reducer.DomainSupplyChainImpact`, the twelve-kind trigger set, or
  the two-tier source-system label. All are contract surface the reducer
  handler consumes, and this package's tests are what assert them.

## Verification

Use TDD. Run the focused child tests, the root dispatcher tests in
`../supply_chain_impact_projection_test.go`, the root ordered fan-out parity
and probe-count tests, package-doc verification, the projector package tree,
and the golden-corpus gates selected by the changed paths.
