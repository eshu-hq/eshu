# 3 AM Operator Runbook

Start from the symptom you were paged for and follow one path. Each path names
the status endpoint to read, the metric to chart, the trace or log field to
pivot on, and the one safe remediation step. Every signal below is a deterministic
read; none requires spelunking, an LLM provider, or write access to the graph.

Begin every page the same way:

```bash
curl -fsS -H "Authorization: Bearer $ESHU_API_KEY" "$ESHU_SERVICE_URL/api/v0/status/operator-control-plane"
```

The operator control-plane read model answers "what is broken, stale, blocked,
or safe to replay" in one call: queue depth with claim-latency and stuck-work
signals, reducer-domain backlogs, collector-family promotion verdicts with the
newest proof artifact, and dead-letter state classed by reducer domain and the
collector-generation commit path. Its `health.state` and the four sections below
route you to the right path. The MCP tool `get_operator_control_plane` returns
the same payload for assistant-driven triage.

!!! note "Scope and redaction"
    Scoped tokens receive the same aggregate counts with raw work-item, scope,
    and generation identifiers and instance-level labels withheld. Never paste a
    raw ARN, hostname, IP, key path, or repo-local secret into a ticket; the
    status surfaces already redact these and expose only `resource.fingerprint`,
    `resource.identity_kind`, and `resource.type`.

## Symptom: stale answer

A query returned an answer the user says is out of date, or a truth envelope
reported `truth.freshness.state = stale`.

- **Status endpoint:** `GET /api/v0/status/freshness-causality`. It explains
  staleness by closed cause (`pending_repo_generation`, `reducer_backlog`,
  `dead_lettered_domain`, `missing_collector_completion`, and the per-answer
  `content_coverage_unavailable`, `unsupported_profile`, `retention_expired`),
  shows the generation lifecycle including retired (superseded) generations, and
  reports pending projection work. Each observed cause carries a bounded
  `next_check` drilldown.
- **Metric:** `eshu_runtime_queue_oldest_outstanding_age_seconds` and
  `eshu_dp_queue_oldest_age_seconds` show how far behind projection is.
- **Trace / log field:** pivot on `generation_id` and `domain` from the
  freshness `recent_transitions` rows to the `projector.run` and `reducer.run`
  spans for the lagging scope; `pipeline_phase` separates `projection` from
  `reduction`.
- **Safe remediation:** if the cause is `pending_repo_generation` or
  `reducer_backlog`, the answer is catching up on its own — confirm
  `state = building` and let it drain. If the cause is `dead_lettered_domain`,
  follow [deadletter growth](#symptom-deadletter-growth). Do not force a replay
  for a building scope.

## Symptom: stuck reducer

Projection has stopped advancing: `health.state = stalled`, or outstanding work
is not draining.

- **Status endpoint:** the operator control-plane `queue.claim_latency`
  (`overdue_claims`, `oldest_outstanding_age`, `coordinator_oldest_pending`) and
  `queue.stuck` (`oldest_outstanding_age`, `blocked_conflicts`); drill into
  `reducer_domains` for the hottest domain.
- **Metric:** `eshu_dp_worker_pool_active` (are workers running at all?),
  `eshu_runtime_queue_outstanding` vs `eshu_runtime_queue_in_flight` (claimed but
  not progressing), and `eshu_dp_reducer_run_duration_seconds` (slow handler).
- **Trace / log field:** the `reducer.run` span carries `domain`,
  `partition_key`, and `conflict_key`; a hot `conflict_key` with many
  `blocked_conflicts` points at conflict-domain contention rather than a crash.
- **Safe remediation:** if `overdue_claims > 0` with idle workers, the lease
  holder died — claims expire and re-deliver on their own; confirm
  `eshu_dp_worker_pool_active > 0` and let the lease lapse. Do not lower worker
  counts to "unstick" a non-idempotent write; that hides the defect.

## Symptom: collector failure

A source is not refreshing, or a collector family reports unhealthy.

- **Status endpoint:** operator control-plane `collector_families` (per-kind
  `promotion_state`, `health`, `claim_state`, `reducer_readback`, newest
  `last_observed_at`, and `blockers`), or `GET /api/v0/status/collector-readiness`
  for the full fleet with recommended next actions.
- **Metric:** `eshu_dp_collector_observe_duration_seconds` and the
  `collector_generation` dead-letter counts in the operator read model
  (`dead_letters.collector_generation`).
- **Trace / log field:** the `collector.observe` and `collector.stream` spans
  carry `source_system` and `collector_kind`; `failure_class` on the log line
  classifies the failure. Source identity appears only as `resource.fingerprint`
  / `resource.identity_kind`.
- **Safe remediation:** a `gated`, `disabled`, or `unsupported` family is usually
  intentional configuration, not an outage — read the `blockers`. A collector
  whose commit dead-lettered before reaching the queue is handled under
  [deadletter growth](#symptom-deadletter-growth).

## Symptom: deadletter growth

Dead-letter counts are rising in the operator read model
(`dead_letters.queue_dead_letter`, `by_domain`, or `collector_generation`).

- **Status endpoint:** operator control-plane `dead_letters` for the class
  breakdown and `latest_failure` (newest `failure_class`, `domain`, and — for
  shared tokens — the correlating `work_item_id`/`scope_id`/`generation_id`).
- **Metric:** `eshu_runtime_queue_dead_letter` and, on `/admin/status`,
  `collector_generation_dead_letters.oldest_dead_letter_age_seconds`.
- **Trace / log field:** pivot on `failure_class` + `domain`; the
  [failure classification](../reference/telemetry/logs.md) separates retryable
  (`dependency_unavailable`, `timeout`) from non-retryable (`input_invalid`) and
  quarantined (`unsafe_payload`) classes.
- **Safe remediation — replay is guarded.** Use the safe replay workflow:

    ```bash
    curl -fsS -X POST -H "Authorization: Bearer $ESHU_API_KEY" \
      -H "Content-Type: application/json" \
      "$ESHU_SERVICE_URL/api/v0/admin/replay" \
      -d '{"failure_class":"timeout","reason":"<why this is safe now>","idempotency_key":"<unique-key>"}'
    ```

    Replay requires an explicit `reason`, an `idempotency_key` (so retries and
    concurrent delivery never double-replay), and an admin (all-scopes) token.
    Non-retryable (`input_invalid`) and quarantined (`unsafe_payload`) classes are
    excluded from broad replays and return `422` on an explicit target unless you
    set `force=true` after fixing the cause. A duplicate key returns the prior
    outcome (`duplicate=true`); a reused key with different parameters, or one
    whose replay is in progress, returns `409`. The CLI mirrors this:
    `eshu admin facts replay --reason "<why>" --failure-class timeout`. Every
    accepted or refused replay records an `admin_recovery_action` governance audit
    event carrying no secret values. See
    [Safe Replay Workflow](../reference/http-api/status-admin.md#safe-replay-workflow).

## Symptom: graph backend degradation

Queries are slow or erroring while the queue looks healthy.

- **Status endpoint:** `GET /readyz` for dependency readiness, then
  `GET /api/v0/status/pipeline` for the projection view.
- **Metric:** `eshu_dp_api_request_errors_total`,
  `eshu_dp_canonical_write_duration_seconds`, and
  `eshu_dp_canonical_retract_duration_seconds`; a spike here with a healthy queue
  points at the graph backend, not the pipeline.
- **Trace / log field:** the `query.*` spans with their `postgres.query` /
  `neo4j.query` children isolate whether time is spent in the API handler or the
  backend; `eshu.store` names the backend.
- **Safe remediation:** confirm the backend's own health and headroom before
  touching Eshu. Do not raise worker or batch defaults to compensate for a slow
  backend; that moves contention, it does not remove it. See
  [Cypher Performance](../reference/cypher-performance.md).

## Symptom: missing proof

A release gate or audit needs a proof artifact that is not present.

- **Status endpoint:** operator control-plane `collector_families[].last_observed_at`
  is the newest proof-artifact timestamp per family;
  `GET /api/v0/status/collector-readiness` reports each family's last proof time,
  blockers, and recommended next action.
- **Metric / field:** the `collector.observe` span's `last_observed_at` and the
  promotion `telemetry` handles name the exact metric and span to chart for the
  family.
- **Safe remediation:** an absent proof for a `gated` or `unsupported` family is
  expected — read the family `blockers`. For an implemented family with stale
  proof, follow [collector failure](#symptom-collector-failure). Record the
  proof commands actually run; an explanation alone is not proof. See
  [Local Testing](../reference/local-testing.md) for the proof gates.

## Escalation and proof

- For freshness causality across many scopes, the console **Freshness** dashboard
  renders these same signals (overall state, per-cause table, generation
  lifecycle, recent transitions) with the tenant-scoped view withholding raw
  identifiers.
- Capture the operator control-plane payload and the relevant metric series in
  the incident; both are deterministic reads safe to attach once redaction is
  confirmed.
- Related: [Health Checks](health-checks.md), [Telemetry](telemetry.md),
  [Troubleshooting](troubleshooting.md), and the
  [Hosted Operations Runbook](hosted-operations-runbook.md).
