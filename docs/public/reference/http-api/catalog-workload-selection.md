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

- `truncated` is true when any catalog collection is partial.
- `workloads_truncated` is true only when the workload collection, and
  therefore the derived service collection, is partial.

Repository-only truncation leaves `workloads_truncated` false. A service
selector should use the narrower field when it is present so it does not warn
that services are missing merely because repository navigation was bounded.
For compatibility with an older API that does not return the narrower field,
clients should fall back to `truncated`.

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
