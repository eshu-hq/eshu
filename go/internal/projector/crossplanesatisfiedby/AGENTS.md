# AGENTS.md — crossplane-satisfied-by projector intent guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` and `../README.md` for projector-wide invariants, including
   the rule that the projector never makes cross-source admission decisions.
3. `../intent/AGENTS.md` for the neutral builder contract.
4. `../scope_generation_intents.go` for root-owned assembly order; this probe
   runs after `projectorkubernetes.BuildCorrelationMaterializationReducerIntent`
   and before `projectorsecurity.BuildSecurityGroupEndpointMaterializationReducerIntent`.
5. `go/internal/reducer/crossplane` for `ExtractCrossplaneSatisfiedByEdgeRows`
   and the `CrossplaneSatisfiedByMaterializationHandler` (registered under the
   reducer root's `DomainCrossplaneSatisfiedByMaterialization` domain): what
   the reducer does with the intent this package enqueues, including the
   cross-scope join against active CrossplaneXRD facts and the SATISFIED_BY
   graph write.

## Invariants

- Import `internal/projector/intent`, never the root projector package. Root
  imports this package to dispatch, so the reverse import cycles.
- `BuildCrossplaneSatisfiedByMaterializationReducerIntent` fires on the
  earliest `content_entity` fact whose `entity_kind` (falling back to
  `entity_type`) is `K8sResource` or `CrossplaneXRD`. A Crossplane Claim is
  never parser-labeled — it is an ordinary `K8sResource` row — so the trigger
  reads the entity type directly rather than firing on any `content_entity`
  presence, which would enqueue a (cheap but unnecessary) intent for every
  repo with parsed code entities.
- `candidateFactKinds` must mirror every kind `triggerFact` inspects.
  `FirstAcrossKinds` uses the list to skip kinds the generation does not
  carry before evaluating the predicate — the list exists for that pruning,
  not to change admission.
- Keep the `Reason` string
  (`"k8s_resource/crossplane_xrd content-entity facts observed"`) and the
  `crossplane_satisfied_by_materialization:<scope>` entity key byte-identical.
  The reducer claims one intent per scope generation and reloads the
  generation's facts itself; this package's own tests pin these values. **The
  root fan-out parity fixture (`../scope_generation_intents_fanout_parity_test.go`
  and `../scope_generation_intents_fanout_test.go`) does NOT cover this
  domain** — `reducer.DomainCrossplaneSatisfiedByMaterialization` appears in
  neither `fanOutParityExpectations` nor `fanOutParityExpectedOrder`, and the
  shared fixture carries no `K8sResource`/`CrossplaneXRD` content-entity fact
  — so this package's own tests are the only thing standing between a
  reason/entity-key edit and a silent contract change.
- `SourceSystem` is the shared two-tier `projectorintent.SourceSystem`
  (trimmed `SourceRef.SourceSystem`, else trimmed `CollectorKind`). The
  pre-extraction root helper (`crossplaneSatisfiedBySourceSystem`) had the
  identical two-tier body, so this is NOT a behavior change — do not
  reintroduce a package-local copy.
- Do not decode a payload field beyond `entity_type`/`entity_kind`, and do
  not check a schema version here; this builder reads only envelope fields
  and two payload keys through a package-local `payloadString` copy
  (`payload.go`, mirroring root's `payload.go` helper of the same name).
- Do not move lookup construction, assembly, queue writes, retries, graph
  writes, or telemetry into this package.

## Common changes

- **Changing the reason string or the entity key.** Both are asserted
  verbatim by this package's own tests only — the root fan-out parity
  fixture does NOT cover this domain (see Invariants above), so there is no
  second safety net.
- **Adding a candidate entity type.** Add it to `triggerFact`'s switch,
  decide whether the reducer's `ExtractCrossplaneSatisfiedByEdgeRows` needs
  the same addition, and update the child tests plus the root dispatcher
  test file (`../crossplane_satisfied_by_materialization_projection_test.go`).

## Failure modes

- **Root dispatcher tests live outside this directory.** The `buildProjection`
  cases for this domain — K8sResource candidate, CrossplaneXRD candidate, and
  the unrelated-entity non-trigger case — stayed at root in
  `../crossplane_satisfied_by_materialization_projection_test.go` because
  they call the unexported root `buildProjection` dispatcher directly, which
  this package cannot import. A change here can break them without touching
  any file in this directory.
- **Route-serves-data registry.** No entry in
  `go/internal/mcp/route_serves_data_registry*.go` cites a projector source
  file for the `crossplane_satisfied_by_materialization` domain (verified by
  `rg` across the registry files at extraction time). If a route is ever
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
  `BuildCrossplaneSatisfiedByMaterializationReducerIntent`. Every sibling
  family in this series exports exactly one builder and no types.

## Changes needing ADR review

- Changing `reducer.DomainCrossplaneSatisfiedByMaterialization`, the
  candidate entity-type set, the entity key, or the two-tier source-system
  label. All are contract surface the reducer handler asserts against, even
  though the root fan-out parity fixture does not.

## Verification

Use TDD. Run the focused child tests, the root dispatcher projection tests,
the root ordered fan-out parity and probe-count tests, the focused
`TestRouteServesDataRegistry` run, package-doc verification, the projector
package tree, and the golden-corpus gates selected by the changed paths.
