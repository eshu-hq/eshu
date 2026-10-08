# Postgres repository selection observation store

## Purpose

This package owns `repository_selection_observations` (#7625): per git
repository scope and per selector, whether the newest complete GitHub org
listing, or an explicit configured repository list, still selects the
repository. The git collector's `membership.Observer` reads and writes it once
per githubOrg or explicit cycle (once per owner for an explicit list), on
shard 0 only. The rows are evidence; nothing in this phase deletes, hides, or retires
a scope because of them.

## Ownership boundary

This package owns the table DDL text (kept byte-identical to migration
`163_repository_selection_observations.sql` by a test; the embedded migration
is the bootstrap source of truth), the org partition read of
`ingestion_scopes`, the selector read, and the batched upsert.
`go/internal/collector/repo/git/membership` owns the evaluation, the
`Store` interface this type implements, and every operator signal.
`cmd/ingester` and `cmd/collector-git` construct the store and hand it to the
native selector, which observes in githubOrg and explicit mode.

## Exported surface

- `ObservationStore` with `NewObservationStore(database db.ExecQueryer)`.
- `KnownScopes`, `Observations`, `UpsertObservations`.

See `doc.go` for the godoc contract.

## Dependencies

- `internal/storage/postgres/db` for the shared `ExecQueryer` contract.
- `internal/collector/repo/git/membership` for the row and batch types.

## Telemetry

None of its own. Store errors return to `membership.Observer`, which logs
`git_repository_selection_store_failed` with `failure_class`
(`known_scopes_read`, `observations_read`, `upsert`) and counts
`eshu_dp_collector_repository_selection_evaluations_total{outcome="store_error"}`.
The collector cycle continues.

## Gotchas / invariants

- The partition read matches `source_system = 'git'`, `scope_kind =
  'repository'`, `collector_kind = 'git'`, and the lowercased slug org. Do not
  add an `ingestion_scopes` index for it; the existing source index already
  serves it.
- The upsert binds `github_repo_id` as `bigint[]` with `0` for unknown and
  maps it back to `NULL` with `NULLIF`, so no nullable array crosses the
  driver. `last_listed_at`, `first_unlisted_at`, and the counter are derived
  in SQL from `state` and the stored row.
- `ORDER BY s.scope_id` is load-bearing: it fixes the lock order across
  replicas of one selector.
- `WHERE o.evaluated_at < EXCLUDED.evaluated_at` makes every write
  advance-only. Removing it makes a replay double-count a miss; the live test
  fails that mutant.
- Two rows for one scope that disagree are rejected before any write, because
  one statement cannot update a row twice (SQLSTATE 21000).

## Evidence

Performance Evidence (#7625), PostgreSQL 18 with 12,000 `ingestion_scopes`
rows (1,000 in the target org, 300 in another org, 10,700 other kinds), the
exact production statements as prepared statements:

| Statement | Result |
| --- | --- |
| `KnownScopes` | 0.74 ms, 45 shared buffers, bitmap scan on `ingestion_scopes_source_idx` |
| `UpsertObservations`, 1,000 rows, first cycle | 10.0 ms, 1,000 inserted (3 ms is the scope_id sort) |
| `UpsertObservations`, 1,000 rows, next cycle | 14.9 ms, 1,000 updated |
| `UpsertObservations`, replay at the same `evaluated_at` | 7.8 ms, 0 written (1,000 removed by the conflict filter) |
| `Observations`, 1,000 rows | 0.20 ms, bitmap scan on `repository_selection_observations_selector_idx` |

The earlier theory shim on the same data measured the partition read at
0.72 ms (58 buffers), and 0.9 ms while another session held `FOR UPDATE` on an
org scope; the evaluation transaction held only `AccessShareLock` on
`ingestion_scopes` and its indexes and no tuple locks. A per-scope lookup by
`scope_id` (the phase-two freshness read) measured 0.019 ms on the primary
key. The work runs once per githubOrg cycle, or once per owner of an explicit
list, on shard 0 only.

### Freshness read

The repository freshness reader (`repositoryFreshnessSelectionQuery` in
`go/internal/storage/postgres/repository_freshness_sql.go`) reads this table
by `scope_id` alone and returns every selector's row for the scope, live or
stale. `selection.Summarize` (`go/internal/scope/selection`) then applies
liveness and two-cycle confirmation in Go, so the collector gauge and the
`not_selected` verdict share one definition. A lookup error fails the whole
freshness read; the API answers 500 instead of a verdict missing evidence.

Observability Evidence (#7625 phase B): no new signal. A selection lookup
failure increments `eshu_dp_repository_freshness_query_errors_total`, and the
lookup's time is inside `eshu_dp_repository_freshness_query_duration_seconds`,
both recorded once per `ReadRepositoryFreshness` call.

Performance Evidence (#7625 phase B, prove-theory-first): PostgreSQL 18 at
127.0.0.1:25432, a scratch schema with 20,000 rows (4,000 scopes x 5
selectors, one selector stale, mixed states), the exact production statement
prepared and executed twice:

```text
Sort (actual time=0.016..0.017 rows=5 loops=1)  Sort Key: selector_id
  Buffers: shared hit=8
  ->  Bitmap Heap Scan on repository_selection_observations (rows=5)
        Recheck Cond: (scope_id = 'git-repository-scope:repository:r_001700')
        Heap Blocks: exact=5
        ->  Bitmap Index Scan on repository_selection_observations_pkey
              Index Cond: (scope_id = ...)  Index Searches: 1  Buffers: shared hit=3
Execution Time: 0.044 ms cold, 0.020 ms warm; a missing scope 0.019 ms (3 buffers)
```

Pushing liveness into SQL (`evaluated_at >= now() - 3 * interval`) only adds a
residual filter over the same 5 heap blocks (0.040 ms, 8 buffers), so keeping
the predicate in Go costs nothing. The rows per scope are bounded by the
selectors that ever observed it. No new index: the primary key prefix serves
the lookup. The rest of the freshness read is unchanged.

Observability Evidence (#7625): the store has no signal of its own by design.
Every failure surfaces through `membership.Observer` as the
`git_repository_selection_store_failed` WARN with a closed `failure_class` and
the `store_error` outcome on
`eshu_dp_collector_repository_selection_evaluations_total`; Postgres latency
for these statements appears on the instrumented database handle the
binaries pass in (`eshu_dp_postgres_query_duration_seconds`).

## Verification

```bash
cd go && go test ./internal/storage/postgres/membership -count=1
ESHU_GENERATION_RETENTION_PROOF_DSN=postgresql://postgres:postgres@localhost:<port>/postgres?sslmode=disable \
ESHU_GENERATION_RETENTION_PROOF_DISPOSABLE=1 \
  go test ./internal/storage/postgres/membership -run ObservationStoreLive -count=1
```
