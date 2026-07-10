<!-- docs-catalog
title: Freshness and Convergence
description: Explains how operators tell whether the graph has converged on current source observations.
type: operate
audience: operator
entrypoint: true
landing: false
-->

# Freshness and Convergence

Use this page when the operator question is "is the graph current?" rather than
"is the process up?". A healthy pod does not prove the graph has converged on
the latest source observation. This page describes the hosted freshness
dashboard and the alert pack that distinguishes the distinct ways convergence
can fail.

The dashboard and alerts use only bounded metrics from the frozen telemetry
contract (`eshu_runtime_*` status gauges and `eshu_dp_*` data-plane series).
They never expose private repository names, trigger payloads, delivery IDs, or
provider secrets as metric labels. Per-event detail belongs in logs and traces,
not metrics.

## Dashboard

`deploy/grafana/dashboards/eshu-freshness-convergence.json` (UID
`eshu-freshness-convergence`) groups freshness signals into four sections. It
has a `service` template variable backed by
`label_values(eshu_runtime_queue_outstanding, service_name)` so you can scope to
one runtime or view all.

### Generation Convergence

| Panel | Metric | Reads as |
| --- | --- | --- |
| Generation State Totals | `eshu_runtime_generation_total` by `state` | active / pending / completed / superseded / failed lifecycle. Pending or failed trending up means convergence is stalling. |
| Scope Activity | `eshu_runtime_scope_active`, `eshu_runtime_scope_changed`, `eshu_runtime_scope_unchanged` | changed scopes drive new freshness work; unchanged scopes were refresh-skipped. |
| Pending Generations | `eshu_runtime_generation_total{state="pending"}` | graph is behind the latest observation when non-zero. |
| Failed Generations | `eshu_runtime_generation_total{state="failed"}` | any value blocks convergence and degrades runtime health. |
| Runtime Health State | `eshu_runtime_health_state` | `stalled`/`degraded` means alive but not converging. |

### Queue Freshness and Drain

| Panel | Metric | Reads as |
| --- | --- | --- |
| Oldest Outstanding Work Age | `eshu_runtime_queue_oldest_outstanding_age_seconds` | primary freshness signal; rising age means the pipeline is falling behind. |
| Queue State Breakdown | `eshu_runtime_queue_outstanding\|pending\|in_flight\|retrying\|dead_letter` | outstanding draining toward zero is convergence; sustained retrying or dead-letter is a stuck queue. |
| Dead-Letter Items | `eshu_runtime_queue_dead_letter` | work that exhausted retries and will not converge without operator action. |
| Data-Plane Queue Depth by Stage | `eshu_dp_queue_depth` by `queue`, `status` | which stage (fact, projector, reducer) owns the backlog. |

### Reducer Convergence

| Panel | Metric | Reads as |
| --- | --- | --- |
| Reducer Enqueue vs Execution Rate | `eshu_dp_reducer_intents_enqueued_total`, `eshu_dp_reducer_executions_total` | enqueue above execution means the reducer is diverging. |
| Reducer Executions by Domain / Status | `eshu_dp_reducer_executions_total` by `domain`, `status` | which domain is failing or lagging. |
| Reducer / Shared Projection Wait p95 | `eshu_dp_reducer_queue_wait_seconds`, `eshu_dp_shared_projection_intent_wait_seconds` | convergence latency even when executions succeed. |
| Shared Projection & Projection Completion | `eshu_dp_shared_projection_cycles_total`, `eshu_dp_shared_projection_stale_intents_total`, `eshu_dp_projections_completed_total` by `status` | cycles completing with low stale counts means shared materialization is converging. |

### Trigger Handoff (Webhook)

| Panel | Metric | Reads as |
| --- | --- | --- |
| Webhook Trigger Decisions | `eshu_dp_webhook_trigger_decisions_total` by `decision`, `status` | whether external change events become freshness work. |
| Webhook Trigger Store Operations | `eshu_dp_webhook_store_operations_total` by `outcome`, `status` | failed stores mean an accepted trigger never became durable work. |
| Webhook Listener Requests | `eshu_dp_webhook_requests_total` by `outcome`, `reason` | rejected or errored requests at the edge never reach trigger evaluation. |

## Alert Pack

The `eshu.freshness` group lives in both `deploy/observability/alerts.yaml`
(standalone Prometheus) and `deploy/observability/prometheus-rule.yaml`
(Prometheus Operator). The five rules are intentionally disjoint and carry a
`freshness_state` label so an operator can tell the failure modes apart instead
of guessing.

| Alert | `freshness_state` | Fires when | Distinguishes from |
| --- | --- | --- | --- |
| `EshuFreshnessProcessDown` | `process_down` | `eshu_runtime_info` for the reducer is absent for 2m | queue-stuck and reducer-not-converging, which both require the process to be up. |
| `EshuFreshnessQueueStuck` | `queue_stuck` | oldest outstanding work > 1h, outstanding > 0, and age flat or rising for 15m | a busy-but-draining queue (age falling) and process-down. |
| `EshuReducerNotConverging` | `reducer_not_converging` | reducer intent enqueue rate > 1.2x execution rate for 15m | a stuck queue with no new intents and a stopped process. |
| `EshuTriggerHandoffFailed` | `trigger_handoff_failed` | webhook store or trigger-decision operations report `status="failed"` for 10m | the healthy `decision=ignored` path, which never carries `status="failed"`. |
| `EshuStaleAnswerGenerationsPending` | `stale_answer` | pending or failed generations persist while oldest work > 30m | pure latency: it is a correctness signal, not a read-path slowness signal. |

Each rule carries a runbook that names the next metric to inspect and warns
against the wrong remediation (for example, do not add reducer workers for a
stalled lease, and do not silence the stale-answer rule by scaling read
replicas).

## Label Safety

All dashboard queries and alert expressions group only on bounded enum labels:
`service_name`, `state`, `status`, `queue`, `domain`, `decision`, `reason`, and
`outcome`. Repository paths, file paths, work-item IDs, delivery IDs, trigger
payloads, and provider secrets are never used as metric labels — they remain in
logs and trace attributes per the
[Telemetry Overview](../reference/telemetry/index.md).

## Related

- [Telemetry](telemetry.md)
- [Health Checks](health-checks.md)
- [Telemetry Overview](../reference/telemetry/index.md)
- [Reducer And Storage Metrics](../reference/telemetry/metrics-reducer-storage.md)
