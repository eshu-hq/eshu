# Evidence: Intent/Domain qualification is spelling-only, no hot-path cost

Records the performance and observability evidence for the alias-retirement
hotfix (bare `Intent`/`Domain` → `reducercontract`-qualified at 6 sites in
4 files, plus 4 imports), so the hot-path evidence gate
(`scripts/verify-performance-evidence.sh`) has a tracked, in-repo record
specific to this change rather than relying on PR prose.

## Change shape

Identifier spelling only. `type Intent = reducercontract.Intent` was an
alias: qualifying the name resolves to the identical type, identical method
set, identical compiled code. No statement, branch, allocation, query, batch
size, worker knob, lease, retry, or queue path changed. The two test files
change only type references in signatures and composite literals.

## Performance Evidence:

- **Baseline**: the base does not compile (`undefined: Intent` at
  `cloud_relationship_resolution.go:19`,
  `documentation_workload_resolution.go:20`, plus 4 test sites) — there is no
  runnable baseline to benchmark against, which is the breakage being fixed.
- **After** (same host as the slice-B/C measurements, hermetic Go test, no
  backend): the 5 handler tests covering both touched writers —
  `TestResolveWorkloadCloudRelationshipRowsLadder`,
  `TestResolveWorkloadCloudRelationshipRowsScopeAndRepoRestriction`,
  `TestResolveWorkloadCloudRelationshipRowsNilResolverKeepsLegacy`,
  `TestResolveDocumentationWorkloadTargetsLadder`,
  `TestResolveDocumentationWorkloadTargetsNilResolverKeepsLegacy` — all PASS
  in 0.00s each; full `go/internal/reducer` suite ok in 5.379s.
- A spelling-only change cannot regress a budget: same symbols, same codegen.

## No-Regression Evidence:

No existing hot path changed shape. Backend/version: no Cypher text altered,
no batch or worker knob touched. Input shape: unchanged. The retirement
guards (`TestBucket6IntentRetired`, `TestBucket7DomainRetired`) stay green,
so the #6626 retirement is preserved, not reverted.

## No-Observability-Change:

No metric, span, log key, or status surface added or altered. Nothing for an
operator to watch beyond existing reducer telemetry.
