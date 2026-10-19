# Postgres repository selection observation store

## Purpose

This package owns `repository_selection_observations` (#7625): per git
repository scope and per selector, whether the newest complete GitHub org or
GitLab group listing, or an explicit configured repository list, still selects
the repository. The git collector's `membership.Observer` reads and writes it
once per githubOrg or gitlabGroup cycle (once per owner for an explicit list),
on shard 0 only. The rows are evidence; nothing deletes, hides, or retires a
scope because of them. The only rows this package deletes are observation
rows that stayed expired for a grace (`membership.ExpiredObservationGrace`,
7 days) past their own liveness window (#7774). A `not_listed` row, which
the mass-miss guard reads, is deleted only when its selector is abandoned:
every cycle advances the selector's `repository_selectors.last_cycle_at`
marker (migration 200, #7840), and the sweep reclaims the row only when
that marker is missing or older than the row's window plus grace.

## Ownership boundary

This package owns the table DDL text (kept byte-identical by a test to the
DDL that follows the leading comment block of migration
`166_repository_selection_observations.sql`; the embedded migration
is the bootstrap source of truth), the org partition read of
`ingestion_scopes`, the selector read, and the batched upsert.
`go/internal/collector/repo/git/membership` owns the evaluation, the
`Store` interface this type implements, and every operator signal.
`cmd/ingester` and `cmd/collector-git` construct the store and hand it to the
native selector, which observes in githubOrg, gitlabGroup, and explicit mode.

## Exported surface

- `ObservationStore` with `NewObservationStore(database db.ExecQueryer)`.
- `KnownScopes`, `Observations`, `UpsertObservations`,
  `UpsertClassifications` (#8454), `DeleteExpiredObservations`,
  `UpsertSelectorCycle` (#7840).

See `doc.go` for the godoc contract.

## Dependencies

- `internal/storage/postgres/db` for the shared `ExecQueryer` contract.
- `internal/collector/repo/git/membership` for the row and batch types.

## Telemetry

None of its own. Store errors return to `membership.Observer`, which logs
`git_repository_selection_store_failed` with `failure_class`
(`known_scopes_read`, `observations_read`, `upsert`, `classification_upsert`
for a failed verdict write, which leaves the outcome `evaluated`) and counts
`eshu_dp_collector_repository_selection_evaluations_total{outcome="store_error"}`.
A sweep error logs `failure_class=expired_sweep` without changing the outcome,
the rows deleted count on
`eshu_dp_collector_repository_selection_observations_deleted_total`, and the
reclaimed `not_listed` subset counts on
`eshu_dp_collector_repository_selection_not_listed_reclaimed_total` (#7840).
A failed marker write logs `failure_class=selector_cycle_upsert`, also
without changing the outcome. The collector cycle continues.

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
- The sweep predicate is strict (`< now`) and uses each row's own
  `liveness_window_seconds`, so a row at the boundary or with a longer window
  stays. A `not_listed` row (`$4`, bound to `membership.StateNotListed`) is
  deleted only when its selector's `repository_selectors` marker is missing
  or older than the row's window plus grace (#7840): deleting the row of a
  selector that still cycles could make the mass-miss guard count an
  already-confirmed missing scope as newly unlisted and trip forever. Each
  batch is one statement selected `FOR UPDATE OF o SKIP LOCKED`, so a row an
  upsert holds is skipped. `FOR UPDATE` rechecks the expiry predicate
  against the newest row version, so a row renewed before the lock is kept;
  the marker side reads the statement snapshot (see the query comment for
  the revive-in-the-same-instant bound). An upsert that collides with an
  in-flight delete waits, then inserts a fresh row. The live test covers all
  of these and fails the `not_listed`, window, boundary, grace, and lock-wait
  mutants.
- The marker write is advance-only (`last_cycle_at < EXCLUDED.last_cycle_at`),
  like the observation upsert: a lagging replica never rewinds freshness.
  Marker rows are never deleted; they are bounded by the selectors ever
  created, and a missing marker reads as abandoned.

### Expired-row sweep

Performance Evidence (#7774, prove-theory-first): PostgreSQL 18.6 at
127.0.0.1:25432 with 200,000 rows (20,000 scopes x 10 selectors, 48h windows):
one live selector, one inside the grace, and eight abandoned 30 days ago. The
shipped statement was run as a prepared statement with a 7-day grace and a
batch of 500:

| Case | Plan | Buffers | Result |
| --- | --- | --- | --- |
| One batch, unordered (shipped) | seq scan, stops at 500 | 3,014 | 5.3 ms, 500 deleted |
| One batch, `ORDER BY` primary key | primary-key index scan | 3,641 | 4.8 ms, 500 deleted |
| One capped call, 20 batches, unordered | | | 91 ms, 10,000 deleted (116 ms ordered) |
| Steady-state probe, 40,000 kept rows, unordered | seq scan | 5,000 | 3.8-4.4 ms, 0 deleted |
| Steady-state probe, 40,000 kept rows, ordered | full primary-key index scan | 44,203 | 10.5-11.4 ms, 0 deleted |

The drain left all 20,000 live rows and all 20,000 in-grace rows.

Re-measured with the `not_listed` exclusion on the same setup, with one scope
in ten of every selector `not_listed` (16,000 abandoned `not_listed` rows),
two runs each of the shipped statement without and with `state <> $4`:

| Case | Without | With (shipped) |
| --- | --- | --- |
| One batch | 5.2-5.4 ms, 3,014 buffers | 5.2-5.4 ms, 3,015 buffers |
| One capped call, 10,000 deleted | 87-92 ms | 90-91 ms |
| Drain of the rest | 149,500 rows, 1.32-1.36 s | 133,500 rows, 1.23-1.24 s |
| Steady-state probe | 40,000 kept, 5,000 buffers, 4.2-5.2 ms | 56,000 kept, 5,000 buffers, 6.5-8.5 ms |

With the exclusion all 16,000 abandoned `not_listed` rows survive and no
abandoned row of another state does. The probe costs about 3 ms more because
it filters the kept `not_listed` rows on each pass.

The probe runs at most once per collector cycle on shard 0, so the steady state is
the common case; that is why the batch is unordered. No index: the
predicate combines two columns with an interval, and an expression index on
`timestamptz + interval` is not allowed because the operator is only
`STABLE`.

Observability Evidence (#7774): rows the sweep deletes are counted on
`eshu_dp_collector_repository_selection_observations_deleted_total{collector_kind}`.
A sweep with nothing to delete records no sample, and a failed sweep logs
`git_repository_selection_store_failed` with `failure_class=expired_sweep`
without changing the evaluation outcome.
`TestObserverSweepsExpiredRowsAfterAnEvaluation`,
`TestObserverRecordsNoDeletedSampleWhenNothingExpired`, and
`TestObserverSweepFailureIsLoggedAndKeepsTheOutcome` pin these. The count
the observer adds is the value `DeleteExpiredObservations` returns, and
`TestObservationStoreSweepLive` asserts that value against the rows actually
removed (10 of 30 in the guard-history case).

Performance Evidence (#7840): the reclaim predicate was proven before the
change and re-measured with the shipped statement after it, on PostgreSQL
18.6 with 200,000 rows (184,000 live `selected`, 8,000 abandoned
`not_listed` with a stale marker, 8,000 `not_listed` of a still-cycling
selector with a fresh marker; 1h windows, 7-day grace, pinned `now`). Before
(#7774 predicate, `state <> 'not_listed'`): the steady-state probe took
19.1 ms (seq scan, 2,874 buffers, 0 rows) and the abandoned `not_listed`
rows were never selected. After (shipped `LEFT JOIN repository_selectors`
batch with `FOR UPDATE OF o SKIP LOCKED ... RETURNING o.state`): one batch
deleted 500 abandoned `not_listed` rows in 16.7 ms, and the steady-state
probe after the drain took 15.7 ms (0 rows; the still-cycling selector's
8,000 rows filtered by the marker arm). The plan is the same sequential scan
plus a hash of the one-row-per-selector marker table, so the probe cost does
not grow with kept `not_listed` rows of live selectors beyond the scan the
#7774 predicate already paid. No new index, for the same reason as #7774.
Migration 200's backfill stamps pre-existing selectors at
`clock_timestamp()` once (`ON CONFLICT DO NOTHING`), verified idempotent on
re-apply against the same table.

Observability Evidence (#7840): reclaimed rows count on
`eshu_dp_collector_repository_selection_not_listed_reclaimed_total{collector_kind}`
in addition to the deleted total, the INFO `git_repository_selection_evaluated`
log carries `not_listed_reclaimed_count` next to `expired_deleted_count`, and
a failed marker write logs `git_repository_selection_store_failed` with
`failure_class=selector_cycle_upsert` without changing the outcome.
`TestObserverReportsReclaimedNotListed`,
`TestObserverTouchesSelectorCycleOnEveryOutcome`,
`TestObserverSelectorTouchFailureKeepsOutcomeAndLogsClass`, and the
`assertAbandonedNotListedRowsAreReclaimed` leg of
`TestObservationStoreSweepLive` pin these.

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

### Performance Evidence (#7765)

Performance Evidence: the gitlab_group prefix partition read
(`starts_with(lower(payload->>'repo_slug'), $1)` with `$1` the lowercased
group path plus `/`, same host predicate) was re-measured on PostgreSQL
18.6 with 20,000 seeded scopes before any index decision; scripts and raw
output are in `docs/internal/evidence/7765-gitlab-group-prefix-read.md`.
Baseline (prove-first shim on the same shape): sub-millisecond small-set
reads with a 0.9 ms contended read. After: the group read returns exactly
the 900 nested scopes (sibling, lookalike, underscore-prefix, other-host,
`repository_ref`, and remote-less scopes drop out) in **9.22 ms median**
(seq scan, 495 shared buffers), 1.56x the host-filtered first-segment
baseline (5.93 ms) from per-row `lower()` CPU, not a plan difference.
pgbench read-only: 8.5 ms average, stddev 0.5 ms. A `FOR UPDATE` held on a
nested scope does not block the read. The change is safe because the read is
read-only, runs once per cycle on shard 0, and the planner correctly
seq-scans at this scale. **No new index**: an index would add write
amplification without serving this shape.

Observability Evidence: `eshu_dp_collector_repository_listing_requests_total`
(`collector_kind`, `provider`, `outcome`) counts every group resolve and page
request, with INFO `git_gitlab_group_listing_completed` per finished listing
and WARN `git_gitlab_group_listing_retry` per waited retry; selection
outcomes ride the existing `repository_selection_evaluations_total` counter.

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

### Probe verdicts (#8454)

Migration 199 adds three nullable columns to
`repository_selection_observations`: `upstream_classification` (CHECK
`transferred`, `renamed`, `still_present`, `not_found_or_inaccessible`),
`successor_slug`, and `classified_at`. `UpsertClassifications` writes one
selector's probe verdicts in one statement: only `not_listed` rows advance,
and only when the stored `classified_at` is older than the probe time, so a
lagging replica never overwrites a newer verdict. The evaluation upsert
clears all three columns whenever it writes any other state (mirrored by
`membership.project`), so a relisted scope never carries a stale verdict.
The reads (`Observations`, `ReadLiveScopeObservations`) return the verdict
with the row; `TestObservationStoreClassificationLive` pins the round trip,
the stale guard, the relist clear, and the CHECK against real PostgreSQL 18.

Performance Evidence (#8454, prove-theory-first): PostgreSQL 18.6, a scratch
schema at the 802-scope QA shape (one selector, 25 confirmed `not_listed`,
migration 166 DDL plus 199), the exact production UPDATE with a 2-row probe
batch, `EXPLAIN (ANALYZE, BUFFERS)`:

```text
Update on repository_selection_observations o  (actual time=0.087..0.087 rows=0.00 loops=1)
  Buffers: shared hit=27
  ->  Nested Loop
        ->  Subquery Scan on s (rows=2, quicksort on scope_id, 25kB)
        ->  Memoize
              ->  Index Scan using repository_selection_observations_pkey
                    Index Cond: ((scope_id = s.scope_id) AND (selector_id = 'sel'::text))
```

One PK probe per verdict row, 0.087 ms for the batch at QA shape; the batch
is capped at 25 rows per cycle. No new index: the primary key serves the
write. Migration 199 re-ran cleanly on the seeded schema (idempotent
`ADD COLUMN IF NOT EXISTS` plus a guarded CHECK add), the way bootstrap
re-applies every migration on every boot.

Observability Evidence (#8454): the store emits no signal of its own. Probe
answers count on
`eshu_dp_collector_repository_selection_probe_total{outcome}` (`transferred`,
`renamed`, `still_present`, `not_found_or_inaccessible`, `kept_prior`), the
post-cycle verdicts sample on
`eshu_dp_collector_repository_selection_classifications{classification}`
(`unclassified` while no probe has reported), and a failed verdict write
logs WARN `git_repository_selection_store_failed` with
`failure_class=classification_upsert` while the outcome stays `evaluated`.

## Verification

```bash
cd go && go test ./internal/storage/postgres/membership -count=1
ESHU_GENERATION_RETENTION_PROOF_DSN=postgresql://postgres:postgres@localhost:<port>/postgres?sslmode=disable \
ESHU_GENERATION_RETENTION_PROOF_DISPOSABLE=1 \
  go test ./internal/storage/postgres/membership -run 'ObservationStoreLive|ObservationStoreSweepLive' -count=1
```
