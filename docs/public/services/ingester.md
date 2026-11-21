# Ingester

## Role and Purpose

The Ingester is a **long-running StatefulSet** that continuously syncs
repositories. It detects new or changed repos, collects facts, projects
source-local graph records, and enqueues reducer intents for cross-domain
work. It runs indefinitely until stopped via SIGTERM.

**Binary**: `/usr/local/bin/eshu-ingester`
**Kubernetes shape**: StatefulSet + PVC
**Source**: `go/cmd/ingester/`

## Workflow

```text
1. Initialize telemetry
2. Open Postgres and the configured graph backend connection
3. Build collector service (GitSource + IngestionStore)
4. Build projector service (ProjectorQueue + ProjectionRunner)
5. compositeRunner.Run() — starts both concurrently:

   collector.Service.Run()          projector.Service.Run()
   ────────────────────             ─────────────────────
   loop forever:                    loop forever (N workers):
     poll GitSource.Next()            claim from projector queue
     if work available:               load facts
       commit facts to Postgres       project → Cypher graph write
       enqueue projector work         write content to Postgres
     else:                            enqueue reducer intents
       sleep PollInterval (1s)        ack work item
                                    if no work:
                                      sleep PollInterval (1s)
```

## Concurrency Model

- **compositeRunner**: Runs `collector.Service` and `projector.Service` as
  concurrent goroutines. First error cancels all runners.
- **Collector**: Single-threaded poll loop calling `GitSource.Next()`. The
  GitSource internally uses N snapshot workers with size-tiered scheduling
  (two-lane channels for small/large repos, semaphore-limited large repo
  concurrency).
- **Projector**: N workers (default min(NumCPU, 8); NornicDB
  `local_authoritative` uses NumCPU) competing for queue items via
  `FOR UPDATE SKIP LOCKED`.
- **Key difference from Bootstrap-Index**: The ingester runs indefinitely.
  After draining the current batch, `GitSource.Next()` resets and triggers a
  fresh discovery cycle on the next poll.

## Backing Stores

| Store | Usage |
|-------|-------|
| Postgres | Facts, projector queue, content store, reducer intents |
| Graph backend | Source-local graph records via backend-neutral Cypher writers |

## Configuration

| Env Var | Default | Purpose |
|---------|---------|---------|
| `ESHU_PROJECTOR_WORKERS` | min(NumCPU, 8); NornicDB `local_authoritative`: NumCPU | Concurrent projection workers |
| `ESHU_PROJECTOR_RETRY_DELAY` | 30s | Retry delay for failed source-local projection work |
| `ESHU_PROJECTOR_MAX_ATTEMPTS` | 3 | Max projector attempts before terminal failure |
| `ESHU_NEO4J_BATCH_SIZE` | 500 | Records per UNWIND batch |
| `ESHU_SNAPSHOT_WORKERS` | min(NumCPU, 8) | Snapshot workers in GitSource |
| `ESHU_PARSE_WORKERS` | min(NumCPU, 8) | Parser concurrency per snapshot |

For the full environment variable contract, use
[Environment Variables](../reference/environment-variables.md).

## Telemetry

Same instrument set as Bootstrap-Index, plus continuous service-level metrics.
It records queue claim latency, queue depth, oldest queue age, projector stage
duration, and projection success/failure counters so operators can distinguish
collector pressure from graph-write or queue-drain pressure.

## Reducer admission (global ceiling, plus an opt-in per-source leg)

Before the projector runtime enqueues reducer intents, an admission writer
(`go/cmd/ingester/reducer_admission.go`) defers the enqueue while either
signal below is hot. Both signals are totals across **all sources**: one
source can use the whole allowance while another source starves. The
per-source leg from issue #10882 (design record
`docs/internal/design/10882-source-backpressure.md`) layers on top: when
the `ESHU_SOURCE_BACKPRESSURE_*` thresholds are all set, intents whose
source is latched slowed — its oldest claimable reducer work older than
the engage age with claimable depth at or above the floor — defer under
reason `source_behind` while other sources' intents in the same batch
admit immediately. Unset (the default) disables the leg; no Proposed
threshold ships as a default.

| Signal | Engage | Release | Env |
|---|---|---|---|
| Total outstanding reducer depth (`pending\|claimed\|running\|retrying`, active generations) | At or above `ESHU_REDUCER_ADMISSION_HIGH_WATER_MARK` (default 10,000) | Below the mark | `ESHU_REDUCER_ADMISSION_HIGH_WATER_MARK`, `ESHU_REDUCER_ADMISSION_POLL_INTERVAL` |
| Graph-write pressure: retrying reducer rows with `failure_class='graph_write_timeout'` | At or above `ESHU_REDUCER_ADMISSION_RETRYING_HIGH_WATER_MARK` (default 500) | Below `ESHU_REDUCER_ADMISSION_RETRYING_LOW_WATER_MARK` (default 100) | `ESHU_REDUCER_ADMISSION_RETRYING_HIGH_WATER_MARK`, `ESHU_REDUCER_ADMISSION_RETRYING_LOW_WATER_MARK` |

While deferring, `Enqueue` poll-waits (the projector heartbeat keeps the
claim alive) until the signals release or `ESHU_REDUCER_ADMISSION_MAX_WAIT`
(default 5m) expires. On expiry the item fails retryable with an incremented
attempt (`ErrReducerAdmissionTimeout`); it becomes a terminal
`admission_timeout` dead letter only if the attempt budget is exhausted. So a
full queue delays intake and, if the queue never drains within the attempt
budget, drops it as a dead letter — it never applies backpressure to the
source that is actually behind.

Scope gaps to know before tuning these marks: the gate guards the ingester's
own intent enqueue only, and the standalone projector
(`go/cmd/projector/runtime_wiring.go`) enqueues through a plain writer with
no admission gate; while the gate defers, scheduled git collection keeps
committing generations and the projector-stage queue grows instead. The
per-source leg does not yet stretch the git repo-selection schedule, so a
slowed git source is paced at intent enqueue, not at selection.
