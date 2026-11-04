# Go module repositories

## Purpose

`gomodules` reads one rotation page of repositories for the Go reachability
scanner: their canonical id, active-generation commit, stored remote, and whether
their stored content holds a `go.mod`.

## Ownership boundary

This package owns one read. The coordinator root owns the schedule and admission,
and `coordinator/reachability` owns planning. It writes nothing and needs no
migration.

## Exported surface

- `Store`, `NewStore`, and `Store.ListGoModuleRepositories`.

## Dependencies

`collector/scannercomponent` for the value types and `storage/postgres/db` for the
queryer.

## Telemetry

None of its own. The coordinator wraps the queryer so the read reports on
`eshu_dp_postgres_query_duration_seconds{store="go_module_repositories"}`.

## Gotchas / invariants

- Where the data lives: the remote is `ingestion_scopes.payload->>'remote_url'`,
  written by the git collector's `buildScope` in normalized form. The repository id
  is `ingestion_scopes.source_key`, and the commit is
  `scope_generations.source_commit_sha`.
- A repeat pass in a bucket is one primary-key probe on `workflow_runs`.
- Detection reads `content_files`, not the `module_declaration` fact. `pageQuery`
  records the measurement and what the choice can disagree on. Do not swap in the
  fact probe without an index for it: a repository with no such row is scanned to
  its end.
- Measured on Postgres 16 for one 256-candidate page, with 3,000 repositories,
  100,000 other scopes, 1.84M facts, and 820,000 content files: 66 ms cold, 55 ms warm.

## Related docs

- `docs/public/reference/standalone-vulnerability-scanner-boundary.md`
