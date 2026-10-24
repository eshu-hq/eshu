-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- Operator acknowledgements of one collector terminal failure (#8830).
--
-- One row per (collector_kind, collector_instance_id, scope_id): the failure
-- episode an operator marked "known, expected, stop reporting". The row records
-- WHICH failure was acknowledged: the newest unresolved failed_terminal row R at
-- the time of the call, as failure_at = R.updated_at, work_item_id =
-- R.work_item_id, and failure_class = R.last_failure_class (raw, '' for NULL).
--
-- The collector status read suppresses a failed_terminal row t of the same
-- target while t.updated_at <= failure_at, or while t has the acknowledged raw
-- class and no completed row of the target landed between failure_at and
-- t.updated_at. A recovery (a completed item, then a failure) or a different
-- raw class therefore reports again with no write to this table.
--
-- The table carries no foreign key on purpose: work_item_id names the
-- acknowledged row for audit and display only, because workflow work-item
-- retention (#8829) deletes work items and must never be blocked by, or cascade
-- into, an acknowledgement. A row for a removed instance or purged scope is
-- inert and bounded by the number of (instance, scope) pairs ever acknowledged.
-- There is no expiry.
CREATE TABLE IF NOT EXISTS collector_terminal_failure_acknowledgements (
    collector_kind TEXT NOT NULL,
    collector_instance_id TEXT NOT NULL,
    scope_id TEXT NOT NULL,
    failure_class TEXT NOT NULL DEFAULT '',
    failure_at TIMESTAMPTZ NOT NULL,
    work_item_id TEXT NOT NULL,
    reason TEXT NOT NULL CHECK (char_length(reason) BETWEEN 1 AND 512),
    actor_class TEXT NOT NULL,
    actor_id_hash TEXT NOT NULL DEFAULT '',
    correlation_id TEXT NULL,
    acknowledged_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (collector_kind, collector_instance_id, scope_id)
);
