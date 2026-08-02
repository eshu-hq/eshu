-- Existing succeeded producers predate the completion queue and therefore have
-- no ACK event to drive a quiet deployment forward. Reopen each current base
-- producer exactly once through the canonical work item. The one-time marker is
-- inserted in the same statement, so failure rolls back both the marker and the
-- replay seed; repeated bootstrap is a no-op after a successful seed.
WITH upgrade_marker AS MATERIALIZED (
    INSERT INTO cross_scope_completion_upgrade_markers (marker_name, applied_at)
    VALUES ('cross_scope_completion_upgrade_095', clock_timestamp())
    ON CONFLICT (marker_name) DO NOTHING
    RETURNING marker_name
)
UPDATE fact_work_items AS source
SET status = CASE
        WHEN source.status = 'succeeded' THEN 'pending'
        ELSE source.status
    END,
    attempt_count = CASE
        WHEN source.status = 'succeeded' THEN 0
        ELSE source.attempt_count
    END,
    container_image_identity_v2_authorized_status = CASE
        WHEN source.status = 'succeeded' AND source.container_image_identity_v2_required
            THEN 'pending'
        ELSE source.container_image_identity_v2_authorized_status
    END,
    container_image_identity_v3_authorized_status = CASE
        WHEN source.status = 'succeeded' AND source.container_image_identity_v3_required
            THEN 'pending'
        ELSE source.container_image_identity_v3_authorized_status
    END,
    lease_owner = CASE WHEN source.status = 'succeeded' THEN NULL ELSE source.lease_owner END,
    claim_until = CASE WHEN source.status = 'succeeded' THEN NULL ELSE source.claim_until END,
    visible_at = CASE
        WHEN source.status = 'succeeded' THEN clock_timestamp()
        ELSE source.visible_at
    END,
    next_attempt_at = CASE
        WHEN source.status = 'succeeded' THEN NULL
        ELSE source.next_attempt_at
    END,
    updated_at = CASE
        WHEN source.status = 'succeeded' THEN clock_timestamp()
        ELSE source.updated_at
    END,
    reopened_at = CASE
        WHEN source.status = 'succeeded' THEN clock_timestamp()
        ELSE source.reopened_at
    END,
    cross_scope_replay_required = source.status IN ('claimed', 'running'),
    failure_class = CASE WHEN source.status = 'succeeded' THEN NULL ELSE source.failure_class END,
    failure_message = CASE WHEN source.status = 'succeeded' THEN NULL ELSE source.failure_message END,
    failure_details = CASE WHEN source.status = 'succeeded' THEN NULL ELSE source.failure_details END
FROM ingestion_scopes AS scope,
     scope_generations AS generation,
     upgrade_marker
WHERE scope.scope_id = source.scope_id
  AND scope.active_generation_id = source.generation_id
  AND generation.scope_id = source.scope_id
  AND generation.generation_id = source.generation_id
  AND generation.status = 'active'
  AND source.stage = 'reducer'
  AND source.domain IN ('container_image_identity', 'ci_cd_run_correlation')
  AND source.status IN ('succeeded', 'claimed', 'running');
