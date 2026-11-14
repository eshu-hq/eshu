# HTTP Declared Manifest Dependency Routes

`GET /api/v0/repository/manifest-dependencies` is a bounded,
repository-scoped inventory of declared manifest dependencies: package name,
declared version or range, manifest file, ecosystem, and the dev-or-runtime
signal when the fact says so. It answers "what does this repository declare?"
directly from the active-generation git content-entity facts, reading the
same rows through the same delta-overlay snapshot the reducer's pair-probe
read uses, so query truth and reducer truth agree row for row. The MCP tool
`list_repository_manifest_dependencies` calls the same handler.

This complements `GET /api/v0/dependencies`, which walks package-native graph
edges without repository ownership, and `GET /api/v0/package-registry/dependencies`,
which is anchored by exact `package_id`/`version_id`. The manifest inventory
anchors on one `repository_id` and reports declared facts, never resolved
versions or cross-repository ownership.

## Parameters

- `repository_id` is the required repository scope anchor. Only a granted
  repository returns rows.
- `limit` bounds the page (1..200, default 50). The read requests `limit+1`
  internally to set `truncated` and a keyset `next_cursor`.
- `after_observed_at` and `after_fact_id` page a prior `next_cursor`; they
  must be sent together.

## Rows and freshness

Each row carries `package_name`, `declared_range`, `manifest_file`,
`ecosystem`, `package_manager`, `section`, `dependency_scope`,
`development_dependency`, and `fact_id`. A repository with an active
generation but no manifest rows returns an empty list with `freshness`
identifying the generation (`fresh`); an unknown scope returns the same empty
shape with `scope_found: false` (`unavailable`). An out-of-grant or
grantless caller receives the same empty page without a store read. A refused
delta base answers 503 rather than serving delta-only rows as the complete
page. Responses are `derived` truth from active-generation facts with
deterministic `(observed_at, fact_id)` ordering and keyset paging.

## Evidence

Observability Evidence: the `query.manifest_dependencies` request span with
repo/count/truncation attributes, the
`eshu_dp_manifest_dependency_list_duration_seconds` and
`eshu_dp_manifest_dependency_list_errors_total` metrics, plus the store-level
`read=repository` manifest read and delta-union metrics on the shared
package-manifest read path. Performance Evidence:
`docs/internal/evidence/10660-manifest-scope-read-plan.md`: the scope-first
probe over the #121 repo-scoped partial index serves a 96,030-row
fleet-shaped corpus in 0.8 ms with no sequential scan and no new index.
