# Evidence: #5593 — `ListActiveStateSnapshotScopes` (config-state-drift catch-up sweep) query shape

Scope: `IngestionStore.ListActiveStateSnapshotScopes`
(`go/internal/storage/postgres/drift_catchup_lister.go`), the query backing
`projector.ConfigStateDriftCatchUpSweeper`
(`go/internal/projector/config_state_drift_catchup_sweeper.go`). Unlike the
existing bootstrap Phase 3.5 sweep (`listActiveStateSnapshotScopesQuery`,
`drift_enqueue.go`), which runs once per one-shot `eshu-bootstrap-index`
process, this query runs on a **fixed interval forever** in the steady-state
reducer (default 5 minutes) — so its cost profile matters in a way the
one-shot query's does not.

This is a **Mandatory Prove-The-Theory-First** proof: measured with
`EXPLAIN (ANALYZE, BUFFERS)` against throwaway, representative-scale data
**before** the query shape landed, per the codebase reviewer's finding that
the original PR shipped this sweeper without measuring its own hot query.

## Machine / backend profile (resource-qualified)

- `machine_profile`: MacBook Pro, Apple M4 Pro (arm64), 12 logical CPU, 64 GiB,
  SSD, macOS 26.5.2.
- Postgres: `postgres:16` (`PostgreSQL 16.14 (Debian 16.14-1.pgdg13+1) on
  aarch64-unknown-linux-gnu`) in Docker, throwaway container
  `eshu-5593-perfcheck-pg`, private host port `25877`, no persistent volume,
  destroyed after this proof.
- `absolute_target_applicable`: false — these are relative before/after shim
  measurements gating a query/index-shape decision, not a reference-profile
  wall-clock target. Absolute numbers here are NOT directly comparable to a
  different reviewer's numbers measured on different hardware/storage (see
  "Discrepancy with the reviewer's numbers" below) — the relative
  before/after ratio and the plan shapes are the load-bearing evidence, not
  the absolute milliseconds.

## Seeded corpus

Real `ingestion_scopes` schema (`migrations/001_ingestion_scopes.sql`),
seeded with a realistic scope-kind mix matching the actual prefixes this
codebase writes (`git-repository-scope:`, `aws:cloud:`, `gcp:project:`,
`state_snapshot:` — see `internal/collector/git_source_processing.go`,
`internal/synth/gcp/*.go`, `internal/scope/tfstate.go`), 2.5% `state_snapshot`
by row count (matching the reviewer's stated proportion), ~90% of
`state_snapshot` rows carrying an `active_generation_id`:

| scope_kind | 500K corpus | 2M corpus |
| --- | ---: | ---: |
| repository | 450,000 | 1,800,000 |
| region (aws:cloud) | 25,000 | 100,000 |
| gcp_project | 12,500 | 50,000 |
| state_snapshot | 12,500 | 50,000 |
| **total** | **500,000** | **2,000,000** |

`git-repository-scope:` and `aws:cloud:` / `gcp:project:` all sort lexically
**before** `state_snapshot:` under the database's default `en_US.utf8`
collation — the root cause below depends on this, and it matches the real
scope_id prefixes this codebase actually writes.

## Theory

`ingestion_scopes_pkey` is a plain btree on `scope_id` (default collation).
There is no `text_pattern_ops` index, so Postgres cannot service a
`LIKE 'state_snapshot:%'` predicate as an index range scan. With
`ORDER BY scope_id ASC LIMIT $1` in the query, the planner has a cheap-looking
option available (walk `ingestion_scopes_pkey` in scope_id order, applying
the `LIKE` filter row by row, stopping at `LIMIT`) that looks attractive by
cost estimate but is a near-full index scan in practice: because
`state_snapshot:` sorts after the dominant real prefixes, the scan must pass
almost the entire table before it starts finding 500 matches.

## OLD shape: `scope_id LIKE 'state_snapshot:%'` + `ORDER BY scope_id LIMIT $1`

```sql
EXPLAIN (ANALYZE, BUFFERS)
SELECT scope.scope_id, scope.active_generation_id
FROM ingestion_scopes AS scope
WHERE scope.scope_id LIKE 'state_snapshot:%'
  AND scope.active_generation_id IS NOT NULL
ORDER BY scope.scope_id ASC
LIMIT 500;
```

**500K rows** (warm cache, 3 runs after 1 cache-fill run):

```
Limit  (cost=0.42..10110.55 rows=500 width=56) (actual time=50.303..55.494 ms rows=500 loops=1)
  Buffers: shared hit=101811 (steady state)
  ->  Index Scan using ingestion_scopes_pkey on ingestion_scopes scope
        Filter: ((active_generation_id IS NOT NULL) AND (scope_id ~~ 'state_snapshot:%'::text))
        Rows Removed by Filter: 487573
Execution Time: 51.066 - 76.897 ms (3 runs: 71.281, 55.494, 51.066 ms)
```

**2M rows** (warm cache, 2 runs):

```
Limit  (cost=0.55..271238.30 rows=13834 width=57) (actual time=360.596..505.930 ms rows=500 loops=1)
  Buffers: shared hit=344938-349936 read=56467-61465
  ->  Index Scan using ingestion_scopes_pkey on ingestion_scopes scope
        Rows Removed by Filter: 1950061
Execution Time: 361.490, 505.930 ms
```

Confirms the reviewer's finding: an Index Scan on the primary key, not a
sequential scan, growing worse as total corpus size grows (not as
state_snapshot count grows) — 51-76 ms at 500K rows, 361-506 ms at 2M rows,
removing essentially the whole table (487,573 of 500,000; 1,950,061 of
2,000,000) via the row-by-row filter.

### Rejected rescue attempt: `text_pattern_ops` index, same query

```sql
CREATE INDEX CONCURRENTLY ingestion_scopes_scope_id_pattern_idx
    ON ingestion_scopes (scope_id text_pattern_ops)
    WHERE active_generation_id IS NOT NULL;
```

Re-running the OLD query (unchanged) with this index present, at 2M rows:
**370-509 ms — no change.** The planner did not pick the pattern-ops index
for this query shape (confirmed by `EXPLAIN`: still `Index Scan using
ingestion_scopes_pkey`). Rescuing the `LIKE` predicate purely by adding an
index, without also changing the query, does not work here without further
investigation into why the planner declines it. **Rejected**: not pursued
further because the equality-predicate rewrite below is strictly simpler and
already proven to work.

## NEW shape: `scope_kind = $1` (equality) + partial index

`scope_kind` is a real, already-populated column on every row this query
targets: `NewTerraformStateSnapshotScope`
(`go/internal/scope/tfstate.go`) sets `ScopeKind: KindStateSnapshot` in the
same struct literal as the `state_snapshot:` scope_id prefix, so the two
predicates select the identical row set for every scope this codebase writes
today. Filtering on `scope_kind` turns the predicate into a plain indexed
equality instead of a prefix-scan problem — "the cheapest query is often the
one that stops asking the expensive question."

```sql
CREATE INDEX CONCURRENTLY IF NOT EXISTS ingestion_scopes_active_state_snapshot_idx
    ON ingestion_scopes (scope_id)
    WHERE scope_kind = 'state_snapshot' AND active_generation_id IS NOT NULL;
```

```sql
EXPLAIN (ANALYZE, BUFFERS)
SELECT scope.scope_id, scope.active_generation_id
FROM ingestion_scopes AS scope
WHERE scope.scope_kind = 'state_snapshot'
  AND scope.active_generation_id IS NOT NULL
ORDER BY scope.scope_id ASC
LIMIT 500;
```

**500K rows** (3 runs):

```
Limit  (cost=0.41..1650.33 rows=500 width=56) (actual time=0.016..1.207 ms rows=500 loops=1)
  Buffers: shared hit=499-511
  ->  Index Scan using ingestion_scopes_active_state_snapshot_idx on ingestion_scopes scope
Execution Time: 0.545, 1.105, 1.070, 1.207 ms
```

**2M rows** (3 runs):

```
Limit  (cost=0.41..46726.96 rows=16663 width=57) (actual time=0.009..0.688 ms rows=500 loops=1)
  Buffers: shared hit=500-511
  ->  Index Scan using ingestion_scopes_active_state_snapshot_idx on ingestion_scopes scope
Execution Time: 0.688, 0.604, 0.619 ms
```

The NEW shape is **flat with total corpus size** (buffer count and execution
time are essentially unchanged from 500K to 2M rows), because the partial
index only ever contains `state_snapshot` rows with an active generation —
its size tracks the state_snapshot count, not the total table size. This is
the correctness argument for why this fix scales: the OLD shape's cost was a
function of total corpus size (git repos dominate), the NEW shape's cost is
a function of the much smaller state_snapshot population.

**Confirmed the OLD LIKE query does NOT use this new index** (re-ran the
unmodified LIKE query with the partial index present, 2M rows): unchanged
plan, `Index Scan using ingestion_scopes_pkey`, ~50 ms at 500K —
`scope_id LIKE 'state_snapshot:%'` does not statically imply
`scope_kind = 'state_snapshot'` to the planner's predicate-implication
checker (different columns, no functional dependency declared). **The query
rewrite is required, not optional** — adding the index alone does not fix
the original query.

### Plan-mode proof (corrected -- the original claim here was false)

An earlier revision of this file claimed a bound `scope_kind = $1` "stays in
use across the custom→generic plan transition," based on 7 consecutive
`EXECUTE` runs that all used the partial index. That claim was never actually
tested: it inferred a generic-plan transition from execution count alone,
without checking whether Postgres had actually switched plan modes. Reviewed
independently against a live 2M-row corpus with the same `PREPARE`/`EXECUTE`
ladder, checking `pg_prepared_statements`:

```sql
PREPARE catchup_lister_final (int) AS
SELECT scope.scope_id, scope.active_generation_id
FROM ingestion_scopes AS scope
WHERE scope.scope_kind = 'state_snapshot'
  AND scope.active_generation_id IS NOT NULL
ORDER BY scope.scope_id ASC
LIMIT $1;
-- 8 consecutive EXECUTE catchup_lister_final(500) calls, all using
-- ingestion_scopes_active_state_snapshot_idx, 0.107-0.847 ms each.

SELECT name, generic_plans, custom_plans FROM pg_prepared_statements
WHERE name = 'catchup_lister_final';
--         name          | generic_plans | custom_plans
-- catchup_lister_final |             0 |            8
```

**`generic_plans = 0` after 8 executions.** No transition ever occurred --
Postgres kept choosing a custom plan every time because the cost gap between
the two plans is large enough that the heuristic never favors switching.
Those 8 runs prove nothing about generic-plan behavior; the ladder never
entered the state the original claim said it validated.

Forcing the issue directly (this was reproduced against the ORIGINAL
`scope_kind = $1` bound-parameter shape, before the fix below):

```sql
SET plan_cache_mode = force_generic_plan;
-- EXPLAIN (ANALYZE, BUFFERS) EXECUTE ... WHERE scope_kind = $1 ...
--   -> Index Scan using ingestion_scopes_pkey   -- partial index NOT used
--   Execution Time: ~296 ms
```

Confirmed: forcing a generic plan makes Postgres fall back to a full
`ingestion_scopes_pkey` scan, reproducing the exact OLD-shape regression this
migration exists to fix -- because Postgres cannot statically prove
`scope_kind = $1` implies the partial index's
`WHERE scope_kind = 'state_snapshot'` predicate when the parameter is
unbound at plan time. The practical risk was low (the cost-based heuristic
should keep choosing custom plans indefinitely), but it was a real,
unmonitored silent-fallback mode: data skew, a planner version change, or a
connection pooler configured with `plan_cache_mode = force_generic_plan`
(not exotic) could all trigger it with no alert.

**Fix: inline the literal instead of binding it.** The call site was
checked, not assumed: `ListActiveStateSnapshotScopes` has exactly one
production caller (`projector.ConfigStateDriftCatchUpSweeper.RunOnce`) and
its own method signature never takes a `scope_kind` argument -- this lister
only ever targets `scope.KindStateSnapshot`, with no variability anywhere in
the call graph. There was no reason to bind it. The query now embeds the
literal directly (built via `fmt.Sprintf` from `scope.KindStateSnapshot`, not
copy-pasted, so a rename of that constant cannot silently desync the SQL text
from the Go value -- see
`TestIngestionStoreListActiveStateSnapshotScopesReturnsBoundedPendingScopes`),
making the predicate statically provable in every plan mode instead of only
the common case.

Re-verified against the FINAL (literal-inlined) shape:

```sql
-- Same 8-execution PREPARE/EXECUTE ladder against the literal-inlined query
-- (only $1 = limit remains bound): generic_plans = 0, custom_plans = 8 --
-- unchanged, the heuristic still prefers custom plans naturally.

SET plan_cache_mode = force_generic_plan;
PREPARE catchup_lister_forced (int) AS
SELECT scope.scope_id, scope.active_generation_id
FROM ingestion_scopes AS scope
WHERE scope.scope_kind = 'state_snapshot'
  AND scope.active_generation_id IS NOT NULL
ORDER BY scope.scope_id ASC
LIMIT $1;
EXPLAIN (ANALYZE, BUFFERS) EXECUTE catchup_lister_forced(500);
--   -> Index Scan using ingestion_scopes_active_state_snapshot_idx
--   Buffers: shared hit=509
--   Execution Time: 0.863 ms
EXPLAIN (ANALYZE, BUFFERS) EXECUTE catchup_lister_forced(500);
--   -> Index Scan using ingestion_scopes_active_state_snapshot_idx
--   Execution Time: 0.362 ms

SELECT name, generic_plans, custom_plans FROM pg_prepared_statements
WHERE name = 'catchup_lister_forced';
--          name          | generic_plans | custom_plans
-- catchup_lister_forced |             2 |            0
```

`generic_plans = 2, custom_plans = 0` -- this time the generic plan path was
genuinely exercised (unlike the bound-parameter ladder above, which never
triggered it naturally), and the index is STILL used, at 0.36-0.86 ms, no
fallback. With the literal inlined, the partial index's applicability no
longer depends on which plan mode Postgres happens to choose. The failure
mode is removed, not documented.

## Write-cost check

Inserted 5,000 non-matching (`repository`) rows and 5,000 matching
(`state_snapshot`, active generation) rows, with and without the partial
index present. Both runs showed timing in the same 74-300 ms band per
5,000-row batch, dominated by this Docker Desktop / macOS VM's inherent I/O
noise (baseline-without-index batches varied 79-298 ms on their own, same
order of magnitude as with-index batches) — **no measurable write
amplification signal distinguishable from environment noise.** This is
expected: the partial index only maintains entries for the ~2.5%
`state_snapshot`-with-active-generation subset of writes, and B-tree
maintenance cost per matching row is small (O(log n) over the much smaller
partial-index population, not the whole table). A precise per-row write-cost
number was not extracted given the noise floor; the qualitative conclusion
(no amplification distinguishable from normal I/O variance, confined to the
small minority of matching writes by construction) is sufficient to accept
this partial index.

## Concurrency / DDL proof

`TestIngestionScopesActiveStateSnapshotIndexAppliesReappliesAndRecoversLive`
(`go/internal/storage/postgres/ingestion_scopes_active_state_snapshot_index_live_test.go`,
`//go:build integration`) proves, against a populated live schema: first
application, identical reapplication, a connection restart followed by
reapplication, rollback (`DROP INDEX CONCURRENTLY`), recovery from a
same-name `INVALID` index left by a failed concurrent build, and that the
final index is actually used by the lister's exact query shape (`EXPLAIN`
plan text contains `ingestion_scopes_active_state_snapshot_idx`). Run:

```
ESHU_POSTGRES_TEST_DSN=postgres://postgres:postgres@localhost:25877/eshu_perf?sslmode=disable \
  go test -tags=integration ./internal/storage/postgres/... \
  -run 'TestIngestionScopesActiveStateSnapshotIndexAppliesReappliesAndRecoversLive' -count=1 -v
```

```
--- PASS: TestIngestionScopesActiveStateSnapshotIndexAppliesReappliesAndRecoversLive (6.60s)
PASS
```

## Discrepancy with the reviewer's numbers

The reviewer reported ~287 ms at 500K rows and ~3,089 ms at 2M rows for the
OLD shape; this proof measured ~51-76 ms and ~361-506 ms on the stated
machine profile above. Both measurements agree on the defect class (Index
Scan on the primary key removing nearly the whole table via row-by-row
filter, growing with total corpus size) and on the fix's effectiveness
(flat sub-1.2ms cost after the rewrite, independent of corpus size). The
absolute-number gap is most likely explained by storage/cache differences
between the two throwaway environments (buffer-hit counts here are
consistent across repeated runs, suggesting warm-cache steady state; a colder
cache or slower disk on the reviewer's host would inflate the absolute
number without changing the plan shape or the relative conclusion). Per
`absolute_target_applicable: false` above, this is a relative before/after
proof, not a claim that either absolute number is the authoritative one.

## Summary table

| Shape | 500K rows | 2M rows | Plan |
| --- | ---: | ---: | --- |
| OLD (`LIKE` + pkey scan) | 51-77 ms | 361-506 ms | Index Scan on `ingestion_scopes_pkey`, ~97-98% of rows removed by filter |
| OLD + `text_pattern_ops` index (rejected) | n/a | 370-509 ms (unchanged) | Still `ingestion_scopes_pkey` |
| NEW (`scope_kind =` + partial index) | 0.5-1.2 ms | 0.6-0.7 ms | Index Scan on `ingestion_scopes_active_state_snapshot_idx` |

**Recommendation:** filter on `scope_kind = $1` (bound parameter), backed by
`ingestion_scopes_active_state_snapshot_idx` (migration 091). Roughly
40-100x faster at 500K rows and 500-850x faster at 2M rows on this machine
profile, and flat with corpus growth rather than degrading linearly with it
— the query this sweeper runs every 5 minutes forever.
