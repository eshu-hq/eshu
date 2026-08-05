# AGENTS.md evidence history, part 4 (issue #5764, round-6 review follow-up)

Split out of [AGENTS-evidence-history-2.md](AGENTS-evidence-history-2.md) to
keep that file under the repository's 500-line cap. Read
[AGENTS.md](AGENTS.md) first for the invariants these entries evidence.

### Infrastructure content-path bound inverted the defect it fixed (round-6 P1/P2/P3 review follow-up to #5764)

The P2-3 follow-up documented in
[AGENTS-evidence-history-3.md](AGENTS-evidence-history-3.md) switched
`queryRepoInfrastructureFromContent` (`repository_infrastructure.go`) from
silently truncating to requesting `repositoryInfrastructureEntityLimit+1`
rows and reporting `len(entities) > limit`. That fix was itself wrong: it
called the UNTYPED `ContentReader.ListRepoEntities`, which has no
`entity_type` predicate and returns every parsed content entity for the
repository -- functions, classes, structs, not only infrastructure. So
`truncated` reported whether the repository had more than 5000 content
entities of ANY type (true for nearly every real repository), not whether the
infrastructure panel itself was clipped, while every infrastructure entity
was actually present and undisclosed as complete. This propagated the same
false-claim class #5764 exists to remove -- inverted -- to `truncated`,
`answer_metadata.truncated`, `answer_metadata.limitations`, and
`partial_reasons` on `GET /repositories/{id}/story`,
`GET /repositories/{id}/context`, `GET /workloads/{id}/context`,
`GET /services/{name}/context`, and `GET /services/{name}/story`. Second-order:
the same 5001-row window, sorted `relative_path, start_line` across ALL
types, could silently drop infrastructure entities in late-sorting paths
before entity-type classification ever saw them -- the exact hazard
`content_reader_by_type.go` documents and `ListRepoEntitiesByType`/
`fetchK8sResourceCandidates` exist to avoid on the k8s SELECTS scan.

Fixed by adding `ContentStore.ListRepoEntitiesByTypes` (`ports.go`,
implemented in `content_reader_by_type.go` as `entity_type = ANY($types)` --
one query, not a per-type loop, mirroring `ListRepoEntitiesByPaths`'s
`relative_path = ANY($paths)` precedent) and switching
`queryRepoInfrastructureFromContent` to call it with the new canonical
`repositoryInfrastructureEntityTypes` list, so the LIMIT and its truncation
signal both apply to the TYPE-FILTERED set. `isRepositoryInfrastructureType`
was refactored to check membership in a set built from that same list
(`repositoryInfrastructureTypeSet`), so the content-path filter and the
graph-path defensive gate cannot drift apart again. Proven by
`TestQueryRepoInfrastructureFromContentIgnoresNonInfrastructureEntityCount`
(`repository_infrastructure_test.go`): `repositoryInfrastructureEntityLimit+1`
entities (4998 `Function`, 3 `TerraformResource`) must report
`truncated=false` with all 3 infrastructure rows present -- RED before the
fix (`truncated=true`, reproducing the defect), GREEN after.

A second, independent defect survived the same content-path bug: with the
untyped read, `fetchServiceReadModelWorkloadContext`
(`entity_workload_context.go`) could see `infrastructureTruncated=true` with
zero infrastructure rows (every entity non-infrastructure-typed, so none
converted). When the read-model path's own graph fallback ALSO failed, the
`err != nil` branch appended `infrastructureReadDegradedReason` to
`limitations` but never reset `infrastructureTruncated`, so the stale `true`
survived into `limitations` alongside the degrade reason -- "more rows may
exist" attached to an EMPTY panel, violating the mutual-exclusion invariant
asserted in `repository_infrastructure_degrade.go`,
`repository_context_helpers.go`,
[AGENTS-evidence-history-2.md](AGENTS-evidence-history-2.md)'s P2-2 entry,
and `docs/public/reference/telemetry/graph-read-safety.md`. The
`ListRepoEntitiesByTypes` fix above narrows how often this triggers but does
not close it structurally (a type-filtered read can still return zero rows
with `truncated=true` if a future type gets added to one list and not the
other), so it is fixed independently: the graph-error branch now resets
`infrastructureTruncated = false`, mirroring `queryRepoInfrastructure`'s own
forced-false-on-error branch. Proven by
`TestGetServiceContextReadModelResetsTruncatedOnGraphFallbackError`
(`service_read_model_workload_context_test.go`), using a
`nonFilteringInfrastructureContentStore` test double that returns raw
non-infrastructure rows regardless of the requested type set (to reach the
precondition independent of whether today's real SQL predicate can produce
it) plus a graph reader returning `ErrGraphUnavailable`: `limitations` must
contain `infrastructure_read_degraded` and must NOT contain
`infrastructure_truncated` -- RED before the fix (both reasons present),
GREEN after.

Third, `openapi_workload_context_keys_test.go`'s guard covered only
`fetchWorkloadContextForOperation`'s key set, not
`fetchServiceReadModelWorkloadContext`'s -- the other function that feeds the
same `WorkloadContext` schema on the read-model fallback path. That function
emits `materialization_status` and `query_basis`, neither declared in
`openapi_components_workload_session.go`. Fixed by adding a second literal
key list (`fetchServiceReadModelWorkloadContextResultKeys`) and a second test
(`TestOpenAPIWorkloadContextDeclaresEveryReadModelKey`), and declaring both
properties in the schema.

Fourth, two documentation-accuracy findings from the same review round:
`entity_workload_context.go`'s comment above the primary graph-materialized
path's `infrastructureReadDegradedReason` append claimed
`/services/{name}/story` "copies this limitations slice into its own
limitations field (`service_story_dossier.go`'s whitelist loop)". Both halves
were wrong: `buildServiceIdentity` (`service_story_dossier.go:57-74`) writes
it to `response["service_identity"]["limitations"]`, and the whitelist loop
(`service_story_dossier.go:23-36`) separately mirrors it onto the response's
own top-level `"limitations"` key (its `raw_context_limits` entry, when
present, carries only truncation metadata for that key, not the visible
copy). Verified directly with a throwaway test exercising
`buildServiceStoryResponse`: both `response["limitations"]` and
`response["service_identity"]["limitations"]` are populated, and
`answerMetadataLimitations` (`answer_metadata.go`) reads both
`data["limitations"]` and `mapValue(data, "service_identity")["limitations"]`
before deduping by reason -- either copy alone is sufficient, and the dedup
is why one degrade reason reaches `answer_metadata.partial_reasons` exactly
once. The comment now names this real two-copy chain instead of the
single-mechanism claim. Separately,
[AGENTS-evidence-history-2.md](AGENTS-evidence-history-2.md)'s row-bound
entry above said "on a repository with more than 500 workloads, the graph
read landed on the LIMIT" -- the same #5764 change added
`RETURN DISTINCT w.name`, so the bound is on distinct workload NAMES, not on
workload node count; corrected in place.

No-Regression Evidence: `go test ./internal/query -run
'TestQueryRepoInfrastructure|TestGetServiceContext|TestOpenAPIWorkloadContext'
-v -count=1` and `go test ./internal/query ./internal/queryplan ./internal/mcp
-count=1`.

No-Observability-Change: no span, metric instrument, or metric label was
added or changed; the `repo_infrastructure` stage log already carried
`infrastructureDegradeLogAttrs` (row_count, failure_class, truncated) from
the original #5764 change, unaffected by this fix.
