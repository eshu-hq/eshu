# Telemetry Traces

Traces answer one question better than any other signal:

**Where did the time go for this specific request, scope, queue item, or
graph write?**

Use metrics to detect a problem first, then use traces to explain which stage,
store, or runtime spent the time.

## Current Trace Contract

The trace contract is the current data-plane contract from
`go/internal/telemetry/contract.go` plus the read-path query wrappers.

The stable span families are:

- `collector.observe`
- `collector.stream`
- `scope.assign`
- `fact.emit`
- `projector.run`
- `reducer_intent.enqueue`
- `reducer.run`
- `reducer.batch_claim`
- `canonical.write`
- `canonical.projection`
- `canonical.retract`
- `ingestion.evidence_discovery`
- `reducer.sql_relationship_materialization`
- `reducer.inheritance_materialization`
- `reducer.cross_repo_resolution`
- `shared_acceptance.lookup`
- `shared_acceptance.upsert`
- `query.relationship_evidence`
- `query.documentation_findings`
- `query.documentation_evidence_packet`
- `query.documentation_packet_freshness`
- `query.dead_iac`
- `query.infra_resource_search`
- `tfstate.collector.claim.process`
- `tfstate.discovery.resolve`
- `tfstate.source.open`
- `tfstate.parser.stream`
- `tfstate.fact.emit_batch`
- `tfstate.coordinator.complete`
- `webhook.handle`
- `webhook.store`
- `oci_registry.scan`
- `oci_registry.api_call`
- `aws.collector.claim.process`
- `aws.credentials.assume_role`
- `aws.service.scan`
- `aws.service.pagination.page`
- `postgres.exec`
- `postgres.query`
- `neo4j.execute`

The read/query layer also emits:

- `query.relationship_evidence`
- `query.documentation_findings`
- `query.documentation_evidence_packet`
- `query.documentation_packet_freshness`
- `query.dead_iac`
- `query.infra_resource_search`
- `postgres.query`
- `neo4j.query`
- `neo4j.query.single`

Legacy span families such as `eshu.http.*`, `eshu.mcp.*`, `eshu.query.*`,
`eshu.index.*`, `eshu.fact_*`, `eshu.resolution.*`, `eshu.graph.*`, and
`eshu.content.*` are not part of the current trace contract.

## How To Read The Trace Tree

### Collector and snapshot path

- `collector.observe` is the top-level collect-and-commit cycle
- `collector.stream` covers the per-scope streaming collection path
- `scope.assign` explains repository selection and scope assignment
- `fact.emit` covers parsing, snapshot shaping, and fact emission for one scope
- child `postgres.exec` and `postgres.query` spans explain Postgres cost inside
  the collector path

### OCI registry collector path

- `oci_registry.scan` is one configured repository scan before durable commit
- `oci_registry.api_call` is one registry call; the `operation` attribute is
  `ping`, `list_tags`, `get_manifest`, or `list_referrers`
- child `postgres.exec` and `postgres.query` spans still belong to the
  collector commit path, not to registry API time

### AWS cloud collector path

- `aws.collector.claim.process` is one workflow-claimed account, region, and
  service slice
- `aws.credentials.assume_role` covers claim-scoped credential acquisition for
  central STS AssumeRole or local workload identity
- `aws.service.scan` is one service scanner run before durable commit
- `aws.service.pagination.page` is one AWS SDK paginated API request

### Projector path

- `projector.run` is one projector claim-and-project cycle
- `canonical.projection` is the scoped materialization sub-phase
- `canonical.write` is the graph/content write phase
- `reducer_intent.enqueue` covers follow-up reducer intent creation
- child `neo4j.execute`, `postgres.exec`, and `postgres.query` spans show store
  cost within the projection cycle

### Reducer path

- `reducer.run` is one reducer claim-and-execute cycle
- `reducer.batch_claim` covers batched reducer claim work where used
- `reducer.cross_repo_resolution` is the cross-repo relationship resolution span
- `shared_acceptance.lookup` covers shared acceptance reads
- `shared_acceptance.upsert` covers shared acceptance writes
- `reducer.sql_relationship_materialization` covers SQL-side relationship
  materialization
- `reducer.inheritance_materialization` covers inheritance/write follow-up
  materialization
- `canonical.write` covers shared projection or canonical edge writes

### Read path

- `postgres.query` traces content-store reads
- `neo4j.query` and `neo4j.query.single` trace graph-backed reads

The read path is intentionally narrower than the write path. It traces storage
cost, not a synthetic transport-layer span family.

### Terraform-state path

- `tfstate.collector.claim.process` wraps one claimed Terraform-state work item
- `tfstate.discovery.resolve` covers exact candidate resolution from seeds and
  committed Git backend facts
- `tfstate.source.open` covers local file or read-only S3 source opens
- `tfstate.parser.stream` covers the streaming state parser
- `tfstate.fact.emit_batch` covers the handoff from parsed facts into the
  claimed generation returned to the shared collector service

Use these spans with the Terraform-state metrics when a state claim is slow.
Metric labels stay bounded, and traces must stay safe too: use backend kind,
result, claim/run correlation, and locator hashes from Terraform-state facts.
Do not put raw bucket names, object keys, local paths, or full state locators in
span attributes.

### Webhook listener path

- `webhook.handle` wraps one provider route request, including body read,
  provider authentication, delivery identity validation, normalization, store
  handoff, and response writing.
- `webhook.store` wraps the durable trigger upsert substep.
- child `postgres.exec` spans from the instrumented Postgres wrapper explain
  query-level persistence cost inside `webhook.store`.

Use these spans with `eshu_dp_webhook_requests_total`,
`eshu_dp_webhook_trigger_decisions_total`, and
`eshu_dp_webhook_store_duration_seconds` when public webhook intake is slow or
rejecting traffic. Metric labels stay bounded; repository names, delivery IDs,
branch names, and commit SHAs must not appear as metric labels.

## Key Attributes

The most useful span attributes on the Go path are:

- `scope_id`
- `scope_kind`
- `source_system`
- `generation_id`
- `collector_kind`
- `domain`
- `partition_key`
- `db.system`
- `db.operation`

For query traces, also pay attention to:

- repo identifiers or entity identifiers added by the caller
- runtime/store labels such as `eshu.store`

For webhook listener traces, also pay attention to bounded attributes:

- `provider`
- `event_kind`
- `decision`
- `status`
- `outcome`
- `reason`

## Investigation Recipes

### A scope is slow to collect

1. Start with `eshu_dp_collector_observe_duration_seconds`.
2. Open the `collector.observe` trace.
3. Check whether time is concentrated in `scope.assign`, `fact.emit`, or child
   Postgres calls.

### Projector backlog is not draining

1. Start with `eshu_dp_queue_depth{queue=projector}` and
   `eshu_dp_queue_oldest_age_seconds{queue=projector}`.
2. Open `projector.run` traces for the slow period.
3. Compare fact-load `postgres.query` spans with `canonical.write` and nested
   `neo4j.execute` spans.

### Reducer relationship work is slow

1. Start with `eshu_dp_reducer_run_duration_seconds` and reducer queue depth.
2. Open `reducer.run` traces.
3. Look for time in `reducer.cross_repo_resolution`,
   `reducer.sql_relationship_materialization`,
   `reducer.inheritance_materialization`, or nested `canonical.write`.

### Graph writes are slow

1. Start with `eshu_dp_canonical_write_duration_seconds`.
2. Open `canonical.write`.
3. Check nested `neo4j.execute` spans and any surrounding reducer/projector
   phase span to see which caller owns the slow write.

### Read path is slow

1. Start with the API or MCP latency metrics for the affected runtime.
2. Open the corresponding query trace.
3. Use `postgres.query`, `neo4j.query`, and `neo4j.query.single` to determine
   whether the tail is in Postgres, Neo4j, or the caller’s shaping code.

### Webhook intake is slow or rejected

1. Start with `eshu_dp_webhook_requests_total` grouped by `provider`,
   `outcome`, and `reason`.
2. If requests are accepted but slow, compare
   `eshu_dp_webhook_request_duration_seconds` with
   `eshu_dp_webhook_store_duration_seconds`.
3. Open the `webhook.handle` trace and check whether time is in provider
   verification, normalization, `webhook.store`, or child `postgres.exec`
   spans.
4. If requests are rejected, use the bounded `reason` label before looking at
   logs for exact request context.

## What This Page Does Not Claim

- It does not claim a Python-style universal `eshu.query.*` family.
- It does not claim every log line has a matching explicit `event_name`.
- It does not claim replay, admin, or recovery flows have their own special
  dedicated trace namespace. They run through the same Go runtime and store spans
  listed above.
