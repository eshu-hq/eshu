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

The handler emits the `query.container_image_list` span,
`eshu_dp_query_image_list_duration_seconds`, and
`eshu_dp_query_image_list_errors_total`.

## Container Image Tag History Route

`GET /api/v0/images/tag-history` returns bounded per-tag digest mutation
history for an `image_ref`.

- **Scoped-token filtering (`grant_filtered: true`):** Rows are kept only when
  the `resolved_digest` image is linked via `ContainerImage-[:BUILT_FROM]->Repository`
  to a granted repository (`previous_digest` is omitted unless its image is also
  granted, and observations with no `BUILT_FROM` edge are withheld). `mutated`
  stays as observed so a caller knows a prior digest existed without learning
  its identity.
- **Refill and bounded scan:** Grant-filtered pages refill in fixed 200-row
  chunks up to an 800-row per-request scan ceiling. If all scanned rows are
  withheld, the response returns `count: 0` with `truncated: true` rather than
  ending the walk.
- **Encrypted cursors:** `next_cursor` is an authenticated-encryption envelope
  sealed with the deployment DEK (`ESHU_AUTH_SECRET_ENC_KEY` or
  `ESHU_AUTH_SECRET_ENC_KEY_FILE`) and bound to `(image_ref, grants,
  first_observed_at, uid)`. Pass it back verbatim as `cursor` until
  `truncated` is `false`. Edited, foreign-ref, or grant-changed cursors return
  `400` (restart from page one); missing DEK omits `next_cursor` with a
  `truth.reason` and rejects `cursor` requests with `503 capability_degraded`.
- **Offset rules:** Unscoped and all-scope callers may use `offset`; scoped
  callers sending a non-zero `offset` receive `400` and responses omit `offset`.

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

Repository detail (`GET /api/v0/status/ingesters/repository` and its legacy
alias) reads queue, coordinator, scope activity, stage summaries, domain
backlogs, and health from one status snapshot. The ingester list
(`GET /api/v0/status/ingesters` and its legacy alias) reads health, queue, and
coordinator counts from the same kind of snapshot. Both omit Terraform-state
serial and warning evidence, which neither response includes. Full pipeline
and index status still read that evidence and surface its read failures. The
`get_ingester_status` and `list_ingesters` MCP tools use the same routes.

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
  than `limit`. `version_count` counts the package's version nodes, `0` included; it can exceed
  the versions the versions route lists while a generation is mid-write or when a
  version's owning package node was absent at edge-write time.

The route returns matching bundle candidates from the active query backend. It
does not upload files, mutate graph state, or import `.eshu` archives.
