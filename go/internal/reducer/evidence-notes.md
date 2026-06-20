# reducer Evidence Notes

Keep this file for scoped reducer evidence that is too detailed for the package
orientation README.

## Code-Call Refresh Fence Memory Bound (#3124)

No-Regression Evidence: the baseline public-repository Helm proof for #2995
got past code-call materialization after #3122, emitted 139,352 `code_calls`
shared intents, then OOM-killed the reducer before any `code_calls` intents
completed. The root cause was the selector's refresh-fence fallback loading the
whole `(scope_id, acceptance_unit_id, source_run_id, code_calls)` pending row
set through `ListPendingAcceptanceUnitIntents`. `go test ./internal/reducer
-run TestCodeCallProjectionRunnerUsesBoundedRefreshFenceLookup -count=1`
failed before the runner used a bounded fence lookup, then passed after stores
that implement `CodeCallProjectionRefreshFenceLookup` answer the fence question
without calling the full acceptance-unit loader. The existing compatibility scan
remains for stores that do not implement the optimized lookup, and the existing
ordering tests still prove file rows defer behind covering refresh rows and
earlier whole-scope rows while later whole refresh rows do not block earlier
file partitions.

Performance Evidence: the first bounded Postgres draft removed the reducer heap
spike but still sent whole-scope rows through the file-refresh JSONB branch; in
the Helm proof, late whole-row `selection_refresh_fence_duration_seconds`
samples grew into multi-second checks. The final patch splits the lookup into a
whole-row `EXISTS` query over earlier pending rows and a file-row query for
repo-refresh coverage. The red
`TestSharedIntentStoreCodeCallProjectionRowBlockedByRepoFenceUsesWholeRowLookup`
case proves whole rows no longer include `jsonb_array_elements_text`, while
`TestSharedIntentStoreCodeCallProjectionRowBlockedByRepoFenceDoesNotFenceFileRefreshRows`
keeps the file-refresh ordering semantics aligned with the in-memory fallback.
After rolling the split-query image, steady-state code-call projection cycles
showed refresh-fence checks in tenths of a second while the full-repo
`code_calls` backlog continued draining without SQL errors or OOM restarts. The
terminal Helm proof reached `code_calls` `139861` done and `0` open, with all
other shared projection domains also at `0` open.

Observability Evidence: the change adds no metric name, metric label, span
name, queue domain, worker, lease, runtime knob, graph write route, or Cypher
statement. Operators still diagnose this path through existing code-call
projection cycle logs (`selection_duration_seconds`,
`selection_phases.refresh_fence_check_seconds`, `processed_intents`,
`blocked_readiness`, write and mark-completed durations), shared-intent backlog
queries, partition lease rows, reducer execution counters, and instrumented
Postgres query spans/duration metrics. The Postgres lookup is one scoped
`EXISTS` query over pending `shared_projection_intents`; it does not return
payload rows for the full acceptance unit.

## Service-Catalog Correlation Fanout Guardrails (#3173)

Contract Evidence: service-catalog correlation decisions now carry
`required_anchor_keys` on bounded refusal outcomes (`ambiguous`, `unresolved`,
`stale`, and `rejected`). The required anchors are closed contract names only:
`repository_id`, canonical repository URL fields, or
`git-repository-scope:<repo_id>`. The reducer still refuses name-only catalog
repository claims and ambiguous repository matches; the new field explains the
missing proof without adding raw repository URLs, repository ids, or provider
values to metric labels.

Observability Evidence: the handler summary exposes max candidate fanout,
dropped ambiguous candidates, missing-anchor entities, and required anchor
keys. `eshu_dp_service_catalog_correlations_total` remains a decision counter
with only the closed correlation outcomes. Guardrail counts use
`eshu_dp_service_catalog_correlation_guardrails_total` labeled by bounded
`guardrail` values (`candidate_fanout`, `dropped_ambiguous_candidate`, and
`missing_anchor_entity`). Focused verification:
`go test ./internal/reducer -run 'TestBuildServiceCatalogCorrelation|TestServiceCatalogCorrelation|TestPostgresServiceCatalogCorrelation' -count=1`
and `go test ./internal/reducer -count=1`.
