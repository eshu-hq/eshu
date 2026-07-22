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
- `GET /api/v0/repositories/{repo_id}/freshness`

Repository routes accept a repository selector in `{repo_id}`. The selector may
be the canonical repository ID, repository name, repository slug, or indexed
path. The server resolves it to the canonical repository ID before querying.

Scoped tokens are supported on the single-repository `{repo_id}` reads --
`context`, `story`, `stats`, `tree`, `coverage`, and `freshness` (#5167 Group
A; freshness already worked from #5143). Each resolves the selector through
the same grant-filtering path (`resolveRepositorySelectorExactForAccess`), so
a repository outside the caller's grant renders `404` rather than leaking
existence, matching every other repository route. The bare list route
`GET /api/v0/repositories` is also scoped-token supported (it filters the
returned catalog to the caller's grant). The `by-language` and
`language-inventory` list variants remain outside the scoped-token allowlist
pending #5167 Group B row-filtering.

`GET /api/v0/repositories/{repo_id}/context` relationship rows include compact
correlation provenance when available: `confidence`, `confidence_basis`,
`resolution_source`, `evidence_type`, `evidence_kinds`, and `resolved_id`.
Use `resolved_id` with `GET /api/v0/evidence/relationships/{resolved_id}` when
a client needs the full evidence preview.

`GET /api/v0/repositories` accepts `limit` and `offset` and returns
`truncated=true` when more indexed repositories are available.

Each repository row also carries additive grouping evidence so the Console can
render repository groups without repository-name rules:

- `group_key`: display label for the source-backed group, empty when no group
  evidence exists
- `group_source`: source used for the grouping decision, currently
  `dependency_cluster`, `repository_dependency_flag`, `repo_slug_namespace`,
  `remote_url_owner`, or `missing_evidence`
- `group_truth`: per-row grouping truth such as `derived` or
  `missing_evidence`
- `group_kind`: `cluster`, `source`, `dependency`, or `unknown`
- `group_reason`: bounded explanation for the assignment or missing evidence

Repositories that participate in a `(:Repository)-[:DEPENDS_ON]->(:Repository)`
edge group by **dependency cluster**: a connected-components pass (union-find)
over the undirected dependency graph assigns every repository in a component the
same `group_key` (the lexicographically smallest repository id in the
component), with `group_source=dependency_cluster`, `group_kind=cluster`, and
`group_truth=derived`. All projected edge sources — resolver/cross-repo,
package-consumption (`projection/package-consumption`), and code-imports
(`projection/code-imports`, repo→repo edges resolved from per-file external
import sources correlated to package-registry ownership) — feed the clustering.
The dependency cluster takes
precedence over every other grouping source. Repositories that participate in no
dependency edge fall through to the source-backed derivation: dependency rows
group from the repository dependency flag; source repositories with a remote
slug group from the first slug namespace; source repositories that lack a slug
but have a git remote URL group from the org/owner segment of that remote
(`remote_url_owner`). Rows without any of these carry
`group_source=missing_evidence` and the inventory `partial_reasons` array
includes `repository_group_evidence_missing` instead of forcing a heuristic
group.

No-Regression Evidence: `go test ./internal/query -run 'TestRepositoryListExposesSourceBackedGroupEvidence|TestOpenAPIRepositoryDocumentsGroupEvidenceFields|TestListRepositoriesGroupsByDependencyCluster|TestListRepositoriesScopedDependencyClusterMembership|TestRepositoryDependencyCluster' -count=1`
proves graph-backed repository rows expose grouping evidence, dependency rows
stay distinguishable, missing evidence is explicit, the OpenAPI schema documents
the new fields with correct enum values for `group_source` (including
`dependency_cluster`) and `group_kind` (including `cluster`), and the
end-to-end handler assigns the right cluster keys. `npm run console:test -- --run
apps/console/src/api/repoCatalog.test.ts
apps/console/src/pages/RepositoriesPage.test.tsx` proves the console loader and
Repositories page consume the source-backed fields.

Observability Evidence: the dependency-cluster edge pre-pass
(`loadRepositoryDependencyClusters`) issues one bounded
`MATCH (s:Repository)-[:DEPENDS_ON]->(t:Repository) … LIMIT 50000` query per
`GET /api/v0/repositories` call that reaches the graph backend. It is
instrumented with the existing `startRepositoryQueryStage` / `Done` timer
(operation=`repository_list`, stage=`dependency_cluster_edges`), which emits
`repository_query.stage_started` and `repository_query.stage_completed` log
events carrying `duration_seconds` and `cluster_count`. Operators can observe
the pre-pass latency and result cardinality from structured logs or any log
aggregator hooked to those events. On error the function returns an empty map
and the handler continues with degraded (non-cluster) grouping rather than
failing the request — the degraded path is visible through the per-row
`group_source=missing_evidence` values in the response. No new metric label,
queue work, collector call, Postgres read, or runtime knob is added.

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

Performance Evidence: platform-qa baseline before this read model required 797
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
beyond the returned page, plus `repository_group_evidence_missing` when one or
more returned repositories lack source-backed group evidence. The existing list
`truncated` field is preserved.

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

`GET /api/v0/repositories/{repo_id}/freshness` (#5143) answers two questions
for one repository: did eshu pick up its latest commit, and is the evidence
for that commit fully built. `verdict=current` speaks to build completeness
for the resolved generation, not necessarily a commit receipt. The response
carries `verdict` (one of `current`, `building`, `behind`, `unobserved`, or
`unknown`), `observed_commit` (the active generation's source commit SHA --
legitimately empty for non-git scopes, pre-delta-baseline generations, and
snapshot-trigger git generations (`trigger_kind=snapshot`, for example a
cassette-replayed source with no commit to report, as opposed to a
push/delta-triggered sync) -- represented explicitly rather than fabricated,
even when `verdict=current`), `generation` (id, status, trigger_kind,
is_delta, activated_at), `stages` (collected/reduced/projected/materialized
booleans), `outstanding_by_stage` (per-stage/status counts of OUTSTANDING
work only -- pending, claimed, running, retrying, failed, or dead_letter;
succeeded rows are never listed here), and `shared_enrichment` (cross-repo
materialization backlog referencing this repository's generation, kept as a
separate axis so one repository's freshness never blames another
repository's shared backlog). An optional `expected_commit` query parameter
asks whether a specific commit SHA is reflected; a mismatch always renders
`behind`, whether or not a generation is
actively progressing. `unobserved_push` reports a queued or claimed webhook
refresh trigger for this repository whose target commit does not match the
observed commit. Scoped tokens receive the same shape; a repository outside
the caller's grant 404s like every other repository route.

Performance Evidence: the single-scope composite read (resolve scope,
generation lookup, stage counts, shared-projection pending) is proven at
2.5ms full shape against a 20,000-scope/150,000-work-item/60,000-shared-intent
synthetic corpus, with the shared-projection lookup keyed on `repository_id`
(0.018ms) rather than `generation_id` alone (2.3ms, degrades with the global
pending backlog); see
`go/internal/storage/postgres/README.md#repo-freshness-single-scope-composite-read-5143`
for the full evidence.

Observability Evidence: every read records
`eshu_dp_repository_freshness_query_duration_seconds` and
`eshu_dp_repository_freshness_query_errors_total` (X1 row in
`docs/public/observability/telemetry-coverage.md`), proven by an OTEL
SDK-backed test in `go/internal/storage/postgres/repository_freshness_test.go`.

`GET /api/v0/repositories/{repo_id}/tree` reconstructs the repository directory
layout from the content-store file index (`content_files`). It resolves the
selector, verifies repository identity, and lists one directory level by default.
Use `path` to list a subdirectory, and `recursive=true` to return the full
subtree. Each entry is `{name, type, path}`; file entries add `size` (line
count) and `language` (the detected source type) when indexed, and directory
entries add `child_count` (the number of descendant files in that subtree). Use
`language=<value>` to filter the listing to files of one language/source type
(e.g. `go`, `python`, `hcl`, `yaml`); aliases expand to a family (`typescript`
also matches `tsx`, `terraform` also matches `hcl`/`tfvars`). Filtering a real
path down to zero matches returns an empty `entries` array, not a `404` (path
existence is resolved unfiltered, so a real directory with no files in the
requested language is an empty `200`, not a `404`). The language predicate **and**
the `path` subtree scope are pushed into the content-store read, so the
per-repository file cap applies to the matching, path-scoped set: a language (or a
deep subdirectory) whose files sort beyond the cap in the full repository is still
returned, and `truncated=true` then reflects that the matching set under `path`
itself exceeded the cap. The response `ref` reports the indexed commit SHA the
tree was built from, and `truncated=true` signals that the (matching) file cap
was reached for a very large repository. When `ref` is supplied, it
must match a captured source ref name/head SHA that equals the indexed commit,
or the exact indexed commit SHA. A known but unindexed ref or unavailable branch
metadata returns `409` instead of silently falling back. An indexed repository
with no files returns an empty `entries` array; an unknown repository, unknown
source-backed ref, or subpath returns a `404` envelope. The endpoint never
returns source bytes; use the repository content route for file contents.

`GET /api/v0/repositories/{repo_id}/content?path={file}` returns the indexed
bytes of a single repository file from the content store. `path` is required.
Text files are returned as `encoding=utf-8` with the file in `content`; bytes
that are not valid UTF-8 are returned as `encoding=base64`. `size` is the
original byte length and `truncated=true` signals the response was capped at the
byte limit (cut on a UTF-8 rune boundary for text). `ref` reports the indexed
commit SHA, and `language` is included when the content store recorded it. When
`ref` is supplied, it must match a captured source ref name/head SHA that equals
the indexed commit, or the exact indexed commit SHA. A known but unindexed ref
or unavailable branch metadata returns `409` instead of silently falling back. A
missing path, unknown source-backed ref, or unknown repository returns a `404`
envelope. This endpoint returns the same redacted content the content store
holds; it never reveals secrets the collectors strip during indexing.

`GET /api/v0/repositories/{repo_id}/branches?limit=&cursor=` returns the refs
the console branch selector uses. For Git-backed repositories, ingestion
captures branch names, tag names, ref kind, default-branch marker, head SHA,
observed timestamp, and the content-store indexed timestamp. The response
returns `default_branch` plus `branches[]` rows (kind `branch`) and `tags[]`
rows (kind `tag`), one per source-backed ref. Tags carry the peeled commit SHA
for annotated tags so a release reference resolves to its commit. The
`head_sha` field carries the peeled (^{}) object SHA for annotated tags — a
commit for normal release tags, the dereferenced blob or tree object for
annotated tags of non-commit objects. Older repositories without captured ref
metadata keep the legacy single indexed commit fallback: one `branches[]`
entry carrying `head_sha` and `last_indexed_at`, with an empty `name` and
`default_branch`, an empty `tags[]` array, and `truncated: false` (this path
never paginates), rather than fabricating branch or tag names. A repository
with no indexed commit returns empty `branches` and `tags` arrays; an unknown
repository returns a `404` envelope.

The endpoint is bounded by one `limit` + `cursor` pair over the combined
branches+tags stream, ordered default ref first, then by ref kind, then by
name (the same order the store persists). With no `limit`/`cursor` supplied,
the response still defaults to `limit=100` — the endpoint always bounds its
response rather than returning the full unbounded ref list. `limit` accepts
`[1, 500]`; an out-of-range or non-integer value returns `400`. The response
always carries top-level `truncated: bool`; when `true`, `next_cursor` is also
present and the caller pages forward by passing it back as `cursor` on the
next request. Each page's window (the visible `limit` refs after the cursor)
is split by kind into `branches[]` and `tags[]`, so a single page can contain
both when the window crosses the branch/tag boundary. `cursor` is an opaque
forward-only keyset token encoding the last-emitted ref's full sort key
(default-rank, kind, name) — not a row offset — so it tolerates concurrent ref
churn (a ref added or removed between pages) without skipping or duplicating
entries; a malformed, wrong-version, wrong-kind, or cross-repository cursor
returns `400 invalid cursor` rather than a silently empty or reset page.

`tags_truncated` is deprecated in favor of `truncated`/`next_cursor` but keeps
its original meaning: `true` when more tags exist beyond what `tags[]` carries
in the current page, computed from the exact in-memory page remainder. The
per-tag result cap this field used to signal (500 tags) is retired; the general
`limit`/`cursor` pair now bounds both `branches[]` and `tags[]` together.

In filesystem source mode (local repos without a git remote), the collector now
discovers local git refs via `git for-each-ref` and emits them into the content
store's `repository_refs` table, enabling the same branches+tags response as
remote-collected repos. Repositories that are not git repos still fall through
to the legacy indexed-commit fallback.

No-Regression Evidence: repository ref persistence writes at most one stale-row
delete plus one bounded upsert per repository generation, keyed by
`(repo_id, ref_kind, name)` and sized by Git branch and tag count rather than
file or entity count. Focused tests cover source-backed branch and tag responses,
selected-ref acceptance/rejection, repository ref schema bootstrap, writer upsert
counts, projector materialization, and collector ref parsing/fact payloads.

No-Observability-Change: the existing projector `content_write` stage remains
the operator-facing timing span/metric path, and the writer now logs
`prepare_repository_refs` and `upsert_repository_refs` stage rows plus
`content_repository_ref_count` so ref writes are visible without adding a new
runtime surface.

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

## Container Image Inventory Route

`GET /api/v0/images` lists container images observed by the OCI registry
collector over the authoritative `(:ContainerImage)` graph. It backs the console
Images browse surface.

- `limit` bounds the page (1..200, default 50); the server reads `limit+1` rows
  to detect more pages.
- `offset` continues a previous page; use `next_cursor.offset` from a truncated
  response rather than computing it by hand.
- `digest`, `repository_id`, and `tag` are optional exact-match filters.
  `repository_id` is the OCI repository identity such as
  `oci-registry://host/path`.

Rows are ordered deterministically by `digest` then `uid` and carry `id`,
`digest`, `repository_id`, derived `registry` and `repository`, `name`, `tag`,
`media_type`, `artifact_type`, `config_digest`, `size_bytes`, and
`source_system`. Fields the graph does not hold are omitted rather than invented.
The response envelope reports `count`, `limit`, `offset`, `truncated`, and
`next_cursor` when truncated.

This route surfaces image node properties only. `ContainerImage` nodes carry no
workload edges in the current graph (`DEPLOYS_FROM` is a repository-to-repository
relationship), so the route does not return deploying-workload links. For
source-to-image provenance, use the container image source bridge routes under
`/api/v0/supply-chain/container-images/identities`.

Performance Evidence: the handler's exact Cypher shape (a bounded
`(:ContainerImage)` label scan with `limit+1`, `SKIP $offset`, and deterministic
`ORDER BY img.digest, img.uid`) was measured against the warm local Compose
NornicDB backend (`nornic` database, `~/example-repos` corpus, 10 `ContainerImage`
nodes) over the Bolt-HTTP tx endpoint: warm priming 3.2 ms, then 0.82 ms,
0.71 ms, 1.02 ms across three runs returning the full 10-row inventory. See
`go/internal/query/evidence-notes.md` for the full evidence record.

Observability Evidence: the handler emits the `query.container_image_list` span
with `http.route` and `eshu.capability=platform_impact.container_image_list`,
the `eshu_dp_query_image_list_duration_seconds` histogram with a low-cardinality
`outcome` label, and the `eshu_dp_query_image_list_errors_total` counter with a
bounded `reason` label.

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
searches the pre-indexed package registry catalog (the `:Package` registry
identities materialized by the reducer) as shareable bundle candidates for
callers that need dependency or library handles.

Request contract:

- required JSON body that supplies a search scope: a `query`
  (matched case-insensitively against package normalized name, namespace, or
  PURL) or an `ecosystem` (e.g. npm, pypi, maven, nuget). The scope value must
  contain a non-whitespace character; an empty, whitespace-only, or absent scope
  returns `400` and the route never scans the whole catalog. The OpenAPI schema
  enforces this with `minLength`/`pattern`, so generated clients reject blank
  scope the same way the server does.
- optional `unique_only` to return distinct package bundles
- optional `limit` (default 50, max 200)

Each result reports `package_id`, `name`, `ecosystem`, `registry`, `namespace`,
`purl`, and `version_count`. The route returns matching bundle candidates from
the active query backend. It does not upload files, mutate graph state, or
import `.eshu` archives.
