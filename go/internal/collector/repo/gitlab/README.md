# gitlab

Leaf client that lists one GitLab group's projects for the git collector's
`gitlabGroup` source mode ([#7765](../../../docs/internal/design/7765-gitlab-group-discovery.md)).
Package `git` owns discovery, selection, and observation; this package only
talks to the Groups API and classifies the result.

## Layout

- `doc.go` — the godoc contract.
- `client.go` — `Client` (`ResolveGroup`, `ListGroupProjects`),
  `CheckExactRules`, and the closed failure classes.
- `client_test.go`, `client_retry_test.go`, `client_resolve_test.go` —
  the httptest suite: request shape, paging, completeness, retry, status
  classes, group resolve, and rule checks.

## Dependencies

- `collector/sdk` for `Retry-After` parsing and API base validation (it
  imports no Eshu package, so this package stays a leaf).
- `internal/telemetry` for the `eshu_dp_collector_repository_listing_requests_total`
  counter. No other metric, no span.

## Operational context

- Request: `GET {apiBase}/groups/{id}/projects` with
  `include_subgroups=true&with_shared=false&order_by=id&sort=asc&per_page=100`,
  offset paging, `PRIVATE-TOKEN` auth. Required token scopes are `read_api`
  (listing) and `read_repository` (clone over HTTPS).
- Retry is 429/502/503/504 only, up to 3 times per page, waiting
  `Retry-After` (seconds or HTTP-date) else 1s/2s/4s clamped to [1s, 60s].
  Transport and decode faults are terminal.
- Logs: INFO `git_gitlab_group_listing_completed` per finished listing,
  WARN `git_gitlab_group_listing_retry` per waited retry. Failures return
  a classified `*Error` (`failure_class`); the cycle-failure path logs it.
- A project whose `path_with_namespace` falls outside the resolved group
  prefix is dropped and counted in `Listing.OutsideGroup`, so a shared-in
  project never enters even if `with_shared` were ignored.
