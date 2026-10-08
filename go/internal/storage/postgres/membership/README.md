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

This package owns the table DDL text (kept byte-identical by a test to the
DDL that follows the leading comment block of migration
`164_repository_selection_observations.sql`; the embedded migration
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
  driver. `last_listed_at`, `state_since`, and `state_cycle_count` are
  derived in SQL from `state` and the stored row.
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

The `github_org` host filter (`AND ($2 = '' OR lower(split_part(payload->>'remote_url', '/', 3)) = $2)`)
was measured before and after on PostgreSQL 18.6 with 20,000
`ingestion_scopes` rows, 12,000 of them git repository scopes (owner `acme`:
900 github.com, 100 gitlab.com, 50 ghe.corp; the rest across 200 owners and
three hosts), migration 001 and 091 indexes, prepared statements, warm runs.
With git repository scopes the majority, the planner picks a sequential scan
for both forms:

| Statement | Rows | Plan | Buffers | Execution |
| --- | --- | --- | --- | --- |
| Slug-only (before) | 1,050 | seq scan | 594 | 4.8-5.1 ms |
| Host `github.com` (after) | 900 | seq scan, same filter plus host | 594 | 5.0-5.4 ms |
| Host `''` (explicit selectors) | 1,050 | identical to slug-only | 594 | 4.8-4.9 ms |

These are custom plans; under a cached generic plan the empty-host predicate
stays as a per-row filter that is always true, with the same plan shape. The
plan shape and buffers do not change; the read runs once per org cycle on
shard 0. No new index.

Re-measured after amendment 1 changed the row shape (`state_since`,
`state_cycle_count`, `liveness_window_seconds`): PostgreSQL 18.6, migration
164's DDL, 36,000 background rows (12,000 scopes x 3 selectors), the exact
production upsert as a prepared statement for a new selector's 1,000-row
batch (scripts and raw output kept with the PR evidence):

| Statement | Result |
| --- | --- |
| `UpsertObservations`, first cycle | 17.1 ms, 1,000 inserted |
| `UpsertObservations`, next cycle, 100 rows change state | 18.4 ms, 1,000 updated; afterwards 900 rows `selected` with count 2 and 100 `not_listed` with count 1 |
| `UpsertObservations`, replay at the same `evaluated_at` | 8.5 ms, 0 written (1,000 removed by the conflict filter) |

The earlier theory shim on the same data measured the partition read at
0.72 ms (58 buffers), and 0.9 ms while another session held `FOR UPDATE` on an
org scope; the evaluation transaction held only `AccessShareLock` on
`ingestion_scopes` and its indexes and no tuple locks. A per-scope lookup by
`scope_id` (the phase-two freshness read) measured 0.019 ms on the primary
key. The work runs once per githubOrg cycle, or once per owner of an explicit
list, on shard 0 only.

### Freshness read

The repository freshness reader calls `ReadLiveScopeObservations`
(`live_read.go`): one primary-key lookup by `scope_id` that keeps only rows
with `evaluated_at + liveness_window_seconds >= now`, the SQL form of
`selection.Live`. `TestLiveFilterParityLive` pins the two at the window
boundary. `selection.Summarize` (`go/internal/scope/selection`) then applies
confirmation and the aggregate rules in Go, so the collector gauge and the
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

After amendment 1 the shipped live read (36,000 rows, 3 selectors per scope,
one expired) is an index scan on `repository_selection_observations_pkey`
with the window as a residual filter: 2 rows returned, 1 removed by the
filter, 4 shared buffers, 0.028-0.030 ms; a missing scope 0.021 ms (3
buffers). The rows per scope are bounded by the selectors that ever observed
it. No new index: the primary key prefix serves the lookup.

### Latest generation read

When rules (a)-(c) hold, the freshness reader issues
`repositoryFreshnessLatestGenerationQuery`
(`SELECT MAX(observed_at) FROM scope_generations WHERE scope_id = $1`) for
rule (d). PostgreSQL 18.6, 12,000 scopes and 739,838 generations shaped like
QA (per-scope p50 27, p99 79, max 3,280, plus a 5,000 tail), heap rows
interleaved across scopes the way time-ordered ingestion stores them, the
migration 002 indexes:

| Scope | Plan | Buffers | Execution |
| --- | --- | --- | --- |
| 27 generations | bitmap scan on `scope_generations_scope_generation_idx` | 30 | 0.10 ms |
| 3,280 generations | same | 3,310 | 2.68 ms |
| 5,000 generations | same | 3,433 | 3.71 ms first run, 2.77 ms warm |

No new index. The read serves one repository per request and only for a scope
that already has confirmed exclusion evidence, so a selected or pending scope
never pays for it.

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
