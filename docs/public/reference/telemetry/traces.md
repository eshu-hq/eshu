# Telemetry Traces

Traces answer one operator question: where did the time go for this request,
scope, queue item, or graph write?

Start with metrics to find the failing service or phase, then open traces to
see the storage call, reducer domain, collector step, or graph write that spent
the time.

## Current Contract

The trace source of truth is `go/internal/telemetry/contract.go` plus the
companion `contract_*.go` files and graph read wrappers. Not every runtime emits
every span on every request.

Use `telemetry.SpanNames()` when you need the exhaustive registry. The
operator-facing families are:

| Family | Common spans |
| --- | --- |
| Collection and projection | `collector.observe`, `collector.stream`, `scope.assign`, `fact.emit`, `projector.run`, `canonical.projection`, `canonical.write` |
| Reducer and materialization | `reducer_intent.enqueue`, `reducer.run`, `reducer.batch_claim`, `reducer.cross_repo_resolution`, `reducer.sql_relationship_materialization`, `reducer.inheritance_materialization`, `shared_acceptance.lookup`, `shared_acceptance.upsert` |
| Query and investigation | `query.*` spans for relationship evidence, documentation, IaC, code topic, hardcoded secrets, dead code, call-graph metrics, change surface, package registry, CI/CD, SBOM, and supply-chain impact reads |
| External collectors and webhooks | `tfstate.*`, `webhook.handle`, `webhook.store`, `oci_registry.*`, `vulnerability_intelligence.*`, `package_registry.*`, and `aws.*` |
| Storage dependencies | `postgres.exec`, `postgres.query`, `neo4j.execute` |

Graph-backed reads can also emit `neo4j.query` and `neo4j.query.single`.
Treat them as read-path dependency spans. They do not replace the write-path
`neo4j.execute` span.

Legacy names such as `eshu.http.*`, `eshu.mcp.*`, `eshu.query.*`,
`eshu.index.*`, `eshu.fact_*`, `eshu.resolution.*`, `eshu.graph.*`, and
`eshu.content.*` are not part of the current Go trace contract.

## How To Read The Tree

### Collector and projector

- `collector.observe` wraps a collect-and-commit cycle.
- `collector.stream` covers per-scope streaming collection.
- `scope.assign` explains repository selection and scope assignment.
- `fact.emit` covers parsing, snapshot shaping, and fact emission.
- `projector.run` wraps one claim, fact load, projection, and ack cycle.
- `canonical.projection` is scoped materialization.
- `canonical.write` is the graph/content write phase.
- Child `postgres.*` and `neo4j.*` spans show store cost inside the phase.

### Reducer

- `reducer.run` wraps one reducer claim-and-execute cycle.
- `reducer.batch_claim` covers batched claim work where used.
- `reducer.cross_repo_resolution` is cross-repo relationship resolution.
- `reducer.sql_relationship_materialization` covers SQL-side relationship
  materialization.
- `reducer.inheritance_materialization` covers inheritance follow-up work.
- `shared_acceptance.lookup` and `shared_acceptance.upsert` cover shared
  acceptance reads and writes.
- `canonical.write` covers shared projection or canonical edge writes.

### Read path

Read traces expose storage cost and query-handler spans, not a synthetic
transport namespace.

- `postgres.query` traces content-store and read-model queries.
- `neo4j.query` and `neo4j.query.single` trace graph-backed reads.
- Query-specific spans, such as `query.documentation_findings`,
  `query.hardcoded_secret_investigation`, or `query.call_graph_metrics`,
  identify the handler-level operation.

### Source collectors and webhook intake

- `tfstate.*` spans follow Terraform-state work from claim through streaming
  parse and fact emission.
- `oci_registry.*`, `vulnerability_intelligence.*`, `package_registry.*`, and
  `aws.*` spans identify external collector work before durable fact commit.
- `webhook.handle` wraps provider authentication, delivery validation,
  normalization, store handoff, and response writing.
- `webhook.store` wraps the durable trigger upsert.

Keep high-cardinality or sensitive values out of span attributes. Raw bucket
names, object keys, local paths, delivery IDs, commit SHAs, and full state
locators belong in bounded logs or hashed identifiers, not dashboard labels.

## Key Attributes

The most useful attributes on the Go path are:

- `scope_id`
- `scope_kind`
- `source_system`
- `generation_id`
- `collector_kind`
- `domain`
- `partition_key`
- `db.system`
- `db.operation`
- `eshu.store`

Webhook traces also use bounded attributes such as `provider`, `event_kind`,
`decision`, `status`, `outcome`, and `reason`.

## Investigation Recipes

### A scope is slow to collect

1. Start with `eshu_dp_collector_observe_duration_seconds`.
2. Open the `collector.observe` trace.
3. Compare time in `scope.assign`, `fact.emit`, and child Postgres spans.

### Projector backlog is not draining

1. Start with `eshu_dp_queue_depth{queue=projector}` and
   `eshu_dp_queue_oldest_age_seconds{queue=projector}`.
2. Open `projector.run` traces for the slow period.
3. Compare fact-load `postgres.query` spans with `canonical.write` and nested
   `neo4j.execute` spans.

### Reducer relationship work is slow

1. Start with `eshu_dp_reducer_run_duration_seconds` and reducer queue depth.
2. Open `reducer.run` traces.
3. Look for time in cross-repo resolution, SQL materialization, inheritance
   materialization, or nested `canonical.write`.

### Graph writes are slow

1. Start with `eshu_dp_canonical_write_duration_seconds`.
2. Open `canonical.write`.
3. Check nested `neo4j.execute` spans and the parent reducer or projector span.

### Read path is slow

1. Start with the API or MCP latency signal for the affected runtime.
2. Open the corresponding query trace.
3. Use `postgres.query`, `neo4j.query`, and `neo4j.query.single` to classify
   the tail as Postgres, graph backend, or caller shaping code.

### Webhook intake is slow or rejected

1. Start with `eshu_dp_webhook_requests_total` grouped by `provider`,
   `outcome`, and `reason`.
2. Compare `eshu_dp_webhook_request_duration_seconds` with
   `eshu_dp_webhook_store_duration_seconds`.
3. Open `webhook.handle` and check provider verification, normalization,
   `webhook.store`, and child `postgres.exec` spans.

## Non-Claims

- There is no current universal `eshu.query.*` span family.
- Replay, admin, and recovery flows do not have a separate dedicated trace
  namespace unless code has added a specific span.
- A trace ID follows one trace tree. Use correlation keys such as `scope_id`,
  `generation_id`, `work_item_id`, `domain`, and `partition_key` to connect
  async work across services.
