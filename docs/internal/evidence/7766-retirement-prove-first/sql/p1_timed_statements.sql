-- #7766 P1: the design-form statements the driver ran, in order. These carry
-- bind parameters ($1..$3), so this file documents the statements and is not
-- runnable through psql as written. $1 is the scope id array; the pair form
-- unnests (scope_id, generation_id) arrays.

-- step 4 read, step 5 lock (outside the timed window)
SELECT scope_id, generation_id FROM scope_generations WHERE scope_id = ANY($1) ORDER BY scope_id, generation_id;
SELECT scope_id FROM ingestion_scopes WHERE partition_key = ANY($1) ORDER BY scope_id FOR NO KEY UPDATE;

-- timed window
LOCK TABLE fact_work_items IN EXCLUSIVE MODE;
-- recheck of live reducer leases (tuned form: w.scope_id = ANY($1))
SELECT COUNT(*) FROM fact_work_items AS w WHERE w.stage = 'reducer' AND w.status IN ('claimed','running') AND w.claim_until > clock_timestamp() AND (w.scope_id, w.generation_id) IN (SELECT * FROM unnest($1::text[], $2::text[]) AS affected(scope_id, generation_id));
-- 7a
UPDATE scope_generations SET status='superseded', superseded_at=$2 WHERE scope_id = ANY($1) AND status IN ('pending','active','failed');
-- 7b
UPDATE ingestion_scopes SET active_generation_id = NULL WHERE scope_id = ANY($1);
-- 7c (pair form; tuned binds only the non-superseded pairs)
UPDATE fact_work_items SET status='superseded', failure_class='repository_retired', lease_owner=NULL, claim_until=NULL, visible_at=NULL, next_attempt_at=NULL, updated_at=$3 WHERE stage='projector' AND (scope_id, generation_id) IN (SELECT * FROM unnest($1::text[], $2::text[]) AS affected(scope_id, generation_id)) AND (status IN ('pending','retrying') OR (status IN ('claimed','running') AND claim_until <= clock_timestamp()));
-- 7d (pair form; tuned: scope_id = ANY($1)); a DELETE in the proven form
DELETE FROM fact_work_items WHERE stage='reducer' AND (scope_id, generation_id) IN (SELECT * FROM unnest($1::text[], $2::text[]) AS affected(scope_id, generation_id)) AND status IN ('pending','retrying','failed','dead_letter','claimed','running') AND NOT (status IN ('claimed','running') AND claim_until > clock_timestamp());
-- 7e, 7f (design form only), 7g
DELETE FROM repository_reindex_requests WHERE scope_id = ANY($1);
SELECT max(lease_expires_at) FROM shared_projection_partition_leases WHERE lease_owner IS NOT NULL AND lease_expires_at > clock_timestamp();
-- 7g: INSERT INTO repository_retirements (...) SELECT ... FROM unnest($1::text[], $2::text[]) ON CONFLICT (repo_id) WHERE readmitted_at IS NULL DO NOTHING
