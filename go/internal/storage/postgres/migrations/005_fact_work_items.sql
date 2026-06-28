CREATE TABLE IF NOT EXISTS fact_work_items (
    work_item_id TEXT PRIMARY KEY,
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    stage TEXT NOT NULL,
    domain TEXT NOT NULL,
    conflict_domain TEXT NOT NULL DEFAULT 'scope',
    conflict_key TEXT NULL,
    status TEXT NOT NULL,
    attempt_count INTEGER NOT NULL DEFAULT 0,
    lease_owner TEXT NULL,
    claim_until TIMESTAMPTZ NULL,
    visible_at TIMESTAMPTZ NULL,
    last_attempt_at TIMESTAMPTZ NULL,
    next_attempt_at TIMESTAMPTZ NULL,
    failure_class TEXT NULL,
    failure_message TEXT NULL,
    failure_details TEXT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

ALTER TABLE fact_work_items
    ADD COLUMN IF NOT EXISTS conflict_domain TEXT NOT NULL DEFAULT 'scope';

ALTER TABLE fact_work_items
    ADD COLUMN IF NOT EXISTS conflict_key TEXT NULL;

CREATE INDEX IF NOT EXISTS fact_work_items_scope_generation_idx
    ON fact_work_items (scope_id, generation_id, status, updated_at DESC);

CREATE INDEX IF NOT EXISTS fact_work_items_status_idx
    ON fact_work_items (status, visible_at, updated_at DESC);

CREATE INDEX IF NOT EXISTS fact_work_items_stage_domain_status_idx
    ON fact_work_items (stage, domain, status, visible_at, updated_at DESC);

CREATE INDEX IF NOT EXISTS fact_work_items_claim_until_idx
    ON fact_work_items (claim_until)
    WHERE claim_until IS NOT NULL;

CREATE INDEX IF NOT EXISTS fact_work_items_reducer_conflict_claim_idx
    ON fact_work_items (stage, conflict_domain, COALESCE(conflict_key, scope_id), status, claim_until, updated_at DESC)
    WHERE stage = 'reducer';

CREATE INDEX IF NOT EXISTS fact_work_items_reducer_source_claim_idx
    ON fact_work_items (
        COALESCE(NULLIF(BTRIM(payload->>'source_system'), ''), 'unknown'),
        domain,
        status,
        visible_at,
        claim_until,
        updated_at,
        work_item_id
    )
    WHERE stage = 'reducer';

-- At most one live reducer lease per conflict key (#4137, completing #3558).
-- The claim query's NOT EXISTS conflict fence defers a sibling only once a
-- holder's claim has COMMITTED, but under READ COMMITTED two genuinely
-- simultaneous single-claim workers can each pick a DIFFERENT pending sibling
-- row before either commits (SKIP LOCKED locks the distinct rows), so both would
-- claim. This partial unique index makes Postgres reject the second concurrent
-- live lease on a conflict key; the claim path translates the resulting
-- unique_violation into a deferred no-op claim. The batch claim path needs no
-- equivalent — it already serializes on one representative row per conflict key.
--
-- Resolve any pre-existing duplicate live leases (only reachable via the race
-- this index closes) before enforcing uniqueness, so the migration is safe to
-- apply to a live deployment: losers return to pending and re-claim one at a
-- time. The winner per key is the smallest work_item_id. Idempotent — once the
-- unique index holds, at most one live lease per key exists, so this resets
-- nothing on later applies.
UPDATE fact_work_items AS loser
SET status = 'pending', lease_owner = NULL, claim_until = NULL
WHERE loser.stage = 'reducer'
  AND loser.status IN ('claimed', 'running')
  AND EXISTS (
      SELECT 1
      FROM fact_work_items AS winner
      WHERE winner.stage = 'reducer'
        AND winner.status IN ('claimed', 'running')
        AND winner.conflict_domain = loser.conflict_domain
        AND COALESCE(winner.conflict_key, winner.scope_id) = COALESCE(loser.conflict_key, loser.scope_id)
        AND winner.work_item_id < loser.work_item_id
  );

CREATE UNIQUE INDEX IF NOT EXISTS fact_work_items_reducer_live_lease_uniq
    ON fact_work_items (conflict_domain, COALESCE(conflict_key, scope_id))
    WHERE stage = 'reducer' AND status IN ('claimed', 'running');
