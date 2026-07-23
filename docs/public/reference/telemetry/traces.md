# Telemetry Traces

Traces answer one question: where did time go for this request, scope, queue
item, collector claim, or graph write?

Start with metrics to find the changed service or phase, then open traces to
see the storage call, reducer domain, collector step, or graph write that spent
the time.

## Current Contract

The frozen span registry is in `go/internal/telemetry/contract.go`,
companion `contract_*.go` files, and `go/internal/telemetry/registry.go`.
Some graph and content read wrappers also emit literal dependency span names
such as `neo4j.query`, `neo4j.query.single`, and `postgres.query`.

Use `telemetry.SpanNames()` when you need the registered set. Check call sites
when a query handler uses a span constant that has not yet been added to the
registry.

| Family | Current spans |
| --- | --- |
| Collection and projection | `collector.observe`, `collector.stream`, `scope.assign`, `fact.emit`, `projector.run`, `reducer_intent.enqueue`, `canonical.projection`, `canonical.write`, `canonical.retract` |
| Reducer and materialization | `reducer.run`, `reducer.batch_claim`, `reducer.eshu_search_index_write`, `reducer.drift_evidence_load`, `reducer.aws_runtime_drift_evidence_load`, `reducer.cross_repo_resolution`, `reducer.sql_relationship_materialization`, `reducer.inheritance_materialization`, `reducer.s3_external_principal_grant_materialization`, `iac_reachability.materialize`, `shared_acceptance.lookup`, `shared_acceptance.upsert` |
| Query handlers | `query.*` spans for relationship evidence, documentation, IaC, code investigation, entity map, package registry, CI/CD, image identity, SBOM, and supply-chain reads |
| Source collectors and webhooks | `tfstate.*`, `webhook.handle`, `webhook.store`, `oci_registry.*`, `vulnerability_intelligence.*`, `security_alert.*`, `ci_cd_run.*`, `pagerduty.*`, `jira.*`, `package_registry.*`, `scanner_worker.*`, and `aws.*` |
| Storage dependencies | `postgres.exec`, `postgres.query`, `neo4j.execute`; read wrappers can also emit `neo4j.query` and `neo4j.query.single` |

Legacy names such as `eshu.http.*`, `eshu.mcp.*`, `eshu.query.*`,
`eshu.index.*`, `eshu.fact_*`, `eshu.resolution.*`, `eshu.graph.*`, and
`eshu.content.*` are not part of the current Go trace contract.

The instrumentation scope name for all data-plane binaries is
`eshu/go/data-plane` (`telemetry.DefaultSignalName` in
`go/internal/telemetry/contract.go`).  The API binary previously reported
`eshu-api` as its scope name; operators with saved Tempo/Honeycomb trace
queries filtering by scope `eshu-api` should update the filter to
`eshu/go/data-plane`.

## How To Read The Tree

| Area | What to inspect |
| --- | --- |
| Collector | `collector.observe`, `collector.stream`, `scope.assign`, and `fact.emit` with child Postgres spans. |
| Projector | `projector.run`, `reducer_intent.enqueue`, `canonical.projection`, and `canonical.write`. |
| Reducer | `reducer.run`, domain-specific reducer spans, `reducer.eshu_search_index_write` for persisted search-index maintenance, shared acceptance spans, and nested `canonical.write`. |
| Read path | Query-specific spans with child `postgres.query`, `neo4j.query`, or `neo4j.query.single`. |
| Webhook | `webhook.handle`, `webhook.store`, and child `postgres.exec` spans. |
| AWS collector | `aws.collector.claim.process`, `aws.credentials.assume_role`, `aws.service.scan`, and `aws.service.pagination.page`. |
| Terraform-state collector | `tfstate.collector.claim.process`, `tfstate.discovery.resolve`, `tfstate.source.open`, `tfstate.parser.stream`, and `tfstate.fact.emit_batch`. |
| Vulnerability intelligence collector | `vulnerability_intelligence.observe` and `vulnerability_intelligence.fetch`. |
| Security alert collector | `security_alert.observe` and `security_alert.fetch`. |
| CI/CD run collector | `ci_cd_run.observe` and `ci_cd_run.fetch`. |
| PagerDuty collector | `pagerduty.observe` and `pagerduty.fetch`. |
| Jira collector | `jira.observe` and `jira.fetch`; fetch spans carry bounded page, emitted-fact, rejected-link, unsupported-provider, and Retry-After counters. |
| Scanner worker | `scanner_worker.claim.process`, `scanner_worker.analyze`, and `scanner_worker.fact.emit_batch`. |

Keep high-cardinality or sensitive values out of span attributes. Raw bucket
names, object keys, local paths, delivery IDs, commit SHAs, full state
locators, package versions, and cloud resource identifiers belong in controlled
evidence or safe hashed identifiers, not dashboard labels.

## Useful Attributes

The most useful attributes on current Go spans are:

- `scope_id`
- `scope_kind`
- `source_system`
- `generation_id`
- `collector_kind`
- `analyzer`
- `target_kind`
- `domain`
- `partition_key`
- `db.system`
- `db.operation`
- `eshu.store`

Webhook traces also use bounded attributes such as `provider`, `event_kind`,
`decision`, `status`, `outcome`, and `reason`.

Graph-read `neo4j.query` spans use `eshu.graph_read.outcome` (`success`, `slow`,
`recovered`, `deadline`, `caller_deadline`, `unavailable`, `canceled`, or
`error`), `eshu.graph_read.attempts` (1-2), and
`eshu.graph_read.configured_deadline_ms`. `caller_deadline` preserves the
enclosing request's attribution instead of counting it as a graph-policy
deadline. These spans deliberately omit Cypher text and raw driver errors. See
[Graph-read safety](graph-read-safety.md) for the matching API, MCP, metric,
and warning contract.

Jira fetch traces use bounded integer attributes such as `jira.search_pages`,
`jira.changelog_pages`, `jira.remote_link_pages`, `jira.metadata_pages`,
`jira.issues_emitted`, `jira.changelog_events_emitted`,
`jira.remote_links_emitted`, `jira.remote_links_rejected`,
`jira.unsupported_provider_links`, `jira.metadata_objects_scanned`,
`jira.metadata_objects_emitted`, `jira.unsupported_metadata`,
`jira.permission_hidden_metadata`, `jira.stale_metadata`,
`jira.metadata_redactions`, `jira.partial_failures`, `jira.rate_limits`,
`jira.retry_after_seconds`, and `jira.stale_windows`. They must not carry site
IDs, issue keys, user identifiers, summaries, metadata names, custom-field IDs,
or URLs.

## Recipes

| Symptom | Start with | Then inspect |
| --- | --- | --- |
| Scope is slow to collect | `eshu_dp_collector_observe_duration_seconds` | `collector.observe`, `scope.assign`, `fact.emit`, child Postgres spans |
| Projector backlog is old | `eshu_dp_queue_depth`, `eshu_dp_queue_oldest_age_seconds` | `projector.run`, fact-load `postgres.query`, `canonical.write`, `neo4j.execute` |
| Reducer relationship work is slow | `eshu_dp_reducer_run_duration_seconds` and reducer queue age | `reducer.run`, relationship materialization spans, `canonical.write` |
| Graph writes are slow | `eshu_dp_canonical_write_duration_seconds` | `canonical.write` and nested `neo4j.execute` |
| Read path is slow | API/MCP request latency or query handler span | `postgres.query`, `neo4j.query`, `neo4j.query.single`, caller shaping code |
| Webhook intake is rejected or slow | `eshu_dp_webhook_requests_total` and webhook duration metrics | `webhook.handle`, provider verification, normalization, `webhook.store` |

## Non-Claims

- There is no current universal `eshu.query.*` span family.
- Replay, admin, and recovery flows do not have a dedicated trace namespace
  unless code has added a specific span.
- A trace ID follows one trace tree. Use `scope_id`, `generation_id`,
  `work_item_id`, `domain`, and `partition_key` to connect async work across
  services.
