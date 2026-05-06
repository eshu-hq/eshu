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
- **Projector**: N workers (default min(NumCPU, 4), configurable via
  `ESHU_PROJECTOR_WORKERS`) competing for queue items via `FOR UPDATE SKIP LOCKED`.
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
| `ESHU_PROJECTOR_WORKERS` | min(NumCPU, 4) | Concurrent projection workers |
| `ESHU_RETRY_DELAY` | 30s | Retry delay for failed projections |
| `ESHU_MAX_ATTEMPTS` | 3 | Max retry attempts before terminal failure |
| `ESHU_NEO4J_BATCH_SIZE` | 500 | Records per UNWIND batch |
| `ESHU_SNAPSHOT_WORKERS` | 8 | Snapshot workers in GitSource |
| `ESHU_PARSE_WORKERS` | 4 | Parser concurrency per snapshot |

## Telemetry

Same instrument set as Bootstrap-Index, plus continuous service-level metrics.
Also records `eshu_dp_queue_claim_duration_seconds{queue=projector}` for
monitoring claim latency under contention.

---

## Optimization Opportunities

### Performance

**1. Cypher edge writes are per-row** (`storage/cypher/edge_writer.go`)

The `EdgeWriter.WriteEdges()` method loops through rows and executes one
Cypher statement per edge record. This is the same N+1 problem we fixed for
source-local writes with batched UNWIND.

- **Fix**: Apply the batched UNWIND pattern from `cypher/writer.go` to the
  edge writer. Group rows by domain, convert to `[]map[string]any`, execute
  as `UNWIND $rows AS row MERGE ...`.
- **Impact**: Proportional to edge count per projection cycle. For repos with
  thousands of edges, this eliminates thousands of round-trips.

**2. Projector worker count ceiling at 4** (`cmd/ingester/wiring.go:153-168`)

`projectorWorkerCount()` caps at 4 even on 16+ core instances. During
projection-heavy periods, this leaves CPU idle.

- **Fix**: Raise the default cap to `min(NumCPU, 8)` (matching bootstrap-index)
  or remove the cap entirely since `ESHU_PROJECTOR_WORKERS` already exists for
  manual override.

**3. Fixed polling interval with no adaptive backoff**

Collector polls every 1s regardless of work volume. Fix: exponential backoff
on empty polls (1s -> 2s -> 4s -> max 30s), reset on work found.

**4. All facts loaded into memory before projecting**

Large repos (534k facts) load all facts into memory at once. Fix: stream
facts in chunks and project incrementally. High complexity -- only pursue if
memory profiling confirms this is a bottleneck.

### SRE / Operability

**5. No per-repo timeout in collector**

A hung git clone or pathological parse blocks the collector indefinitely.
Fix: configurable per-repo context timeout (`ESHU_REPO_TIMEOUT=5m`). Log
timeouts with `failure_class=timeout` and `scope_id`.

**6. No circuit breaker for the graph backend**

If the graph backend is down, projector workers retry indefinitely. Fix:
circuit breaker that opens after N consecutive failures, backs off
exponentially, and half-opens to test recovery.

**7. Missing queue depth gauge**

Operators can't see projector queue backlog from dashboards. Fix: periodic
gauge reporting `SELECT count(*) FROM projector_queue WHERE claimed_at IS NULL`.

**8. No graceful drain on SIGTERM**

`compositeRunner` cancels all runners immediately -- in-flight work may be
interrupted mid-transaction. Fix: on SIGTERM, stop claiming new work, let
in-flight items complete within a 30s deadline.

### Accuracy / Correctness

**9. No idempotency guard on fact re-emission**

Unchanged repos re-emit identical facts, wasting Postgres writes and
re-enqueuing projector work. Fingerprint infrastructure exists in
`git_snapshot_fingerprint.go` but isn't wired as a skip gate. Fix: compare
generation fingerprint before committing; skip if identical.

**10. Retry injector error masking**

`loadIngesterRetryInjector()` silently uses defaults on parse errors. Fix:
return error on invalid config, fail startup loudly.

---

## Priority Matrix

| # | Category | Impact | Effort | Priority |
|---|----------|--------|--------|----------|
| 1 | Performance | High | Medium | P1 |
| 7 | SRE | High | Low | P1 |
| 9 | Accuracy | High | Medium | P1 |
| 5 | SRE | Medium | Low | P2 |
| 2 | Performance | Medium | Low | P2 |
| 8 | SRE | Medium | Medium | P2 |
| 3 | Performance | Low | Low | P3 |
| 6 | SRE | Low | Medium | P3 |
| 10 | Accuracy | Low | Low | P3 |
| 4 | Performance | Medium | High | P3 |
