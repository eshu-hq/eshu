# HTTP Container Image, Ingester, And Bundle Routes

Use these routes for the container image inventory, ingester status, and
indexed bundle candidate search. Split out of
[Repository routes](repositories-ingesters-bundles.md)
to keep each reference page under the repository's 500-line cap.

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
`purl`, and `version_count`, ordered by `ecosystem`, `name`, then `package_id`.
Scoped-token behavior (#5167): a `Package` node has no repository key for a
grant to bind, so a scoped token is admitted and gated on visibility, the same
gate the package-registry ecosystem browse applies.

- A scoped caller sees only packages whose `visibility` is exactly `public`. A
  private package, and a package whose fact carries no `visibility`, stay
  hidden. Correlation-granted private packages are not served on this route.
- A scoped token with an empty grant gets an empty page; the graph is not read.
- The shared key and all-scope callers see every package. An all-scope caller
  is admitted under the `ESHU_GOVERNANCE_MODE` rules in
  [HTTP API](../http-api.md#all-scope-credentials-on-grant-filtered-routes).
- `limit` bounds the page and `truncated` is `true` when more matches exist
  than `limit`. `version_count` is exact for every returned row, `0` included.

The route returns matching bundle candidates from the active query backend. It
does not upload files, mutate graph state, or import `.eshu` archives.
