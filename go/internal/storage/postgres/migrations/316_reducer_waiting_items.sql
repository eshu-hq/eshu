-- #10876: waiting-item set-aside for reducer work that is not ready.
--
-- A row here parks one claimed-then-deferred reducer item against the named
-- thing it waits for (waits_for_kind + waits_for_key). The queue row itself
-- stays 'retrying' in its existing non-counting readiness class with
-- visible_at as the 10-minute backstop (decision 4), so the claim ranking
-- never sees a new status and the attempt budget never moves: a wait is not
-- a try by construction, not by a new exemption.
--
-- A new table instead of a fact_work_items status keeps the hot queue table
-- and its claim queries unchanged (migration 110 already settled this shape
-- for the same hot table). A wait is M:N — one item waits on several keys,
-- many items wait on one key — and it must outlive per-generation queue
-- rows, so a status column on the queue row cannot express it.
--
-- waits_for_kind is a closed enum (decision 5a): relationship_generation_active,
-- phase_committed, generation_active, producer_domain_terminal,
-- winners_watermark, fence_clear, target_committed. waits_for_key is JSONB so
-- the #10877 corpus-fence narrowing can move from one global key to per-item
-- read sets without a schema change: a key that must name a SET of
-- repository or scope ids is a JSON array, never a second row shape.
--
-- Wake path: the providing save deletes the matching wait rows and reopens
-- the queue rows in the same transaction (shared helper, #7597 lock order:
-- claimant work row, providing row, waiting rows by work_item_id COLLATE "C"
-- with SET LOCAL lock_timeout, parked/dead rows SKIP LOCKED). The 10-minute
-- backstop plus the 5-minute audit reopen a wait whose wake was missed.
CREATE TABLE IF NOT EXISTS reducer_waiting_items (
    work_item_id TEXT NOT NULL,
    waits_for_kind TEXT NOT NULL,
    waits_for_key JSONB NOT NULL,
    scope_id TEXT NOT NULL,
    generation_id TEXT NOT NULL,
    domain TEXT NOT NULL,
    failure_class TEXT NOT NULL,
    first_deferred_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    row_version BIGINT NOT NULL DEFAULT 0,
    CONSTRAINT reducer_waiting_items_pkey PRIMARY KEY (work_item_id, waits_for_kind, waits_for_key),
    CONSTRAINT reducer_waiting_items_kind_check CHECK (waits_for_kind IN (
        'relationship_generation_active',
        'phase_committed',
        'generation_active',
        'producer_domain_terminal',
        'winners_watermark',
        'fence_clear',
        'target_committed'
    )),
    CONSTRAINT reducer_waiting_items_row_version_check CHECK (row_version >= 0)
);
CREATE INDEX IF NOT EXISTS reducer_waiting_items_wake_idx
    ON reducer_waiting_items (waits_for_kind, waits_for_key);
CREATE INDEX IF NOT EXISTS reducer_waiting_items_reason_idx
    ON reducer_waiting_items (failure_class, first_deferred_at);
