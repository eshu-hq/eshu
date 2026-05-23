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
