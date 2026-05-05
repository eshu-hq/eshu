# Bootstrap-Index (Indexer)

## Role and Purpose

Bootstrap-Index is a **one-shot job** that seeds the Postgres fact store and
configured graph backend before the API and Ingester come online. It runs as an
init container (or standalone job) during deployment and exits when all
repositories have been collected and projected.

**Binary**: `/usr/local/bin/eshu-bootstrap-index`
**Kubernetes shape**: Job / init container
**Source**: `go/cmd/bootstrap-index/`

## Workflow

```text
1. Initialize telemetry (OTEL traces, metrics, structured logging)
2. Open Postgres connection, apply bootstrap schema
3. Open graph backend connection (InstrumentedExecutor with batch support)
4. Build collector deps (GitSource, IngestionStore committer)
5. Build projector deps (ProjectorQueue, FactStore, ProjectionRunner)
6. runPipelined() — concurrent collection and projection:

   Collector goroutine              Projector goroutine
   ─────────────────                ───────────────────
   discover repos (SelectionBatch)  poll projector queue
   for each repo:                   for each claimed item:
     snapshot → shape → emit facts    load facts from Postgres
     commit to Postgres               project graph records
     enqueue projector work item      batch UNWIND write to graph backend
   signal collectorDone              write content to Postgres
                                      enqueue reducer intents
                                    drain mode (5 empty polls → exit)
```

## Concurrency Model

- **Collection**: N snapshot workers (default 8, configurable via
  `ESHU_SNAPSHOT_WORKERS`) with size-tiered scheduling. Small repos stream
  freely; large repos (>500 files) acquire a semaphore limiting concurrent
  large parses (default 2, `ESHU_LARGE_REPO_MAX_CONCURRENT`).
- **Projection**: N workers (default min(NumCPU, 8), configurable via
  `ESHU_PROJECTION_WORKERS`) compete for queue items via Postgres
  `FOR UPDATE SKIP LOCKED`.
- **Pipelining**: `drainingWorkSource` wraps the projector queue. While the
  collector runs, empty claims trigger a poll wait. After the collector
  finishes, consecutive empty claims are counted; 5 empties triggers exit.

## Backing Stores

| Store | Usage |
|-------|-------|
| Postgres | Facts, projector queue, content store, reducer intents |
| Neo4j | Source-local graph records (batched UNWIND, 500/batch default) |

## Configuration

| Env Var | Default | Purpose |
|---------|---------|---------|
| `ESHU_PROJECTION_WORKERS` | min(NumCPU, 8) | Concurrent projection workers |
| `ESHU_SNAPSHOT_WORKERS` | 8 | Concurrent snapshot workers |
| `ESHU_LARGE_REPO_MAX_CONCURRENT` | 2 | Max concurrent large repo parses |
| `ESHU_LARGE_REPO_THRESHOLD` | 500 | File count threshold for "large" |
| `ESHU_STREAM_BUFFER` | worker count | Generation stream channel buffer |
| `ESHU_NEO4J_BATCH_SIZE` | 500 | Records per UNWIND batch |
| `ESHU_POSTGRES_DSN` | required | Postgres connection string |
| `NEO4J_URI` | required | Neo4j bolt URI |

## Telemetry

| Signal | Instruments |
|--------|-------------|
| Spans | `collector.observe` (per repo), `collector.stream` (full stream), `fact.emit` (per snapshot), `projector.run` (per projection) |
| Histograms | `eshu_dp_collector_observe_duration_seconds`, `eshu_dp_repo_snapshot_duration_seconds`, `eshu_dp_projector_run_duration_seconds`, `eshu_dp_pipeline_overlap_seconds`, `eshu_dp_neo4j_batch_size` |
| Counters | `eshu_dp_facts_emitted_total`, `eshu_dp_facts_committed_total`, `eshu_dp_projections_completed_total`, `eshu_dp_neo4j_batches_executed_total` |
| Logs | `bootstrap scope collected`, `bootstrap projection succeeded/failed`, `bootstrap pipeline complete` (with `overlap_seconds`, `total_seconds`) |

See [Telemetry Reference](../reference/telemetry/index.md) for the full
instrument catalog.

## Recent Optimizations (completed)

1. **Batched UNWIND writes**: Replaced per-record Cypher `MERGE` with batched
   `UNWIND` (500 records/batch). 534k-fact repos: ~1,068 round-trips instead
   of 534,000. ~500x improvement.

2. **Pipelined collection and projection**: Collection and projection now run
   concurrently via `runPipelined()`. Small repos finish end-to-end while
   large repos are still being collected.

3. **InstrumentedExecutor**: Cypher graph writes are wrapped with OTEL tracing
   and metrics (duration histogram, batch size histogram, batch count counter).
