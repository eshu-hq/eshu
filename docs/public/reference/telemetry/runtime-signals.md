# Runtime Signals

Runtime signals answer whether Eshu is alive, ready, making progress, and
current enough to trust.

## Runtime Flow

```text
source event or scheduled scan
  -> collector/runtime claim
  -> fact commit
  -> projector queue
  -> reducer/shared projection queue
  -> graph/content write
  -> API, MCP, or admin read
```

Use runtime signals to locate the blocked step before restarting services or
forcing a broader re-index.

## Shared Runtime Endpoints

Long-running runtimes expose the same operator surface:

| Endpoint | Use |
| --- | --- |
| `/healthz` | Process liveness. |
| `/readyz` | Dependency readiness. |
| `/admin/status` | Queue, generation, domain, and failure state. |
| `/metrics` | Prometheus scrape surface. |

The API, MCP server, ingester, webhook listener, collectors, workflow
coordinator, and resolution engine follow this shape when they run as
long-lived services.

## By Runtime

### API

- Use API request traces and storage spans when a read path is slow.
- Use `/admin/status` before assuming a read answer is stale because of the API.
- Graph-backed read latency usually points to `postgres.query`,
  `neo4j.query`, or `neo4j.query.single` spans.

### MCP Server

- Treat MCP tools as prompt-facing read surfaces.
- Check tool logs and query spans before retrying broad graph requests.
- Keep MCP list-style calls bounded with limits, timeouts, ordering, and
  truncation signals.

### Ingester

- Starts collection, fact commit, source-local projection, content writes, and
  reducer intent enqueue.
- Use collector, fact, projector, and queue metrics together. A slow ingestion
  run can be parse-bound, Postgres-bound, graph-bound, or waiting behind a
  queue.
- Compare `collector.observe`, `fact.emit`, `projector.run`, and dependency
  spans before changing worker counts.

### Webhook Listener

- Authenticates provider deliveries, normalizes events, and stores trigger
  decisions durably.
- Use `eshu_dp_webhook_requests_total` for public delivery volume and rejection
  reasons.
- Use `eshu_dp_webhook_store_duration_seconds` to separate Postgres trigger
  persistence from provider authentication and normalization cost.

### Facts Layer

- Facts are the source of reducer and projector truth.
- Use fact batch metrics to tell whether source collection is still producing
  data or blocked while committing it.
- Use queue age and dead-letter status before replaying or backfilling facts.

### AWS Cloud Collector

- Claims bounded account, region, and service work.
- Uses AWS API, throttle, budget, checkpoint, freshness, and emitted-resource
  metrics to show source behavior without leaking ARNs or resource names.
- Use `aws.service.pagination.page` spans when a scan is slow inside AWS page
  retrieval.

### Package Registry Collector

- Fetches package metadata and emits package identity facts.
- Uses request, rate-limit, parse-failure, emitted-fact, and observation-lag
  metrics.
- Runtime failures persist bounded `failure_class` values. Exact package names,
  feed URLs, versions, and credentials stay out of labels.

### Confluence Collector

- Reads configured Confluence spaces and emits documentation facts.
- Uses request, fetch-duration, permission-denied, document, section, link, and
  sync-failure metrics.
- Permission-denied pages indicate partial documentation syncs caused by source
  ACLs, not necessarily collector failures.

### Resolution Engine

- Drains reducer intents, materializes graph/content truth, performs shared
  follow-up work, and dead-letters terminal failures.
- Use reducer queue wait, run duration, shared projection wait, shared
  processing, and graph/storage spans before tuning concurrency.
- `/admin/status` includes queue blockages when eligible reducer work is held by
  an in-flight conflict domain or key.

### Admin And CLI Status

- Start with `/admin/status` or `eshu-admin-status` when the question is
  freshness, backlog, failure class, or queue progress.
- The status report intentionally summarizes bounded runtime state instead of
  becoming a high-cardinality metrics surface.
- Use it before restart, replay, forced re-index, or worker-count changes.

## Incremental Refresh And Reconciliation

Incremental refresh is healthy when unchanged scopes avoid unnecessary work and
changed scopes still drain through projection and reducer queues.

Watch:

- `eshu_runtime_scope_changed`
- `eshu_runtime_scope_unchanged`
- `eshu_runtime_refresh_skipped_total`
- `eshu_runtime_queue_oldest_outstanding_age_seconds`
- `eshu_dp_queue_oldest_age_seconds`
- reducer and shared-projection queue metrics

If refresh skips rise while users report stale results, inspect source freshness
and generation status before changing the read path.
