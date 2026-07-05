# operator-dashboard-metrics.sh — eshu_dp_* metric name registry for
# scripts/generate-operator-dashboard.sh. Sourced by the generator
# (and its test mirror) so the headline panel list stays in lockstep
# with the X1 telemetry-coverage contract at
# docs/public/observability/telemetry-coverage.md and the metric
# registrations at go/internal/telemetry/instruments.go. When any
# metric here changes name, update the variable in this file and
# regenerate the dashboard.

# Generation liveness (the alarm row).
ACTIVE_GENERATIONS='eshu_dp_active_generations'
GENERATION_LIVENESS_FAILURES='eshu_dp_generation_liveness_failures_total'
GENERATION_LIVENESS_RECOVERED='eshu_dp_generation_liveness_recovered_total'
GENERATION_LIVENESS_SUPERSEDED='eshu_dp_generation_liveness_superseded_total'

# Queue and worker pool.
QUEUE_DEPTH='eshu_dp_queue_depth'
QUEUE_OLDEST_AGE='eshu_dp_queue_oldest_age_seconds'
WORKER_POOL_ACTIVE='eshu_dp_worker_pool_active'

# Graph and shared-projection state.
SHARED_ACCEPTANCE_ROWS='eshu_dp_shared_acceptance_rows'
GRAPH_ORPHAN_NODES='eshu_dp_graph_orphan_nodes'
CROSS_REPO_FENCED='eshu_dp_cross_repo_activation_fenced_total'

# Typed-payload decode accuracy. A non-zero rate means the reducer skipped
# facts whose payload was missing a required identity field (input_invalid);
# the graph is under-projecting for that domain/fact_kind until the collector
# defect is fixed. A sustained spike is an accuracy alarm, not routine noise.
REDUCER_INPUT_INVALID_FACTS='eshu_dp_reducer_input_invalid_facts_total'

# Projector-side typed-payload decode accuracy. Same accuracy signal as the
# reducer counter above, but for the projector's canonical extractors (stage
# label, e.g. oci_registry_canonical): a non-zero rate means a canonical
# extractor skipped a fact whose payload was missing a required identity field,
# so the graph is under-projecting for that stage/fact_kind until the collector
# defect is fixed.
PROJECTOR_INPUT_INVALID_FACTS='eshu_dp_projector_input_invalid_facts_total'

# Extraction-provenance drift.
EDGES_BY_SOURCE_TOOL='eshu_dp_edges_by_source_tool'
FILES_BY_LANGUAGE='eshu_dp_files_by_language'

# API surface.
API_REQUEST_DURATION='eshu_dp_api_request_duration_seconds_bucket'
API_REQUEST_ERRORS='eshu_dp_api_request_errors_total'

# Per-collector backpressure, retry, and reconciliation.
COLLECTOR_RECONCILIATION_FULL='eshu_dp_collector_reconciliation_full_snapshots_total'
COLLECTOR_RECONCILIATION_DRIFT='eshu_dp_reconciliation_drift_retractions_total'
COLLECTOR_RECONCILIATION_CONVERGENCE='eshu_dp_reconciliation_convergence_total'
COLLECTOR_BACKPRESSURE='eshu_dp_workflow_claim_provider_throttle_total'
COLLECTOR_RETRIES='eshu_dp_workflow_claim_retries_total'
COLLECTOR_DEAD_LETTER='eshu_dp_workflow_claim_attempt_budget_exhausted_total'
