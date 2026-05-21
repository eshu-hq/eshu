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
  local-authoritative uses NumCPU) competing for queue items via
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
| `ESHU_PROJECTOR_WORKERS` | min(NumCPU, 8); NornicDB local-authoritative: NumCPU | Concurrent projection workers |
| `ESHU_PROJECTOR_RETRY_DELAY` | 30s | Retry delay for failed source-local projection work |
| `ESHU_PROJECTOR_MAX_ATTEMPTS` | 3 | Max projector attempts before terminal failure |
| `ESHU_NEO4J_BATCH_SIZE` | 500 | Records per UNWIND batch |
| `ESHU_SNAPSHOT_WORKERS` | 8 | Snapshot workers in GitSource |
| `ESHU_PARSE_WORKERS` | 4 | Parser concurrency per snapshot |

For the full environment variable contract, use
[Environment Variables](../reference/environment-variables.md).

## Telemetry

Same instrument set as Bootstrap-Index, plus continuous service-level metrics.
It records queue claim latency, queue depth, oldest queue age, projector stage
duration, and projection success/failure counters so operators can distinguish
collector pressure from graph-write or queue-drain pressure.
