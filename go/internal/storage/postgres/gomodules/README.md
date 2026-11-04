# Go module repositories

## Purpose

`gomodules` reads one rotation page of repositories for the Go reachability
scanner: their canonical id, active-generation commit, and stored remote. It keeps
a repository whose stored content holds a `go.mod`, or whose go-reachability scope
still holds Go evidence.

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
- A repository is planned on `has_go_module OR has_go_evidence`. Without the second
  branch, a repository that lost its `go.mod` is never claimed, so its Go evidence
  never retracts. `has_go_evidence` is true while the repository's
  `go-reachability://` scope has an active generation holding a non-tombstoned
  `vulnerability.go_module_evidence` or `vulnerability.go_call_reachability` fact. The
  component scans once more and lands a trusted `no_go_modules` generation holding
  only a warning, so the repository drops out on its next visit. The cost is one
  extra shallow clone per lost `go.mod`.
- The probe is designed as the `ingestion_scopes` primary key plus a prefix of
  `fact_records_scope_generation_idx (scope_id, generation_id, fact_kind)`.
  `EXPLAIN (ANALYZE, BUFFERS)` of the amended query has not been run: replace this
  line with its figures before merge. The ruling requires them.
- A repository with Go evidence and an unusable remote is returned with
  `HasGoEvidence` set, and the planner skips it, so that evidence stays active. See
  eshu-hq/eshu#10055.
- A repository scope that retires leaves its go-reachability scope active, because
  candidates are active repository scopes only. Core lifecycle owns that, not this
  query.
- Detection reads `content_files`, not the `module_declaration` fact. For one
  256-candidate page over 1.84M facts, the fact probe took 1,511 ms and read 197k
  buffers; this probe took 68 ms and read 7.5k. Do not swap in the fact probe
  without an index for it: a repository with no such row is scanned to its end.
- That choice can disagree with the fact in two cases, and
  `TestListGoModuleRepositoriesDetectsFromContentNotFactsLive` pins the source:
  1. A `go.mod` with no `module` line is planned, though the parser emits no fact.
     The component reads the commit and finds no module.
  2. Content ahead of or behind the active generation. A stale row causes one empty
     scan; a `go.mod` whose content has not landed waits one rotation.
- Measured on Postgres 16 for one 256-candidate page, with 3,000 repositories,
  100,000 other scopes, 1.84M facts, and 820,000 content files: 66 ms cold, 55 ms warm.

## Related docs

- `docs/public/reference/standalone-vulnerability-scanner-boundary.md`
