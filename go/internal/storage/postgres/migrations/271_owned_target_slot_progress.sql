-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #9776: persisted progress for least-recently-planned owned-package slot
-- selection. One row per (collector instance, derivation kind, virtual slot).
-- Virtual slots are the pinned V = 1024 modulus of the OSV slot scopes, so a
-- row survives a change of the reader's hash-slot count P (P divides V while
-- P <= V). The coordinator writes a row by compare-and-set inside the
-- admission transaction, under the per-instance advisory lock; the table holds
-- O(instances x V) rows regardless of the target total. A missing row orders
-- as the oldest slot, so an empty table starts the rotation at slot 0 and a
-- deleted row costs one repeat window, never a missed target. There is no
-- seed, so the file is DDL only and safe to re-apply.
--
-- last_planned_window_start is the start of the reconcile window the slot was
-- planned in, as whole seconds since the Unix epoch. It is a time, not a count
-- of scan intervals, so it means the same thing after an operator raises or
-- lowers scan_interval: rows stay ordered by when they were planned, a new key
-- is below an old one by at most one window after a raise, and the age of the
-- oldest slot is a plain difference of two times.
CREATE TABLE IF NOT EXISTS owned_target_slot_progress (
    collector_instance_id TEXT NOT NULL,
    derivation_kind TEXT NOT NULL,
    virtual_slot INTEGER NOT NULL CHECK (virtual_slot >= 0),
    last_planned_window_start BIGINT NOT NULL,
    last_planned_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (collector_instance_id, derivation_kind, virtual_slot)
);

-- The oldest-slot read orders by (last_planned_window_start, virtual_slot)
-- within one instance and kind; this is the index the #9776 shim measured at 0.023 ms.
CREATE INDEX IF NOT EXISTS owned_target_slot_progress_oldest_idx
    ON owned_target_slot_progress (
        collector_instance_id,
        derivation_kind,
        last_planned_window_start,
        virtual_slot
    );

REVOKE ALL ON owned_target_slot_progress FROM PUBLIC;
