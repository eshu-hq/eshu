-- #4740: detect and bound-recover the dead-letter/poison class the #4727
-- projector claimer does not reach: a fact_work_items row whose status is
-- 'dead_letter' and whose scope has no strictly-newer generation. Such a row
-- is terminal (dead_letter is never claimable) and its scope will never
-- self-heal without an operator or this bounded liveness arm.
--
-- The stuck-gauge query (countPoisonDeadLettersQuery) and the bounded arm
-- (recoverPoisonDeadLettersQuery) both anchor on
-- "dead.status = 'dead_letter'" first, so a partial index on exactly that
-- predicate lets both read only the dead_letter subset instead of scanning
-- fact_work_items_status_idx's much larger (status, visible_at, updated_at)
-- prefix across every status. dead_letter is a terminal status (no further
-- writes touch it except this arm's own bounded re-drive), so the index is
-- low-churn: only inserts on new dead-letters and occasional deletes when the
-- arm successfully re-enqueues (status leaves 'dead_letter', dropping the row
-- from the partial index).
CREATE INDEX IF NOT EXISTS fact_work_items_dead_letter_poison_idx
    ON fact_work_items (scope_id, generation_id)
    WHERE status = 'dead_letter';
