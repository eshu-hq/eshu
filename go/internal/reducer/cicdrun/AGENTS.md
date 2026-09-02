# Agent instructions: internal/reducer/cicdrun

Scoped rules for this directory. The root `AGENTS.md` still applies.

## What this package is

The `ci_cd_run_correlation` reducer intent handler, its writer port and
Postgres implementation, the typed decode of the seven `ci.*` fact kinds, the
artifact-only cross-generation patch rebuild, the workflow-image and
deployment-event evidence bridges, and the exact workflow-image `BUILT_FROM`
provenance-edge projection (issue #6061). See `README.md` for the full
ownership boundary and exported surface.

## Read first

- Repository-root `AGENTS.md`
- `go/internal/reducer/AGENTS.md`
- `go/internal/reducer/cicdrun/README.md`
- `go/internal/reducer/crossscope/README.md` (the cross-scope readiness floor `Handle` calls)
- `docs/internal/design/package-restructure.md`
- `docs/internal/evidence/5709-cross-scope-readiness-floor.md`

## Invariants

- **No import of the reducer root, ever.** This package is a leaf below
  `internal/reducer`: the root imports it for `DefaultHandlers` wiring and
  the handler construction, never the reverse.
- **The cross-scope readiness signal is sampled BEFORE the cross-scope load,
  never after.** Reordering reopens the exact race #5709 exists to close —
  see the Gotchas section in `README.md` and `crossscope`'s own README for
  the full argument. Do not "fix" a flaky-looking test here by moving the
  sample point.
- **`ContainerImageProvenanceEdgeWriter` is imported from
  `internal/reducer/contract`, never re-declared locally.** It is a genuine
  two-family shared contract (`container_image_identity` also implements
  against it), homed in `contract` rather than duplicated — unlike
  `codetaint`'s `GraphQueryRunner`, which IS locally redeclared there because
  it is owned by root families this package has no relationship with. Do not
  copy that pattern here; if a future root-owned interface only this package
  and one sibling family need, hoist it to `contract` the same way, not a
  local redeclaration.
- **A malformed required field on any of the seven `ci.*` fact kinds
  dead-letters as an `input_invalid` quarantine, never a silent drop.**
  `buildCICDRunCorrelationDecisionsWithQuarantine`
  (`ci_cd_run_correlation_decode.go`) routes every decode failure through
  `factdecode.PartitionDecodeFailures`; do not swallow a decode error to make
  a batch "succeed."
- **`CICDRunKeyFromParts`/`TrimmedCICDPtr` are exported ONLY because the
  reducer root's `container_image_identity_ci_loader.go` and
  `container_image_identity_typed_evidence.go` read them across the seam**
  (via the `cicdRunKeyFromParts`/`trimmedCICDPtr` forwarders in
  `ci_cd_run_correlation_compat.go`). Do not treat them as a public run-key
  API for new callers outside that one cross-family join.
- **`ProjectCICDWorkflowImageBuiltFromEdges` is exported ONLY because the
  reducer root's shared `provenance_edge_submission_metrics_test.go`
  benchmarks/exercises it directly** (it also exercises the unrelated
  `PackageSourceCorrelationHandler` and `ContainerImageIdentityHandler`
  provenance-edge counters and could not move here). Do not treat it as a
  public projection API for new callers.

## Root-side test doubles this package's move required

`go/internal/reducer/container_image_identity_ci_fixtures_test.go` and
`go/internal/reducer/cross_scope_readiness_floor_handler_test.go` (root) each
hold a SEPARATE, hand-kept-in-sync copy of a subset of this package's own
test fixtures (`ciRunFact`, `ciArtifactFact`, `containerImageIdentityFact`,
`stringSliceContains`, `testCICDDigest`,
`stubCICDRunCorrelationFactLoader`, `recordingCICDRunCorrelationWriter`,
`cicdDecisionsByRun`) — Go test files cannot share unexported symbols across
packages, and those root suites still need to build `ci.run`/`ci.artifact`/
identity fixtures or drive `CICDRunCorrelationHandler.Handle` end to end. See
`README.md`'s "Root-side test doubles this package's move required" section
for the full list, including the symmetric case
(`recordingContainerImageProvenanceEdgeWriter`, duplicated in both
directions). If you change a shared fixture's shape here (a builder's
fields, a stub's method set), update the root copy in the same commit —
nothing enforces they stay identical.

The batched-insert fake `Execer` and its call decoder are NOT duplicated:
`internal/reducer/factwrite/factwritetest` is a shared, exported,
non-`_test.go` support package this package's own writer tests import
(`ci_cd_run_correlation_test.go`, `ci_cd_run_correlation_writer_batch_test.go`),
alongside ~30 other reducer-root writer test files that still use the
root's own pre-existing copy (`reducer_fact_batch_insert_test_helpers_test.go`,
untouched). Prefer `factwritetest` over a local fake for any NEW batched-writer
test in this package or any sibling family.

## Common changes

Adding a new field to a decoded `ci.*` fact: extend the relevant struct in
`sdk/go/factschema/cicdrun/v1`, the corresponding `schemadecode.DecodeCICD*`
function, and `ci_cd_run_correlation_decode.go`'s consumption of it together.
Then update the matching fixture builder in
`container_image_identity_ci_fixtures_test.go` or
`cross_scope_readiness_floor_handler_test.go` (root) if a root test exercises
the new field.

Adding a new outcome or changing `CICDRunCorrelationDecision`'s shape: update
`cicdRunCorrelationPayload` (`ci_cd_run_correlation_writer.go`) so the
published fact payload carries it, and check
`supply_chain_impact_evidence_load.go` / `supply_chain_impact_runtime.go`
(reducer root) for a consumer that reads the field by string key off that
payload — those are not type-checked against this package's struct.

## Failure modes to avoid

- Sampling the cross-scope readiness signal after the cross-scope load
  instead of before — see Invariants.
- Re-declaring `ContainerImageProvenanceEdgeWriter` locally instead of
  importing it from `contract` — that would silently diverge from
  `container_image_identity`'s implementation over time.
- Letting a root-side test-double copy (see above) silently diverge from
  this package's own fixture when either side's shape changes.
- Adding a new caller of `CICDRunKeyFromParts`/`TrimmedCICDPtr`/
  `ProjectCICDWorkflowImageBuiltFromEdges` outside the one cross-family seam
  each is exported for.

## Do not change without ADR review

- The evidence-source string `CICDWorkflowImageBuiltFromEvidenceSource`
  (`"reducer/ci-cd-run-correlation/workflow-image"`) — it is retracted and
  re-asserted by exact string match every generation; changing it orphans
  every previously-written `BUILT_FROM` edge.
- The batch-wide (not per-run) cross-scope resolved-count semantics in
  `Handle` — a documented residual gap of the #5709 floor, not a bug; see
  `docs/internal/evidence/5709-cross-scope-readiness-floor.md` before
  narrowing it to a per-run predicate.
