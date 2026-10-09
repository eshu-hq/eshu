-- #7766 P1 fleet pause: the three claim-shaped proxy statements that ran
-- against unrelated scopes while the phase 1 section ran. They carry bind
-- parameters, so this file documents them and is not runnable as written.

-- claim-shaped (three workers)
UPDATE fact_work_items SET status='claimed', lease_owner='proof-claimer', claim_until=now()+interval '10 minutes', attempt_count=attempt_count+1, updated_at=now() WHERE work_item_id=$1 AND status='pending';
-- heartbeat-shaped (two workers)
UPDATE fact_work_items SET claim_until = now()+interval '30 days', updated_at=now() WHERE work_item_id=$1;
-- enqueue-shaped (one worker)
INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, status, visible_at, created_at, updated_at) SELECT $1, g.scope_id, g.generation_id, 'projector', 'source_local', 'succeeded', NULL, now(), now() FROM scope_generations g WHERE g.generation_id=$2;
