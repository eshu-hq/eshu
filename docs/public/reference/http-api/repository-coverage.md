# HTTP Repository Stats And Story Coverage

These routes report repository content coverage from the bounded content-store
read model. They moved here from
[Repository routes](repositories-ingesters-bundles.md) to keep each reference
page under the repository's 500-line cap.

`GET /api/v0/repositories/{repo_id}/story` resolves `{repo_id}` as a repository
selector and includes `coverage_summary` from the same bounded content-store
coverage contract used by repository stats. When content coverage is available,
the story reports `coverage_summary.status=available`,
`coverage_summary.source_backend=content_store`,
`coverage_summary.query_shape=content_store_repository_coverage`,
`coverage_summary.whole_graph_traversal=false`, file/entity counts, language
and entity-type buckets, and an empty `coverage_summary.missing_evidence`
array. If coverage is unavailable, the story keeps
`coverage_summary.status=unavailable` and names the missing evidence, such as
`content_store_coverage` or `content_store_coverage_error`, instead of emitting
the old generic coverage limitation.

No-Regression Evidence: the focused repository story regression verifies that
story, stats, and coverage routes agree for one repository selector with indexed
content-store counts and that story coverage does not call the graph fallback:
`go test ./internal/query -run 'TestGetRepositoryStoryUsesContentCoverageWhenStatsAndCoverageRoutesHaveCounts|TestGetRepositoryStats|TestQueryContentStoreCoverage' -count=1`.

Observability Evidence: repository story now emits the existing
`repository_query.stage_started` and `repository_query.stage_completed` log
events for `operation=repository_story` with `stage=content_coverage`,
including `duration_seconds`, `source_backend`, `query_shape`,
`counts_available`, `entity_types_available`, `whole_graph_traversal`, and
`missing_evidence`. `ContentReader` continues to wrap the coverage query in a
`postgres.query` span with `db.operation=repository_coverage` and
`db.sql.table=content_files,content_entities`.

`GET /api/v0/repositories/{repo_id}/stats` resolves `{repo_id}` as a repository
selector, verifies the canonical repository identity with a direct
`Repository{id}` lookup when a graph backend is present, and then reads
`file_count`, `entity_count`, `languages`, and `entity_types` from the
content-store coverage read model. Selector resolution, identity verification,
and content coverage share a route-local 2s read budget. The handler does not
run a post-resolution whole-graph traversal to compute totals. If content
coverage is unavailable or times out, `file_count` and `entity_count` are
`null`, `languages` and `entity_types` are empty, and
`coverage.missing_evidence` names the missing evidence instead of inventing
zero totals.

Stats responses include `coverage.source_backend`, `coverage.query_shape`,
`coverage.counts_available`, `coverage.entity_types_available`,
`coverage.whole_graph_traversal`, `coverage.partial_results`,
`coverage.truncated`, `coverage.timeout`, `coverage.timeout_budget`, and
`coverage.missing_evidence`. The normal bounded shape is
`coverage.query_shape=content_store_repository_coverage` with
`coverage.source_backend=content_store`, `coverage.partial_results=false`, and
`coverage.truncated=false`. The explicit missing-evidence shape is
`coverage.query_shape=repository_identity_only` with
`coverage.source_backend=unavailable`; coverage timeouts set
`coverage.partial_results=true`, `coverage.truncated=true`,
`coverage.timeout=true`, and
`coverage.missing_evidence=["content_store_coverage_timeout"]`. Selector or
identity lookup timeouts return `504` because no trustworthy repository
identity exists for a partial stats response.

`coverage.last_error` (stats) and `coverage_summary.last_error` (story) carry a
fixed string from a closed set, never backend error text (#7674):
`content store coverage exceeded 2s route timeout`, `content store coverage
read failed` (the coverage read failed; `missing_evidence` names
`content_store_coverage_error`), `content store not configured`, or `content
store coverage unavailable`. The field, its type, and its presence are
unchanged. Before degrading to that partial `200`, both routes answer a reader
fence on the coverage read with the retryable `503` and `Retry-After`, and a
client cancel with `499` and the fixed message `repository stats content
coverage read failed` or `repository story content coverage read failed`.
The fence wins over the stats route timeout: when the 2s budget expires while
the coverage read waits on the reader fence, the route answers `503`, and that
read's `content_coverage` stage log may still show `timeout=true` and
`content_store_coverage_timeout`.

Stats responses also carry the canonical truth envelope plus an additive
`result_limits` drilldown block and an explicit `partial_reasons` slot, matching
the prompt-ready context routes. The truth basis is `content_index` for
content-backed counts and `hybrid` when a graph backend verifies repository
identity; a transport-only count is never promoted to graph truth.
`result_limits` reports the bounded language/entity-type limit, deterministic
ordering, `language_count`, `entity_type_count`, a `truncated` flag, the
`get_repository_coverage` drilldown, and the stats `context_path`.
`partial_reasons` is always present and lists the coverage `missing_evidence`
plus `content_store_coverage_timeout` when the read times out. These fields are
additive: the existing `coverage.partial_results`, `coverage.truncated`, and
`coverage.timeout` fields are preserved.

The empty-selector inventory form of `get_repository_stats` is served by
`GET /api/v0/repositories`, which carries the same envelope shape: an additive
`result_limits` block (bounded page limit/offset, deterministic name-then-id
ordering, `repository_count`, `truncated`, the `get_repository_stats`
drilldown, and the `/api/v0/repositories` context path) and a `partial_reasons`
slot that names `repository_inventory_truncated` when more repositories exist
beyond the returned page, `repository_group_evidence_missing` when one or
more returned repositories lack source-backed group evidence, and
`dependency_marker_evidence_incomplete` when the bounded
`(:Repository)-[:DEPENDS_ON]->(:Repository)` edge read backing each row's
`is_dependency` field (and its dependency-cluster grouping) failed or was
truncated for this request -- `is_dependency` may be under-reported (`false`
where the true value was `true`) on affected rows. That last reason is
independent of `truncated`: a page whose auxiliary dependency-marker evidence
is incomplete does NOT mean more repositories exist beyond it, so it never
sets `truncated` / `result_limits.truncated`, only its own
`partial_reasons` entry. The existing list `truncated` field is preserved.

No-Regression Evidence: the focused query test covers repository-name and
canonical-id selectors, proves the stats route does not issue the old optional
graph aggregation after selector resolution, verifies content-store
file/entity/language/entity-type counts, and checks that missing content
coverage returns explicit missing-evidence metadata rather than zero totals:
`go test ./internal/query -run 'TestGetRepositoryStats|TestContentReaderRepositoryCoverageDerivesEntityTotalsFromOnePass' -count=1`.

Performance Evidence: issue #1462 coverage adds route-deadline regressions for
selector resolution and content coverage plus a large-count response fixture
with 5,000,000 files and 4,200,000 entities, proving the response stays inside
the bounded content-store shape without graph aggregation:
`go test ./internal/query -run 'TestGetRepositoryStats(ReturnsPartialMetadataWhenContentCoverageTimesOut|SelectorResolutionUsesRouteDeadline|ReturnsLargeContentCoverageInsideBoundedShape)' -count=1`.

Observability Evidence: stats calls emit `repository_query.stage_started` and
`repository_query.stage_completed` log events for `operation=repository_stats`
with `stage=repository_lookup` and `stage=content_coverage`, including
`duration_seconds`, `source_backend`, `query_shape`, `counts_available`,
`entity_types_available`, `whole_graph_traversal`, `partial_results`,
`truncated`, and `timeout`. `ContentReader` wraps the coverage query in a
`postgres.query` span with
`db.operation=repository_coverage` and
`db.sql.table=content_files,content_entities`.
