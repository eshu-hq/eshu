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
processes. API and MCP currently open Postgres directly for read surfaces; Helm
renders the same environment variables for those deployments, but the API/MCP
binaries do not apply the pool or ping-timeout values yet.

| Variable | Default | Tune when |
| --- | --- | --- |
| `ESHU_POSTGRES_MAX_OPEN_CONNS` | `30` | Workers are blocked waiting for DB connections and Postgres still has connection, CPU, and I/O headroom. |
| `ESHU_POSTGRES_MAX_IDLE_CONNS` | `10` | Runtimes churn connections or repeatedly pay connection setup cost. Keep this at or below max open. |
| `ESHU_POSTGRES_CONN_MAX_LIFETIME` | `30m` | Connections must recycle for network, proxy, or server-side maintenance behavior. Do not lower it to mask slow queries. |
| `ESHU_POSTGRES_CONN_MAX_IDLE_TIME` | `10m` | Idle connections accumulate after bursty collector or reducer phases. |
| `ESHU_POSTGRES_PING_TIMEOUT` | `10s` | Startup readiness fails before the database is reachable on slower environments. |

Kubernetes exposes the same knobs under each runtime's
`connectionTuning.postgres` block. The Helm helper renders them into the
`ESHU_POSTGRES_*` environment variables across deployments; count only
`OpenPostgres` callers when sizing effective pool caps until API/MCP adopt the
shared runtime opener.

Size each process against the server limit, not in isolation. PostgreSQL
documents `max_connections` as the server-wide concurrent connection cap and
notes that raising it increases allocated resources, including shared memory:
<https://www.postgresql.org/docs/current/runtime-config-connection.html>.

Use this sizing check before raising pools:

```text
sum(OpenPostgres runtime replicas * ESHU_POSTGRES_MAX_OPEN_CONNS)
  + migration/bootstrap/admin headroom
  + operator emergency headroom
  <= Postgres max_connections
```

If that inequality fails, reduce per-runtime pools or add a measured pooling
layer outside Eshu. Do not raise every runtime to the same number just because
one phase is slow.

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
