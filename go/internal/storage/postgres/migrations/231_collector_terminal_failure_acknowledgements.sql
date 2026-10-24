-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- Operator acknowledgements of one collector terminal failure (#8830).
--
-- One row per (collector_kind, collector_instance_id, scope_id): the failure
-- episode an operator marked "known, expected, stop reporting". The row
-- records WHICH failure was acknowledged (its bounded persisted class and the
-- updated_at of the failed_terminal work item the operator saw), never a
-- reference to the work item itself, so workflow_work_items retention (#8829)
-- can delete the failed row without touching the acknowledgement.
--
-- The collector status read suppresses a failed_terminal row only while all of
-- these hold: the row has the acknowledged class, it is not older than
-- failure_at, and no completed row of the same kind, instance, and scope is
-- newer than failure_at. A recovery (a later completed item) or a different
-- failure class therefore reports again with no write to this table.
--
-- The table carries no foreign key on purpose: it must outlive the retention
-- of the work items it refers to. A row for a removed instance is inert and
-- is bounded by the number of (instance, scope) pairs ever acknowledged.
CREATE TABLE IF NOT EXISTS collector_terminal_failure_acknowledgements (
    collector_kind TEXT NOT NULL,
    collector_instance_id TEXT NOT NULL,
    scope_id TEXT NOT NULL,
    failure_class TEXT NOT NULL DEFAULT '',
    failure_at TIMESTAMPTZ NOT NULL,
    reason TEXT NOT NULL CHECK (char_length(reason) BETWEEN 1 AND 512),
    actor_class TEXT NOT NULL,
    actor_id_hash TEXT NOT NULL DEFAULT '',
    acknowledged_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (collector_kind, collector_instance_id, scope_id)
);
