-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #11018 P1: audit clock for the #10878 changed-evidence gate.
--
-- last_audit_at is the completion time of the last full
-- aws_cloud_runtime_drift run (audit_due, fallback, or first run — each is
-- a complete re-derivation including config, so each satisfies the daily
-- audit purpose). NULL means never audited, which the planner and the
-- reopen listing treat as audit-due. The record path advances it on full
-- runs and preserves it on deltas under the same claimed_at fence, so
-- hourly deltas never defer the daily audit.
--
-- A new guarded migration, not an edit of 317: 317 shipped with the #11018
-- merge, so #7002 forbids amending it. ADD COLUMN nullable with no default
-- is metadata-only (no table rewrite, no backfill): the column reads NULL
-- on existing rows, which is exactly the never-audited state.
ALTER TABLE IF EXISTS aws_drift_reopen_fingerprints
    ADD COLUMN IF NOT EXISTS last_audit_at TIMESTAMPTZ;
