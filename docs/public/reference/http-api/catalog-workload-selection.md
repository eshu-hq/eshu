# Catalog And Workload Selection

Use these routes to populate bounded service selectors and turn an operator's
input into a canonical workload handle:

- `GET /api/v0/catalog`
- `POST /api/v0/entities/resolve`
- `GET /api/v0/services/{service_name}/context`

OpenAPI remains canonical for the complete request and response schemas.

## Catalog truncation

`GET /api/v0/catalog` returns bounded `repositories`, `workloads`, and
`services` collections. `limit` applies independently to each collection.

The response has two truncation signals:

- `truncated` is true when any catalog collection (repositories, or
  workloads/services) is itself a bounded partial page -- i.e. more rows
  exist beyond `limit`.
- `workloads_truncated` is true only when the workload collection, and
  therefore the derived service collection, is partial.

Repository-only truncation leaves `workloads_truncated` false. A service
selector should use the narrower field when it is present so it does not warn
that services are missing merely because repository navigation was bounded.
For compatibility with an older API that does not return the narrower field,
clients should fall back to `truncated`.

`truncated` is strictly about row-count paging. It is never set by a
degraded *auxiliary* read on an otherwise-complete page: each repository
row's `is_dependency` field is backed by a separate, bounded
`DEPENDS_ON`-edge marker read, and if that read fails or is truncated the
repository/workload rows returned are still the complete set -- only
`is_dependency` on them may be incomplete (under-reported as `false`). That
case is disclosed through `limitations: ["dependency_marker_evidence_incomplete"]`
instead, so a caller does not mistake it for "more repositories exist" and
request a page that is not actually there.

The catalog is unscoped, so its marker read first runs a whole-graph
`DEPENDS_ON` count and skips the edge read when the graph has none. Otherwise
it reads `(:Repository)-[:DEPENDS_ON]->(:Repository)` grouped by source
repository (`RETURN s.id, collect(t.id)`), bounded at 50,001 source groups
and 50,000 flattened edges; exceeding either bound is the truncation case
above. Operators see this read as a pair of structured log events,
`repository_query.stage_started` and `repository_query.stage_completed`, with
`operation=catalog_list` and `stage=dependency_cluster_edges`. The completion
event carries `duration_seconds`, `edge_count`, `truncated`, `error`, and
`edge_scan_skipped` (true when the count proved there were no edges). It has
no `cluster_count`, because the catalog builds no dependency clusters. A read
failure or truncation also logs a `repository_query.dependency_edges_degraded`
warning with the same `operation`.

## Workload resolution

`POST /api/v0/entities/resolve` accepts `name`, optional `type`, optional
`repo_id`, and optional `limit`. Exposure Path submits free text with
`type=workload` before calling service context.

Workload resolution is graph-authoritative. It matches the exact workload name
and authorizes repository ownership before applying the requested limit. The
bounded lookup covers both canonical ownership forms:

- the workload's `repo_id` property;
- `Repository-[:DEFINES]->Workload` evidence for legacy or shared workloads.

A workload can have more than one `DEFINES` edge, so the relationship lookup
groups by workload identity before applying its limit. Repository names are
then hydrated through a separate bounded repository-ID lookup. Workload
resolution does not fall back to similarly named content entities.

The response returns `entities`, `count`, normalized `limit`, and `truncated`.
Callers must not auto-select one visible row when `truncated` is true, because
another matching workload may exist beyond the bounded page.

## Service context by name

`GET /api/v0/services/{service_name}/context` tries an exact workload name,
then an exact workload id, then the repository read model. Workload names are
not unique, so the name lookup reads a bounded candidate set (51 rows). A
workload is admitted when its own `repo_id` is granted or a granted repository
`DEFINES` it. For a scoped caller that grant filters the candidate read itself,
so the bound counts only workloads the caller may see, and Go re-checks every
returned row. When several admitted
workloads share the name, the lowest workload id is returned, so repeated
calls answer the same way. When more workloads match the name than the lookup
reads, the route returns HTTP 409 with fixed text asking the caller to retry
with a workload id; the body never reports how many workloads matched. For a
scoped caller that happens only when more than 50 granted workloads share the
name, so workloads outside the grant can neither cause the 409 nor be
inferred from it.

## Console selection behavior

The Console catalog is an authorized suggestion set, not the source of truth
for arbitrary free text. Exact catalog display names and other free text go
through the workload resolver. An exact stripped canonical alias from an
authorized catalog option, such as `payments-api` for
`workload:payments-api`, maps directly to that option's canonical ID. Pasted
canonical `workload:...` handles also remain canonical.

If resolution returns no row, multiple rows, or a truncated page, the Console
shows a precise empty or ambiguous state instead of choosing the first visible
service. Browser history that removes the active `service` query parameter
also clears the prior ingress result so stale posture is not left on screen.
