ALTER TABLE fact_work_items
    ADD COLUMN IF NOT EXISTS provenance_edge_identity_upgrade_required
        BOOLEAN NOT NULL DEFAULT FALSE;

-- Keep the capability requirement attached to every replayable provenance
-- producer, including rows inserted or reopened by an old pod after the
-- pre-upgrade schema hook has completed.
CREATE OR REPLACE FUNCTION require_provenance_edge_identity_upgrade()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.stage = 'reducer'
        AND NEW.domain IN ('package_source_correlation', 'container_image_identity')
        AND NEW.status IN ('pending', 'retrying', 'claimed', 'running') THEN
        NEW.provenance_edge_identity_upgrade_required := TRUE;
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS fact_work_items_require_provenance_edge_identity_insert
    ON fact_work_items;
CREATE TRIGGER fact_work_items_require_provenance_edge_identity_insert
BEFORE INSERT ON fact_work_items
FOR EACH ROW
WHEN (
    NEW.stage = 'reducer'
    AND NEW.domain IN ('package_source_correlation', 'container_image_identity')
    AND NEW.status IN ('pending', 'retrying', 'claimed', 'running')
)
EXECUTE FUNCTION require_provenance_edge_identity_upgrade();

DROP TRIGGER IF EXISTS fact_work_items_require_provenance_edge_identity_update
    ON fact_work_items;
CREATE TRIGGER fact_work_items_require_provenance_edge_identity_update
BEFORE UPDATE OF status, domain, stage ON fact_work_items
FOR EACH ROW
WHEN (
    NEW.stage = 'reducer'
    AND NEW.domain IN ('package_source_correlation', 'container_image_identity')
    AND NEW.status IN ('pending', 'retrying', 'claimed', 'running')
    AND NOT OLD.provenance_edge_identity_upgrade_required
)
EXECUTE FUNCTION require_provenance_edge_identity_upgrade();

-- Old reducers running after a pre-upgrade schema hook do not know how to
-- clear the capability flag. Fence their terminal transition back to pending.
-- The new reducer clears the flag in the same statement that marks the work
-- succeeded or dead-lettered; retries retain it until a terminal transition.
CREATE OR REPLACE FUNCTION enforce_provenance_edge_identity_upgrade()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    NEW.status := 'pending';
    NEW.attempt_count := 0;
    NEW.container_image_identity_v2_authorized_status := CASE
        WHEN NEW.container_image_identity_v2_required THEN 'pending' ELSE '' END;
    NEW.container_image_identity_v3_authorized_status := CASE
        WHEN NEW.container_image_identity_v3_required THEN 'pending' ELSE '' END;
    NEW.lease_owner := NULL;
    NEW.claim_until := NULL;
    NEW.visible_at := clock_timestamp();
    NEW.next_attempt_at := NULL;
    NEW.updated_at := clock_timestamp();
    NEW.reopened_at := NEW.updated_at;
    NEW.failure_class := NULL;
    NEW.failure_message := NULL;
    NEW.failure_details := NULL;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS fact_work_items_enforce_provenance_edge_identity_upgrade
    ON fact_work_items;
CREATE TRIGGER fact_work_items_enforce_provenance_edge_identity_upgrade
BEFORE UPDATE OF status ON fact_work_items
FOR EACH ROW
WHEN (
    OLD.provenance_edge_identity_upgrade_required
    AND NEW.provenance_edge_identity_upgrade_required
    AND OLD.stage = 'reducer'
    AND OLD.status IN ('claimed', 'running')
    AND NEW.status IN ('succeeded', 'failed', 'dead_letter')
)
EXECUTE FUNCTION enforce_provenance_edge_identity_upgrade();

-- Existing succeeded provenance writers produced endpoint-only relationship
-- identities before #5827. Reopen the current active work once so each
-- handler runs its normal scope/evidence retract and rebuilds the surviving
-- row set with scope_id+evidence_source in the MERGE identity.
--
-- The marker and replay seed share one statement: a failed bootstrap rolls
-- both back, while later successful bootstraps are no-ops. In-flight old
-- reducers are dirtied through cross_scope_replay_required so their ACK must
-- return to pending once under the new writer instead of leaving a collapsed
-- relationship behind during a rolling application upgrade.
WITH upgrade_marker AS MATERIALIZED (
    INSERT INTO cross_scope_completion_upgrade_markers (marker_name, applied_at)
    VALUES ('provenance_edge_identity_upgrade_096', clock_timestamp())
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
    provenance_edge_identity_upgrade_required = TRUE,
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
  AND source.domain IN ('package_source_correlation', 'container_image_identity')
  AND source.status IN ('pending', 'retrying', 'succeeded', 'claimed', 'running');
