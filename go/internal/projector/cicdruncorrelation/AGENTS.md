# AGENTS.md — CI/CD run-correlation projector intent guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` and `../README.md` for projector-wide invariants, including
   the rule that the projector never makes cross-source admission decisions.
3. `../intent/AGENTS.md` for the neutral builder contract.
4. `../scope_generation_intents.go` for root-owned assembly order; this probe
   runs after `buildContainerImageIdentityReducerIntent` and before the
   `sbomattestation.BuildSBOMAttestationAttachmentReducerIntent` probe.
5. `go/internal/reducer/cicdrun/ci_cd_run_correlation.go` for what the
   reducer does with the intent this package enqueues: the full-snapshot and
   bounded artifact-only patch correlation, the cross-scope
   `reducer_container_image_identity` read, and the durable decision write.

## Invariants

- Import `internal/projector/intent`, never the root projector package. Root
  imports this package to dispatch, so the reverse import cycles.
- `BuildCICDRunCorrelationReducerIntent` fires on a `ci.run` fact, else a
  `ci.artifact` fact. A `ci.run` always outranks a `ci.artifact` regardless
  of input order — the two kinds are looked up independently via
  `FirstOfKind`, and there is deliberately no cross-kind original-order
  merge. Do not "fix" this to `FirstAcrossKinds`: it changes `FactID`
  provenance for generations carrying both kinds.
- The artifact-only trigger is load-bearing (#5770). Removing it means a
  later artifact whose matching run belongs to an earlier, already-projected
  generation never retriggers correlation, and the artifact is permanently
  lost from the correlation snapshot.
- Keep the `Reason` string and the `ci_cd_run_correlation:<scope>` entity key
  byte-identical. The reducer claims one intent per scope generation and
  reloads the generation's facts itself; this package's own tests pin these
  values. The root fan-out parity fixture carries no `ci.run` or
  `ci.artifact` fact, so it never exercises this domain -- do not rely on it
  as a safety net for a change here.
- `SourceSystem` is the shared two-tier `projectorintent.SourceSystem`
  (trimmed `SourceRef.SourceSystem`, else trimmed `CollectorKind`). The
  pre-extraction root helper (`cicdRunCorrelationSourceSystem`) had the
  identical two-tier body, so this is NOT a behavior change — do not
  reintroduce a package-local copy.
- Do not decode a payload or check a schema version here. This builder reads
  only `FactKind`, `FactID`, `SourceRef`, and `CollectorKind`; the reducer
  handler owns typed decode where one exists and the cross-scope identity
  read.
- Do not move lookup construction, assembly, queue writes, retries, graph
  writes, or telemetry into this package.

## Common changes

- **Changing the reason string or the entity key.** Both are asserted
  verbatim by this package's own tests. The root fan-out parity fixture
  (`../scope_generation_intents_fanout_parity_test.go`) does NOT cover this
  domain -- it has no `ci.run` fact -- so the package tests are the only
  thing standing between a reason/entity-key edit and a silent contract
  change.
- **Adding a trigger kind.** Decide explicitly whether it joins the run tier
  or the artifact tier, keep the run-over-artifact rule, and update the
  child tests plus the root dispatcher tests in
  `../ci_cd_run_correlation_projection_test.go`. Also update
  `cicdRunCorrelationFactKinds` in
  `go/internal/reducer/cicdrun/ci_cd_run_correlation.go` if the reducer needs
  to load the new kind for its own correlation pass — this package's trigger
  set is deliberately narrower than the handler's load set, and the two are
  not required to match.

## Failure modes

- **Route-serves-data registry path citations.** The registry in
  `go/internal/mcp/route_serves_data_registry_routes.go` cites
  `go/internal/query/ci_cd_run_correlations.go` and
  `go/internal/query/incident_context_runtime_sql.go` for the
  `ci_cd_run_correlation` domain, not any projector file — no entry cites
  this package (verified with a positive control against the cloudinventory
  citations). If a route is ever repointed to cite a projector source file
  for this family, that will create the coupling the sibling extractions
  warn about: the registry test reads cited files by path and fails with
  `read ...: no such file` on a rename, two packages away from this one.
- **A trigger fact with no `SourceRef.SourceSystem` and no `CollectorKind`**
  yields an empty `SourceSystem` rather than a literal default. That is the
  preserved pre-extraction behavior, not a bug to patch in passing.
- **Root dispatcher tests live outside this directory.** The
  `buildProjection` cases for this domain — the run-only, artifact-only,
  run-and-artifact-same-generation, and no-CI/CD-facts cases — are at root
  in `../ci_cd_run_correlation_projection_test.go`. A change here can break
  them without touching any file in this directory.

## Anti-patterns

- Do not add a package-local source-system helper that duplicates
  `projectorintent.SourceSystem`; the two-tier fallback IS the
  pre-extraction behavior here (unlike the code-taint/interproc families,
  whose single-tier labels must not use it, or observability-coverage's
  three-tier label, which the shared helper cannot represent).
- Do not import the root `projector` package. Root imports this package to
  dispatch, and the reverse direction is an import cycle.
- Do not widen the export surface past
  `BuildCICDRunCorrelationReducerIntent`. Every sibling family in this
  series exports exactly one builder and no types.

## Changes needing ADR review

- Adding a decode seam. This family triggers on fact presence alone; families
  that need one keep a local decode call against `sdk/go/factschema` rather
  than importing root's wrapper, and that split is a design decision rather
  than a local call.
- Changing `reducer.DomainCICDRunCorrelation`, the run-over-artifact rule, or
  the two-tier source-system label. All three are contract surface the
  reducer handler consumes, and this package's tests are what assert them.

## Verification

Use TDD. Run the focused child tests, the root dispatcher tests in
`../ci_cd_run_correlation_projection_test.go`, the root ordered fan-out
parity and probe-count tests, package-doc verification, the projector
package tree, and the golden-corpus gates selected by the changed paths.
