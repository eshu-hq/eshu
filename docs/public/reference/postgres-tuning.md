# Postgres Tuning

Use this guide when Eshu is correct but the front half, queue claim path, or
content/search projection is slower than the repo-scale baseline. Tune from
evidence, not from worker count alone: a larger pool only helps when Postgres
has CPU, I/O, lock, and connection headroom.

## What Postgres Owns

Postgres is on the hot path before graph writes begin. It stores facts,
projection queues, content rows, search-index rows, status, recovery state, and
workflow-control state. The high-volume path is:

```text
discover/parse -> fact_records -> fact_work_items -> reducer/projector ->
content/search tables -> graph-backed query surfaces
```

The main hot tables are:

| Table | Why it matters |
| --- | --- |
| `fact_records` | Durable fact envelope store. Full-corpus runs load rows by `scope_id`, `generation_id`, and `fact_kind`; the table has 63 fact-specific indexes, so write amplification and stale plans are visible here first. |
| `fact_work_items` | Reducer/projector queue table. Claim queries use `FOR UPDATE SKIP LOCKED`, status/visibility predicates, and reducer conflict fences. |
| `workflow_work_items` / `workflow_claims` | Collector workflow queue and lease state. The family queue-depth index backs per-family queue-depth gauges. |
| `content_files` / `content_entities` | Content projection and query rows. Trigram indexes make search useful but can be expensive during bulk local loads. |
| `eshu_search_index_documents` / `eshu_search_index_terms` | Persisted search-document and term index. Term upserts can dominate the tail when search indexing grows faster than reducer drain. |

## Pool Knobs

Runtimes that open Postgres through `internal/runtime.OpenPostgres` read the
same pool variables. That includes the write-heavy ingester, reducer,
projector, bootstrap, workflow, webhook, scanner-worker, and collector
processes. API and MCP open Postgres directly for read surfaces; Helm renders
the same environment variables for those deployments. The API/MCP binaries now
apply the shared pool caps (`ESHU_POSTGRES_MAX_OPEN_CONNS` and the idle/lifetime
values via `ConfigurePostgresPool`); they do not yet apply the
`ESHU_POSTGRES_PING_TIMEOUT` value (they ping with the ambient startup context).

| Variable | Default | Tune when |
| --- | --- | --- |
| `ESHU_POSTGRES_MAX_OPEN_CONNS` | `30` | Workers are blocked waiting for DB connections and Postgres still has connection, CPU, and I/O headroom. |
| `ESHU_POSTGRES_MAX_IDLE_CONNS` | `10` | Runtimes churn connections or repeatedly pay connection setup cost. Keep this at or below max open. |
| `ESHU_POSTGRES_CONN_MAX_LIFETIME` | `30m` | Connections must recycle for network, proxy, or server-side maintenance behavior. Do not lower it to mask slow queries. |
| `ESHU_POSTGRES_CONN_MAX_IDLE_TIME` | `10m` | Idle connections accumulate after bursty collector or reducer phases. |
| `ESHU_POSTGRES_PING_TIMEOUT` | `10s` | Startup readiness fails before the database is reachable on slower environments. |
| `ESHU_PG_MAX_CONNECTIONS` | `640` | Compose Postgres server-side `max_connections`, sized for the largest stack that shares this postgres (the remote-e2e collector fleet). Raise it (and provision RAM) before raising per-process pools or adding pool-holding services beyond the budget below. |

Kubernetes exposes the same knobs under each runtime's
`connectionTuning.postgres` block. The Helm helper renders them into the
`ESHU_POSTGRES_*` environment variables across deployments. Every runtime that
opens a pool bounds it to `ESHU_POSTGRES_MAX_OPEN_CONNS` — the API and MCP servers
apply the shared pool config too, so all pool holders count at the same per-process
cap when sizing.

Size each process against the server limit, not in isolation. PostgreSQL
documents `max_connections` as the server-wide concurrent connection cap and
notes that raising it increases allocated resources, including shared memory:
<https://www.postgresql.org/docs/current/runtime-config-connection.html>.

Use this sizing check before raising pools:

```text
sum(pool-holding services * ESHU_POSTGRES_MAX_OPEN_CONNS)
  + reserved/admin headroom (superuser_reserved_connections, psql, admin-status)
  <= Postgres max_connections
```

In local Compose this inequality is enforced by
`TestComposePostgresMaxConnectionsCoversPoolBudget`: the default
`ESHU_PG_MAX_CONNECTIONS` (640) must cover every pool-holding service at
`ESHU_POSTGRES_MAX_OPEN_CONNS` (30) plus a 20-connection reserve, so adding a
pool-holder or raising the per-process pool without lifting the server ceiling
fails the build.

If that inequality fails, reduce per-runtime pools or add a measured pooling
layer outside Eshu. Do not raise every runtime to the same number just because
one phase is slow.

## Planner Cost Knobs (`random_page_cost`)

`random_page_cost` tells the planner how expensive a random (non-sequential)
page fetch is relative to a sequential one. Postgres ships `4.0`, a
**spinning-disk** ratio: it prices a random heap visit ~4x a sequential read.
On SSD/NVMe the real ratio is close to 1, so `4.0` over-prices random access and
suppresses index plans that would win on flash storage.

### What Eshu already sets

Eshu's local Compose Postgres runs SSD-tuned (see `docker-compose.yaml` and
`docker-compose.neo4j.yml`):

| Setting | Compose value | Postgres default |
| --- | --- | --- |
| `random_page_cost` | `1.1` | `4.0` |
| `effective_io_concurrency` | `200` | `1` |

These are **not** rendered into the Helm/production values: a self-hosted
deployment points Eshu at the operator's own Postgres, and Eshu does not force
planner cost settings on storage it does not control. The B-7 golden-corpus gate
runs the whole pipeline against the Compose Postgres at `random_page_cost=1.1`
and passes, so `1.1` is correctness-safe across the corpus; the question this
section answers is only *when it is performance-safe* on your storage.

### Why it matters for Eshu's read path

Several bounded read queries fetch a capped, ordered candidate set whose ordering
matches a covering/partial index, so an ordered index scan can skip an explicit
`Sort` and terminate after the `LIMIT` -- but only if the planner *chooses* that
plan, which is cost-sensitive. The clearest measured case is the #5490
K8sResource candidate fetch
(`go/internal/query/content_reader_k8s_select_candidates.go`, migration
`077_content_entities_k8s_select_partial_index.sql`); full ladder in
`docs/internal/evidence/5490-k8sresource-candidate-index.md`:

| `random_page_cost` | Plan | Mean exec (ms) |
| --- | --- | ---: |
| `4.0` (Postgres default) | keeps `content_entities_repo_idx` + explicit `Sort` -- new index **not** chosen | 8.20 |
| `1.1` (SSD recommendation) | **naturally** picks the ordered `Index Scan`, no `Sort` | 1.85 |

Same query, same data, byte-identical result set -- only the plan changes.

### No-regression spot-check (representative shapes, SSD)

To confirm the knob does not regress other query shapes, three representative
shapes were measured on a throwaway `postgres:16` seeded with the #5490
worst-case partition (18,000 `content_entities` rows, 6,000 K8sResource in one
repo), Apple M4 Pro / SSD, 5 warm `EXPLAIN ANALYZE` runs each:

| Query shape | `4.0` plan / mean | `1.1` plan / mean | Effect |
| --- | --- | --- | --- |
| K8sResource ordered candidate fetch (`LIMIT 5001`) | `Sort` / ~9.8 ms | partial-index `Index Scan`, no `Sort` / ~0.8 ms | ~12x faster (index adopted) |
| Function ordered paginated scan (`LIMIT 200`, no partial index) | `Incremental Sort` / ~0.84 ms | `Incremental Sort` (same) / ~0.80 ms | unchanged -- no regression |
| Primary-key point lookup (control) | `pkey Index Scan` / ~0.030 ms | `pkey Index Scan` / ~0.033 ms | unchanged -- `rpc`-insensitive |

On SSD, `1.1` unlocked the index-adoption win on the one shape that could use it
and left the others' plans and timings unchanged -- no regression across the
three shapes. This is a same-machine *relative* SSD result, not a portable
wall-clock target: `random_page_cost` re-plans **every** query, and its effect is
fundamentally storage-dependent, so it must be validated on the operator's own
hardware before being applied as a server setting.

### Operator guidance

- **SSD / NVMe local storage, warm cache** (the common modern deployment):
  lowering `random_page_cost` toward `1.1` matches what Eshu's own Compose
  already runs and unlocks ordered-index plans like the K8sResource fetch above.
  This is the Postgres project's own documented SSD recommendation
  (<https://www.postgresql.org/docs/current/runtime-config-query.html#GUC-RANDOM-PAGE-COST>).
- **Spinning disk, cold cache, or high-latency network/EBS-style storage**: do
  **not** lower it blindly. `1.1` tells the planner random and sequential reads
  cost nearly the same; if they do not, an ordered scan performing thousands of
  random single-row heap fetches (for example the <=5,001-row K8sResource cap)
  can be strictly worse -- seconds of I/O -- than the sort-once plan. Leave the
  default, or measure on the real hardware first.
- **Prove it on your storage before committing.** Validate with a bounded
  before/after on a representative statement (see [Run Evidence](#run-evidence)
  and [Hot-Path Checks](#hot-path-checks)):

  ```sql
  SET random_page_cost = 1.1;   -- session-local; compare EXPLAIN ANALYZE
  RESET random_page_cost;       -- back to the server default
  ```

  Apply it as a server setting (or per-role) only after the before/after shows a
  win with no regression on your own storage and cache state.

## Run Evidence

Capture these facts for every tuning run so before/after comparisons survive:

| Evidence | Why |
| --- | --- |
| Eshu commit, graph backend, Postgres version, schema bootstrap state | Makes runs comparable. |
| Runtime worker knobs and Postgres pool knobs | Separates application concurrency from DB concurrency. |
| Queue depth and oldest age at fixed intervals | Shows whether backlogs drain or only move between stages. |
| Stage timings for parse, fact commit, reducer claim, reducer run, content write, search index write, and graph write | Prevents optimizing the wrong phase. |
| `pg_stat_statements` top queries by total time, mean time, p95 if available, rows, and calls | Identifies query shape and plan drift. |
| Table and index stats for hot tables | Separates slow SQL from bloat, stale stats, and write amplification. |
| Autovacuum/analyze timestamps and dead tuple counts | High-churn queue tables need fresh stats. |

PostgreSQL's `pg_stat_statements` module tracks planning and execution
statistics for normalized SQL statements:
<https://www.postgresql.org/docs/current/pgstatstatements.html>. Enable it in
performance environments when allowed, then reset statistics immediately before
a bounded proof run.

## Eshu Metrics To Watch

Start with Eshu telemetry before opening database internals:

| Metric | Read it as |
| --- | --- |
| `eshu_dp_queue_depth` | Current queue depth by queue and status. |
| `eshu_dp_queue_oldest_age_seconds` | Freshness risk. A bounded depth with rising oldest age still means work is stuck. |
| `eshu_dp_queue_source_depth` | Which source system is filling a queue. |
| `eshu_dp_queue_source_oldest_age_seconds` | Which source system is aging. |
| `eshu_dp_queue_claim_duration_seconds` | Reducer/projector claim-path pressure. Compare to the reducer claim-latency gate. |
| `eshu_dp_reducer_run_duration_seconds` | Handler/store/graph work after a claim succeeds. |
| `eshu_dp_projector_stage_duration_seconds` | Projection stage cost, including `content_write`. |
| `eshu_dp_search_index_write_duration_seconds` | Persisted search-document and term write cost. |
| `eshu_dp_canonical_write_duration_seconds` | Graph/content canonical write latency. |
| `eshu_dp_active_generations{age_bucket="stuck"}` | Operator alarm for generations that activated but did not complete. |

Queue depth alone is not a worker-count diagnosis. If oldest age rises while
workers are active, inspect claim latency, lock waits, slow statements, and hot
table stats before increasing workers.

## Hot-Path Checks

Use bounded read-only diagnostics during a run. Replace table names only when
the bottleneck moved.

```sql
SELECT
  stage,
  status,
  count(*) AS rows,
  EXTRACT(EPOCH FROM (now() - min(COALESCE(visible_at, created_at)))) AS oldest_age_seconds
FROM fact_work_items
WHERE status IN ('pending', 'retrying', 'claimed', 'running')
GROUP BY stage, status
ORDER BY stage, status;
```

```sql
SELECT
  schemaname,
  relname,
  n_live_tup,
  n_dead_tup,
  last_autovacuum,
  last_autoanalyze,
  vacuum_count,
  autovacuum_count,
  analyze_count,
  autoanalyze_count
FROM pg_stat_user_tables
WHERE relname IN (
  'fact_records',
  'fact_work_items',
  'workflow_work_items',
  'workflow_claims',
  'content_files',
  'content_entities',
  'eshu_search_index_documents',
  'eshu_search_index_terms'
)
ORDER BY relname;
```

```sql
SELECT
  relname,
  indexrelname,
  idx_scan,
  idx_tup_read,
  idx_tup_fetch
FROM pg_stat_user_indexes
WHERE relname IN ('fact_records', 'fact_work_items', 'eshu_search_index_terms')
ORDER BY relname, idx_scan DESC, indexrelname;
```

PostgreSQL exposes table and index statistics through cumulative statistics
views such as `pg_stat_all_tables`, `pg_stat_user_tables`,
`pg_stat_all_indexes`, and `pg_stat_user_indexes`:
<https://www.postgresql.org/docs/current/monitoring-stats.html>.

## When To Suspect Postgres

Suspect Postgres when:

- fact commit or content/search write stage time grows while parser throughput
  is steady;
- queue oldest age rises while claim workers are active and graph write latency
  is not the long pole;
- `pg_stat_statements` shows the same Eshu query family consuming most total
  execution time across the run;
- hot tables show high dead tuple counts or no recent analyze after a large
  ingest/reducer wave;
- index scans suddenly drop or row estimates are clearly wrong after a data
  shape change;
- pool wait is visible in traces/logs and Postgres still has server headroom.

Routine vacuuming and analyze keep table space and planner statistics healthy;
PostgreSQL's maintenance documentation is the source of truth for server-level
autovacuum behavior:
<https://www.postgresql.org/docs/current/routine-vacuuming.html>.

## What Not To Do

- Do not reduce workers, force batch size `1`, or serialize queue drains as a
  permanent fix for lock conflicts or non-idempotent writes.
- Do not raise `ESHU_POSTGRES_MAX_OPEN_CONNS` beyond Postgres server headroom.
- Do not disable search indexing or trigram indexes to claim a speedup unless
  the run explicitly measures that feature-off profile and the issue says the
  feature may be excluded.
- Do not compare a cold Docker rebuild against a warm runtime run.
- Do not wait hours for an open-ended performance run. State the time bound,
  collect enough evidence, stop, and pivot to the measured bottleneck.

## Related Docs

- [Runtime And Storage Environment](environment-runtime-storage.md)
- [Reducer Claim-Latency Gate](reducer-claim-latency-gate.md)
- [Telemetry Metrics](telemetry/metrics.md)
- [Reducer And Storage Metrics](telemetry/metrics-reducer-storage.md)
- [Profiling And Concurrency](local-testing/profiling-and-concurrency.md)
- [NornicDB Tuning](nornicdb-tuning.md)
