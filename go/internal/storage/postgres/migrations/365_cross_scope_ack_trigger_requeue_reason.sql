-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #11615: converge the 093 cross-scope ACK trigger to stamp the requeue
-- reason. When a succeeded ACK carries replay debt, the trigger returns the
-- row to pending for one rerun; the debt was scheduled (and counted) with
-- reason cross_scope_replay by the flagging writer, and the conversion now
-- records the same reason on the row so the rerun reads attributed. This
-- also backstops flags written by older binaries that did not stamp the
-- reason at schedule time.
--
-- The trigger WHEN list is unchanged from 093 (only the function body
-- converges), so the guarded-DROP/CREATE ceremony of migrations 112/120 is
-- not needed: CREATE OR REPLACE FUNCTION is re-runnable on every bootstrap
-- and converges once. The non-succeeded branch (retry, dead_letter,
-- failed) keeps the 093 behavior exactly: it clears the flag and returns
-- without touching the reason, because the Fail/retry path owns the row's
-- reason there (retry_backoff or readiness_wait). Shipped file 093 is never
-- edited. Keep the trigger-exists guard so a database that never ran 093
-- still converges.
CREATE OR REPLACE FUNCTION enforce_cross_scope_required_replay()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
	IF NEW.status <> 'succeeded' THEN
		NEW.cross_scope_replay_required := FALSE;
		RETURN NEW;
	END IF;
    NEW.status := 'pending';
    NEW.attempt_count := 0;
    NEW.container_image_identity_v2_authorized_status := CASE
        WHEN NEW.container_image_identity_v2_required THEN 'pending'
        ELSE ''
    END;
    NEW.container_image_identity_v3_authorized_status := CASE
        WHEN NEW.container_image_identity_v3_required THEN 'pending'
        ELSE ''
    END;
    NEW.lease_owner := NULL;
    NEW.claim_until := NULL;
    NEW.visible_at := clock_timestamp();
    NEW.next_attempt_at := NULL;
    NEW.updated_at := clock_timestamp();
    NEW.reopened_at := NEW.updated_at;
    NEW.last_requeue_reason := 'cross_scope_replay';
    NEW.cross_scope_replay_required := FALSE;
    NEW.failure_class := NULL;
    NEW.failure_message := NULL;
    NEW.failure_details := NULL;
    RETURN NEW;
END;
$$;

DO $trigger$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_trigger
        WHERE tgrelid = 'fact_work_items'::regclass
          AND tgname = 'fact_work_items_enforce_cross_scope_required_replay'
          AND NOT tgisinternal
    ) THEN
        CREATE TRIGGER fact_work_items_enforce_cross_scope_required_replay
        BEFORE UPDATE OF status ON fact_work_items
        FOR EACH ROW
        WHEN (
            OLD.cross_scope_replay_required
            AND OLD.stage = 'reducer'
            AND OLD.status IN ('claimed', 'running')
            AND NEW.status IN ('succeeded', 'retrying', 'dead_letter', 'failed')
        )
        EXECUTE FUNCTION enforce_cross_scope_required_replay();
    END IF;
END;
$trigger$;
