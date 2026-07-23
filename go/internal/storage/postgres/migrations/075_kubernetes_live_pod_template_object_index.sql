-- #5639 codex P1: the declared-object live-evidence anchor queries in
-- impact_trace_deployment_live_evidence_store_declared.go filter
-- fact_records on payload->>'group_version_resource',
-- payload->>'namespace', payload->>'name' for
-- fact_kind = 'kubernetes_live.pod_template' with no supporting index, so
-- the planner falls back to fact_records_scope_generation_idx and filters
-- every active pod-template fact per scope in memory. This partial
-- expression index gives that predicate a direct index scan.
CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_kubernetes_live_pod_template_object_idx
    ON fact_records (
        (payload->>'group_version_resource'),
        (payload->>'namespace'),
        (payload->>'name')
    )
    WHERE fact_kind = 'kubernetes_live.pod_template'
      AND is_tombstone = FALSE;
