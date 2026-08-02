-- Acquire the first-upgrade lock before creating any completion object. The
-- steady-state bootstrap path skips this hot-table lock once both columns are
-- present.
DO $upgrade_lock$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM (VALUES
            ('cross_scope_replay_required'),
            ('cross_scope_completion_ack_epoch')
        ) AS required(attname)
        WHERE NOT EXISTS (
            SELECT 1
            FROM pg_attribute
            WHERE attrelid = 'fact_work_items'::regclass
              AND pg_attribute.attname = required.attname
              AND NOT attisdropped
        )
    ) THEN
        LOCK TABLE fact_work_items IN ACCESS EXCLUSIVE MODE;
    END IF;
END;
$upgrade_lock$;

CREATE TABLE IF NOT EXISTS cross_scope_completion_events (
    event_id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    producer_domain TEXT NOT NULL,
    producer_item_count BIGINT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    attempt_count INTEGER NOT NULL DEFAULT 0,
    lease_owner TEXT NULL,
    claim_until TIMESTAMPTZ NULL,
    visible_at TIMESTAMPTZ NULL,
    claim_epoch BIGINT NOT NULL DEFAULT 0,
    failure_message TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT cross_scope_completion_events_producer_domain_check
        CHECK (producer_domain IN ('ci_cd_run_correlation', 'container_image_identity')),
    CONSTRAINT cross_scope_completion_events_status_check
        CHECK (status IN ('pending', 'claimed', 'running', 'retrying')),
    CONSTRAINT cross_scope_completion_events_items_check
        CHECK (producer_item_count > 0),
    CONSTRAINT cross_scope_completion_events_lease_shape_check CHECK (
        (
            status IN ('pending', 'retrying')
            AND lease_owner IS NULL
            AND claim_until IS NULL
        ) OR (
            status IN ('claimed', 'running')
            AND NULLIF(BTRIM(lease_owner), '') IS NOT NULL
            AND claim_until IS NOT NULL
        )
    )
);

CREATE INDEX IF NOT EXISTS cross_scope_completion_events_pending_idx
    ON cross_scope_completion_events (
        producer_domain, status, visible_at, event_id
    )
    WHERE status IN ('pending', 'claimed', 'running', 'retrying');

CREATE UNIQUE INDEX IF NOT EXISTS cross_scope_completion_events_queued_domain_uniq
    ON cross_scope_completion_events (producer_domain)
    WHERE status IN ('pending', 'retrying');

CREATE UNIQUE INDEX IF NOT EXISTS cross_scope_completion_events_live_domain_uniq
    ON cross_scope_completion_events (producer_domain)
    WHERE status IN ('claimed', 'running');

DO $columns$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_attribute
        WHERE attrelid = 'fact_work_items'::regclass
          AND attname = 'cross_scope_replay_required'
          AND NOT attisdropped
    ) THEN
        ALTER TABLE fact_work_items
            ADD COLUMN cross_scope_replay_required
                BOOLEAN NOT NULL DEFAULT FALSE;
    END IF;
    IF NOT EXISTS (
        SELECT 1
        FROM pg_attribute
        WHERE attrelid = 'fact_work_items'::regclass
          AND attname = 'cross_scope_completion_ack_epoch'
          AND NOT attisdropped
    ) THEN
        ALTER TABLE fact_work_items
            ADD COLUMN cross_scope_completion_ack_epoch
                BIGINT NOT NULL DEFAULT 0;
    END IF;
END;
$columns$;

CREATE TABLE IF NOT EXISTS cross_scope_completion_upgrade_markers (
    marker_name TEXT PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL
);

-- A rolling old reducer does not know about cross_scope_replay_required. Fence
-- its successful ACK in the database: an in-flight consumer dirtied by fanout
-- must return to pending once before it may become succeeded. A retry or
-- dead-letter already leaves the old execution path and is cleared by the
-- normal queue mutation instead.
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

-- Rolling upgrades can run an older reducer after this schema lands. New
-- reducers aggregate their batch in the ACK statement and increment an ACK
-- epoch; this narrow row trigger emits only when an old producer ACK preserves
-- that epoch.
-- Unrelated fact_work_items updates do not materialize transition tables or run
-- this function. The queued row remains a per-domain dirty bit with an audit
-- count, a 250 ms quiet window, and a two-second maximum flush deadline.
CREATE OR REPLACE FUNCTION enqueue_cross_scope_completion_event()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    emitted_at TIMESTAMPTZ := clock_timestamp();
BEGIN
    INSERT INTO cross_scope_completion_events (
        producer_domain, producer_item_count, status,
        visible_at, created_at, updated_at
    )
    VALUES (
        NEW.domain, 1, 'pending',
        emitted_at + INTERVAL '250 milliseconds', emitted_at, emitted_at
    )
    ON CONFLICT (producer_domain) WHERE status IN ('pending', 'retrying') DO UPDATE SET
        producer_item_count =
            cross_scope_completion_events.producer_item_count +
            EXCLUDED.producer_item_count,
        visible_at = CASE
            WHEN cross_scope_completion_events.status = 'retrying'
                THEN cross_scope_completion_events.visible_at
            ELSE LEAST(
                cross_scope_completion_events.created_at + INTERVAL '2 seconds',
                EXCLUDED.visible_at
            )
        END,
        updated_at = EXCLUDED.updated_at;
    RETURN NEW;
END;
$$;

DO $trigger$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_trigger
        WHERE tgrelid = 'fact_work_items'::regclass
          AND tgname = 'fact_work_items_cross_scope_completion'
          AND NOT tgisinternal
    ) THEN
        CREATE TRIGGER fact_work_items_cross_scope_completion
        AFTER UPDATE OF status ON fact_work_items
        FOR EACH ROW
        WHEN (
            OLD.stage = 'reducer'
            AND NEW.stage = 'reducer'
            AND OLD.domain = NEW.domain
            AND NEW.domain IN ('ci_cd_run_correlation', 'container_image_identity')
            AND OLD.status IN ('claimed', 'running')
            AND NEW.status = 'succeeded'
            AND OLD.cross_scope_completion_ack_epoch = NEW.cross_scope_completion_ack_epoch
        )
        EXECUTE FUNCTION enqueue_cross_scope_completion_event();
    END IF;
END;
$trigger$;
