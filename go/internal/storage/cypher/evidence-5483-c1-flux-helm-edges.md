# Evidence: FluxHelmRelease RECONCILES_FROM edge writer (#5483 C1)

## Change

Adds four new static Cypher templates in `canonical_flux_helm_edges.go`
(FluxHelmRelease -> FluxHelmRepository/FluxGitRepository/FluxOCIRepository/
FluxBucket) plus a second Drain=true retract anchored on FluxHelmRelease, and
appends `r.reconciler_kind = 'Kustomization'` to the three existing
Kustomization-sourced templates in `canonical_flux_edges.go`
(`canonical_flux_edges.go` is content-flagged by the performance-evidence
gate because it contains `MERGE`/`MATCH`/`DELETE` text, even though the
touched lines are an additive literal `SET` clause change, not a new query
shape).

## No-Regression Evidence

Every new template is byte-for-byte the SAME shape as the existing, already
in-production Kustomization-sourced templates: `UNWIND $rows AS row`, one
`MATCH` on the source node by `{uid: row.source_uid}`, one `MATCH` on the
target node by `{uid: row.target_uid}`, one `MERGE` on the relationship, one
`SET` of literal + row-parameterized properties. No new traversal, no new
label scan, no new index requirement, no cartesian product, no unbound
pattern. The only per-template difference from the existing three is the
target label string and two additional literal `SET` clauses
(`r.reconciler_kind = '...'`, `r.via = '...'`) -- literals add zero query cost
(no additional row read, no additional index lookup). The added retract
(`retractFluxHelmReconcilesFromEdgesCypher`) is the identical shape to
`retractFluxReconcilesFromEdgesCypher`, already measured safe and Drain-marked
from its own first commit (the #4476 NornicDB grouped-write DELETE no-op
class).

Each new template only executes when `collectFluxHelmReleaseEntities` and
`resolveFluxReconciliationRows` (Go-side resolution, reusing the identical
T1-T4 resolveFluxSourceCandidate tiers verbatim -- unmodified function) have
already produced at least one resolved row for that specific
(FluxHelmRelease, target label) pair; a repository with no FluxHelmRelease
entities emits zero additional statements
(`TestFluxHelmReconcilesFromNilWithoutFluxHelmRelease`). This is the same
guarded-emission pattern the Kustomization-sourced templates already use, so
a Kustomization-only repo sees no behavior change from this feature at all
(`TestFluxHelmReconcilesFromKustomizationAndHelmReleaseCoexistIndependently`
proves both reconciler kinds resolve correctly in the same materialization
without cross-contaminating retract scope or template selection).

Verification: `go test ./internal/storage/cypher/... -count=1` (full package,
including every T1-T4 resolver case reused for HelmRelease, the chartRef ->
HelmChart honest-non-link guard, the both-chart-and-chartRef guard, and both
retract statements) is green. No query-plan gate entry
(`go/internal/queryplan/testdata/hot-cypher.yaml`) pins any of these writer
statements (only query-handler-side Cypher is pinned there), so there is no
plan-shape regression surface to update.

## No-Observability-Change

No new metric, span, log line, or runtime knob. The writer reuses the
existing canonical-node-writer phase-group execution path
(`OperationCanonicalUpsert`/`OperationCanonicalRetract`, `Drain`) and its
existing instrumentation; the four new statements are visible through the
same structural_edges phase counters and `graph_write_*` instruments every
other Flux/Atlantis/GitLab canonical edge writer already emits.
