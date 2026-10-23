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
- **Metric:** `eshu_runtime_health_state` and `/healthz` confirm process
  liveness, `eshu_runtime_queue_outstanding` vs
  `eshu_runtime_queue_in_flight` separates backlog from claimed work, and
  `eshu_dp_reducer_run_duration_seconds` shows slow handlers. Treat
  `eshu_dp_worker_pool_active` as in-flight worker intent, not process liveness.
  During migration 096, a nonzero
  `eshu_runtime_provenance_edge_identity_upgrade_required` with
  `eshu_runtime_provenance_edge_identity_upgrade_applied=1` attributes
  apparently successful work returning to pending to an incompatible old
  reducer.
- **Trace / log field:** the `reducer.run` span carries `domain`,
  `partition_key`, and `conflict_key`; a hot `conflict_key` with many
  `blocked_conflicts` points at conflict-domain contention rather than a crash.
- **Safe remediation:** if `overdue_claims > 0` and the reducer process is
  healthy, the lease holder may have died after claiming work — claims expire
  and re-deliver on their own. Confirm `/healthz` or runtime status plus
  `eshu_runtime_queue_in_flight` / `overdue_claims`, then let the lease lapse.
  Do not require `eshu_dp_worker_pool_active > 0` during the wait; it can be `0`
  after the handler has exited. Do not lower worker counts to "unstick" a
  non-idempotent write; that hides the defect.
  If the migration 096 gauges identify an old reducer, roll that reducer
  forward. Do not disable the compatibility-fence triggers.

### Deferred reconciliation: `security_alert_reconciliation_waiting_for_impact`

A `security_alert_reconciliation` work item that logs WARN `reducer execution deferred`
with `failure_class=security_alert_reconciliation_waiting_for_impact` is waiting
by design. The repository has alert facts and owned dependency evidence, but the
package has no impact finding yet. The queue retries it, and it is counted as
`eshu_dp_reducer_executions_total{domain="security_alert_reconciliation",status="deferred"}`,
not `failed`.

- **Do nothing** while the deferred count is small and the impact findings
  arrive on the next attempt. Do not page on `deferred` alone.
- **Worry** when reconciliations keep resolving at attempt 3 with `unmatched`
  rows while `supply_chain_impact` is slow or backlogged. The handler waits for
  impact evidence on attempts 1 and 2 only. At attempt 3 it stops waiting,
  writes `unmatched` decisions, and the attempt records `succeeded`. At the
  default budget (`ESHU_REDUCER_MAX_ATTEMPTS=3`) the wait therefore never fails
  and nothing dead-letters. Check the `supply_chain_impact` queue age and
  blockages for the package's scope and generation. #8827 tracks the
  reconciliation burning attempts while it waits on its own producer.
- A `failed` execution with this `failure_class` is not a normal end of the
  wait. It means `ESHU_REDUCER_MAX_ATTEMPTS` is 1 or 2, so the queue
  dead-letters before the handler's cap of 3, or the error is a real one. Replay
  the class from [deadletter growth](#symptom-deadletter-growth).

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
    The `input_invalid`, `unsafe_payload`, `projection_bug`, and
    `resource_exhausted` classes are excluded from broad replays and return `422`
    on an explicit `failure_class` or `work_item_ids` target unless you set
    `force=true` after fixing the cause
    ([refusal contract](../reference/http-api/admin-replay-refusals.md)). A duplicate key returns the prior
    outcome (`duplicate=true`); a reused key with different parameters, or one
    whose replay is in progress, returns `409`. The CLI mirrors this:
    `eshu admin facts replay --reason "<why>" --failure-class timeout`. Every
    accepted or refused replay records an `admin_recovery_action` governance audit
    event carrying no secret values. See
    [Safe Replay Workflow](../reference/http-api/status-admin.md#safe-replay-workflow).
- **Own-gate dead letters (#7597):** a `deployable_unit_correlation` or
  `workload_materialization` dead letter in `*_producer_dead_lettered` or
  `*_producer_missing` waits on its scope generation's `deployment_mapping`
  producer. Replay or fix the producer first; its activation reopens these
  consumers on its own (`eshu_dp_reducer_own_gate_wake_total`). If the scope's
  relationship generation is already `active` and the consumer is still
  dead-lettered, replay the consumer with
  `"failure_class":"deployable_unit_correlation_producer_dead_lettered"` (or the
  matching `workload_materialization_*` or `*_producer_missing` class).

## Symptom: graph backend degradation

Queries are slow or erroring while the queue looks healthy.

- **Status endpoint:** `GET /readyz` for dependency readiness, then
  `GET /api/v0/status/pipeline` for the projection view.
- **Metric:** `eshu_dp_api_request_duration_seconds`,
  `eshu_dp_api_request_errors_total`, `eshu_dp_neo4j_query_duration_seconds`,
  and `eshu_dp_postgres_query_duration_seconds`; elevated read latency with a
  healthy queue points at the API handler or graph/storage backend, not the
  projection pipeline.
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

## Symptom: key sidecar tables keep growing

`relationship_reference_candidate_keys`, `package_manifest_consumption_keys`
or `package_registry_identity_keys` grows while retention prunes facts.

- **Census:** since #7420 no foreign key removes these rows with their facts;
  retention deletes them explicitly. Run the read-only census below
  (`OrphanCensusSQL` in `go/internal/storage/postgres/facts/keys/census.go`).
  It returns one row per table, scope and generation with orphan key rows and
  nothing on a healthy database. It scans `fact_records` once per table, so run
  it off-peak or on a replica.
- **Metric:** `eshu_dp_generation_retention_rows_pruned_total` with the three
  table values should move with the `fact_records` value while generations
  carry keys.
- **Log:** the reducer WARN `generation retention fact key delete differs
  from its pre-count` names a pruned batch whose key delete removed more or
  fewer rows than the batch counted (`fact_key_rows_pruned`,
  `fact_key_rows_counted`). The pre-count joins keys to their facts, so extra
  deleted rows were orphans. Run the census.
- **Rollout check:** if one release carries both #7420 changes (the explicit
  key deletes and migrations 188 to 190), a retention pod from the older image
  that still runs after `db-migrate` prunes generations without deleting their
  keys. Run the census once the rollout completes.
- **Safe remediation:** queries never read an orphan key row, so answers stay
  correct; the cost is storage. Run the cleanup below once per reported scope
  (`$1`) and generation (`$2`), rerun the census, then find the fact delete
  that skipped the key deletes. Do not shorten the cleanup to a `fact_id`-only
  anti-join: it misses keys that point at a fact of another generation.

```sql
SELECT 'relationship_reference_candidate_keys' AS table_name,
    keys.scope_id, keys.generation_id, count(*) AS orphan_rows
FROM relationship_reference_candidate_keys AS keys
WHERE NOT EXISTS (
    SELECT 1 FROM fact_records AS fact
    WHERE fact.fact_id = keys.fact_id AND fact.scope_id = keys.scope_id AND fact.generation_id = keys.generation_id
)
GROUP BY keys.scope_id, keys.generation_id
UNION ALL
SELECT 'package_manifest_consumption_keys', keys.scope_id, keys.generation_id, count(*)
FROM package_manifest_consumption_keys AS keys
WHERE NOT EXISTS (
    SELECT 1 FROM fact_records AS fact
    WHERE fact.fact_id = keys.fact_id AND fact.scope_id = keys.scope_id AND fact.generation_id = keys.generation_id
)
GROUP BY keys.scope_id, keys.generation_id
UNION ALL
SELECT 'package_registry_identity_keys', keys.scope_id, keys.generation_id, count(*)
FROM package_registry_identity_keys AS keys
WHERE NOT EXISTS (
    SELECT 1 FROM fact_records AS fact
    WHERE fact.fact_id = keys.fact_id AND fact.scope_id = keys.scope_id AND fact.generation_id = keys.generation_id
)
GROUP BY keys.scope_id, keys.generation_id
ORDER BY 1, 2, 3
```

```sql
WITH relationship AS (
    DELETE FROM relationship_reference_candidate_keys AS keys
    WHERE keys.scope_id = $1 AND keys.generation_id = $2
      AND NOT EXISTS (
          SELECT 1 FROM fact_records AS fact
          WHERE fact.fact_id = keys.fact_id AND fact.scope_id = keys.scope_id AND fact.generation_id = keys.generation_id
      )
    RETURNING 1
), manifest AS (
    DELETE FROM package_manifest_consumption_keys AS keys
    WHERE keys.scope_id = $1 AND keys.generation_id = $2
      AND NOT EXISTS (
          SELECT 1 FROM fact_records AS fact
          WHERE fact.fact_id = keys.fact_id AND fact.scope_id = keys.scope_id AND fact.generation_id = keys.generation_id
      )
    RETURNING 1
), registry AS (
    DELETE FROM package_registry_identity_keys AS keys
    WHERE keys.scope_id = $1 AND keys.generation_id = $2
      AND NOT EXISTS (
          SELECT 1 FROM fact_records AS fact
          WHERE fact.fact_id = keys.fact_id AND fact.scope_id = keys.scope_id AND fact.generation_id = keys.generation_id
      )
    RETURNING 1
)
SELECT (SELECT count(*) FROM relationship) AS relationship_reference_candidate_keys,
    (SELECT count(*) FROM manifest) AS package_manifest_consumption_keys,
    (SELECT count(*) FROM registry) AS package_registry_identity_keys
```

## Symptom: search index tables keep growing

`eshu_search_index_terms` or `eshu_search_index_documents` grows while
retention holds superseded generations for the fact-history window. Readers
join the scope's active generation, so a superseded generation's index rows
are read-invisible; they are derived from facts the hold keeps anyway.

- **Census:** run the attribution query
  (`docs/internal/evidence/7932-storage-attribution.sql`) on a replica with
  the deployed settings to confirm the bytes sit in superseded generations'
  index rows rather than live bloat.
- **Flag:** `ESHU_GENERATION_RETENTION_SEARCH_INDEX_SUPERSEDED_SWEEP_ENABLED`
  (bool, default `false`). With the switch off the sweep issues no SQL.
  Enablement is HELD until primary dead-tuple/bloat measurements for the
  terms and documents families are attached to #8231 — do not enable on
  row-count estimates alone.
- **Metric:** `eshu_dp_search_index_superseded_rows_swept_total{table}`
  moves per swept table while the sweep drains the backlog; phases
  `census_search_index_superseded` and `sweep_search_index_superseded` on
  `eshu_dp_generation_retention_phase_duration_seconds` time the listing and
  the batches.
- **Log:** one `generation retention search index superseded batch committed`
  line per committed batch (`scope_id`, `generation_id`, `table`,
  `batch_rows_deleted`); the per-run `generation retention search index
  superseded sweep completed` line carries `candidates`, `rows_swept`,
  `batches`, and `unswept_generations`. A backlog that does not fall across
  runs with rising `unswept_generations` means batches keep finding their
  generations ineligible — check the per-generation `... superseded
  generation swept` outcome (`ineligible` with live work or a moved active
  pointer; the sweep never touches those).
- **Safe remediation:** none needed for correctness — answers stay correct
  while the rows sit there; the cost is storage. To stop a running sweep,
  unset the flag and roll the reducer; committed batches stay deleted and
  later passes list nothing new. To restore a swept generation's index,
  re-run its search-document projection from the held facts (rebuild on
  demand, never eager). See [Generation Retention Search Index
  Pre-Delete](../reference/telemetry/generation-retention-search-index.md#superseded-sweep-flagged-8231).

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
