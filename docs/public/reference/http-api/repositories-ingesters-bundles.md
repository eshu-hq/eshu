# HTTP Repository, Ingester, And Bundle Routes

Use these routes for repository catalog navigation, repository-scoped context,
ingester status, and indexed bundle candidate search.

## Repository Routes

- `GET /api/v0/catalog`
- `GET /api/v0/repositories`
- `GET /api/v0/repositories/{repo_id}/context`
- `GET /api/v0/repositories/{repo_id}/story`
- `GET /api/v0/repositories/{repo_id}/stats`
- `GET /api/v0/repositories/{repo_id}/coverage`

Repository routes accept a repository selector in `{repo_id}`. The selector may
be the canonical repository ID, repository name, repository slug, or indexed
path. The server resolves it to the canonical repository ID before querying.

`GET /api/v0/repositories` accepts `limit` and `offset` and returns
`truncated=true` when more indexed repositories are available.

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
