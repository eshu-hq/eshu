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

Use runtime signals to locate the blocked step before restarting services,
forcing replay, or broadening a re-index.

## Shared Runtime Endpoints

Long-running runtimes mount the shared operator surface:

| Endpoint | Use |
| --- | --- |
| `/healthz` | Process liveness. |
| `/readyz` | Dependency readiness. |
| `/admin/status` | Queue, generation, domain, failure, and completeness state. |
| `/metrics` | Prometheus scrape surface with runtime status metrics plus OTEL data-plane metrics where the runtime wires a Prometheus handler. |

The API, MCP server, ingester, projector, reducer, webhook listener, workflow
coordinator, and hosted collectors follow this shape when they run as
long-lived services.

## First Checks By Runtime

| Runtime | Check first |
| --- | --- |
| API | Request logs, query spans, `/admin/status`, and storage spans before assuming the read handler is stale. |
| MCP Server | Tool logs and query spans. Keep list-style calls bounded with limit, timeout, ordering, and truncation. |
| Ingester | `collector.observe`, `fact.emit`, `projector.run`, fact counters, and queue age. |
| Projector | `projector.run`, `canonical.projection`, `canonical.write`, projector stage duration, and queue age. |
| Webhook Listener | `eshu_dp_webhook_requests_total`, webhook duration metrics, `webhook.handle`, and `webhook.store`. |
| Workflow Coordinator | `eshu_runtime_coordinator_*` status gauges plus `eshu_dp_workflow_coordinator_*` reconcile and reap metrics. |
| AWS Cloud Collector | AWS API, throttle, budget, checkpoint, freshness, emitted-resource, and `aws.service.pagination.page` signals. |
| Package Registry Collector | Request, rate-limit, parse-failure, emitted-fact, observation-lag, and failure-class signals. |
| SBOM Attestation Collector | Claim status, fetch failure class, parse warnings, emitted fact counts, redacted source URI, and reducer attachment status. |
| Confluence Collector | Request, fetch-duration, permission-denied, document, section, link, and sync-failure metrics. |
| Semantic Extraction | `semantic_extraction.queue.*` spans, semantic queue depth/age gauges, semantic queue event counters, budget token/cost counters, and redacted `/api/v0/status/semantic-extraction` queue/budget/audit readbacks. |
| Resolution Engine | Reducer queue wait, run duration, shared projection wait, shared processing, graph/storage spans, and `/admin/status` conflict-domain state. |

Facts are the source of reducer and projector truth. Use fact batch metrics to
tell whether source collection is still producing data or blocked while
committing it. Use queue age and dead-letter status before replaying or
backfilling facts.

## Freshness And Progress

Start with:

- `eshu_runtime_health_state`
- `eshu_runtime_queue_outstanding`
- `eshu_runtime_queue_oldest_outstanding_age_seconds`
- `eshu_runtime_stage_items`
- `eshu_runtime_domain_oldest_age_seconds`
- `eshu_runtime_collector_generation_dead_letter`
- `eshu_runtime_collector_generation_replay_requested`
- `eshu_runtime_collector_generation_replay_attempts`
- `eshu_runtime_collector_generation_dead_letter_oldest_age_seconds`
- `eshu_dp_queue_depth`
- `eshu_dp_queue_oldest_age_seconds`

Incremental refresh is healthy when unchanged scopes avoid unnecessary work and
changed scopes still drain through projection and reducer queues. Watch:

- `eshu_runtime_scope_changed`
- `eshu_runtime_scope_unchanged`
- `eshu_runtime_refresh_skipped_total`

If refresh skips rise while users report stale results, inspect source
freshness, generation status, and reducer/shared backlog before changing the
read path.

## Before Changing Workers

Do not tune worker counts from one metric. Compare:

- queue depth and oldest age
- claim duration
- handler or stage duration
- graph and Postgres storage spans
- retry, dead-letter, and failure-class logs
- `/admin/status` conflict-domain or key state

High queue age with low handler time points to claim, routing, conflict-domain,
or scheduling pressure. High handler time points to source reads, fact loading,
storage, graph writes, or query shape.

Collector generation dead-letter gauges describe commit failures before normal
projector work items existed. Fix the commit failure first, then use
`/admin/replay-collector-generations` with a collector kind, optional scope
filter, optional failure class, and bounded limit to request source-level
replay.
