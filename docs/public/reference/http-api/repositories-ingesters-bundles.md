# HTTP Repository, Ingester, And Bundle Routes

Use these routes for repository catalog navigation, repository-scoped context,
ingester status, and indexed bundle candidate search.

## Repository Routes

- `GET /api/v0/catalog`
- `GET /api/v0/repositories`
- `GET /api/v0/repositories/by-language`
- `GET /api/v0/repositories/language-inventory`
- `GET /api/v0/repositories/{repo_id}/context`
- `GET /api/v0/repositories/{repo_id}/story`
- `GET /api/v0/repositories/{repo_id}/stats`
- `GET /api/v0/repositories/{repo_id}/tree`
- `GET /api/v0/repositories/{repo_id}/content`
- `GET /api/v0/repositories/{repo_id}/branches`
- `GET /api/v0/repositories/{repo_id}/coverage`

Repository routes accept a repository selector in `{repo_id}`. The selector may
be the canonical repository ID, repository name, repository slug, or indexed
path. The server resolves it to the canonical repository ID before querying.

`GET /api/v0/repositories` accepts `limit` and `offset` and returns
`truncated=true` when more indexed repositories are available.

`GET /api/v0/repositories/by-language?language=typescript&limit=100&offset=0`
returns `repository_count`, `file_count`, normalized language aliases, and a
bounded repository page from the Postgres content index. Use `limit=0` when a
caller only needs the count and does not need repository rows. Language aliases
currently include:

- `typescript` / `ts` -> `typescript`, `tsx`
- `javascript` / `js` -> `javascript`, `jsx`
- `terraform` -> `terraform`, `hcl`, `tfvars`

`GET /api/v0/repositories/language-inventory?limit=100&offset=0` returns
aggregate repository and file counts for indexed language buckets. This is the
fast "what languages exist?" surface for MCP and API callers; it avoids fetching
every repository and then calling repository coverage one by one.

Performance Evidence: ops-qa baseline before this read model required 797
`get_repository_coverage` fan-out calls for a full language count. A direct
aggregate over 99,552 `content_files` rows took 94.472 ms before the
`content_files(language, repo_id)` index, so the new API path keeps language
inventory server-side and indexed instead of pushing the loop into MCP clients.

Observability Evidence: `ContentReader` wraps the new count, list, and
inventory queries in existing `postgres.query` spans with `db.operation` set to
`count_repositories_by_language`, `list_repositories_by_language`, or
`repository_language_inventory`; responses include `limit`, `offset`, and
`truncated` so slow or incomplete calls are diagnosable from traces and payloads.

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

No-Regression Evidence: the focused query test covers repository-name and
canonical-id selectors, proves the stats route does not issue the old optional
graph aggregation after selector resolution, verifies content-store
file/entity/language/entity-type counts, and checks that missing content
coverage returns explicit missing-evidence metadata rather than zero totals:
`go test ./internal/query -run 'TestGetRepositoryStats|TestContentReaderRepositoryCoverageIncludesEntityTypeCounts' -count=1`.

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

`GET /api/v0/repositories/{repo_id}/tree` reconstructs the repository directory
layout from the content-store file index (`content_files`). It resolves the
selector, verifies repository identity, and lists one directory level by default.
Use `path` to list a subdirectory, and `recursive=true` to return the full
subtree. Each entry is `{name, type, path}`; file entries add `size` (line
count) and `language` when indexed, and directory entries add `child_count` (the
number of descendant files in that subtree). The response `ref` reports the
single indexed commit SHA the tree was built from, and `truncated=true` signals
that the file cap was reached for a very large repository. An indexed repository
with no files returns an empty `entries` array; an unknown repository or
subpath returns a `404` envelope. The endpoint never returns source bytes; use
the repository content route for file contents.

`GET /api/v0/repositories/{repo_id}/content?path={file}` returns the indexed
bytes of a single repository file from the content store. `path` is required.
Text files are returned as `encoding=utf-8` with the file in `content`; bytes
that are not valid UTF-8 are returned as `encoding=base64`. `size` is the
original byte length and `truncated=true` signals the response was capped at the
byte limit (cut on a UTF-8 rune boundary for text). `ref` reports the single
indexed commit SHA, and `language` is included when the content store recorded
it. A missing path or unknown repository returns a `404` envelope. This endpoint
returns the same redacted content the content store holds; it never reveals
secrets the collectors strip during indexing.

`GET /api/v0/repositories/{repo_id}/branches` returns the refs the console
branch selector uses. Git branch names are not captured by ingestion yet, so
this reports the single indexed commit ref per repository — one `branches[]`
entry carrying `head_sha` (the indexed `commit_sha`) and `last_indexed_at`,
with an empty `name` and `default_branch` — truth-labeled `derived` rather than
fabricating a multi-branch list. A repository with no indexed commit returns an
empty `branches` array; an unknown repository returns a `404` envelope. When git
ref ingestion lands, this endpoint will return the full `default_branch` and
per-branch head list.

Repository responses should be treated as:

- canonical identity: `id`
- remote identity: `repo_slug`, `remote_url`
- server-local checkout metadata: `local_path`

If a downstream workflow needs local file operations on a user machine, use
`repo_access` or ask the user for a local checkout path instead of assuming the
server path exists locally.

For indexing workflows, use the CLI or deployment runtime:

- local: `eshu index <path>`
- Kubernetes: repository ingestion is deployment-managed through the ingester
  runtime

## Ingester Status Routes

Canonical routes:

- `GET /api/v0/status/ingesters`
- `GET /api/v0/status/ingesters/{ingester}`

Legacy GET aliases:

- `GET /api/v0/ingesters`
- `GET /api/v0/ingesters/{ingester}`

The default ingester is `repository`. Status responses include identity,
current status, active run ID, last attempt and success, next retry timing,
repository progress counts, failure counts, and last error details.

The shipped public API does not include a per-ingester scan POST route. Use
`POST /api/v0/admin/reindex` or deployment-managed ingestion.

## Bundle Search

`POST /api/v0/code/bundles`

Bundle import is not a shipped public HTTP API. The shipped bundle route
searches indexed repositories as pre-indexed bundle candidates for callers that
need dependency or library handles.

Request contract:

- JSON body with `query`
- optional `limit`

The route returns matching bundle candidates from the active query backend. It
does not upload files, mutate graph state, or import `.eshu` archives.
