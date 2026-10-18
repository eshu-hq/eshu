-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #8371 P2: the deployable_unit_correlation gap reconciliation lists the
-- latest decision per repository anchor every maintenance pass
-- (backfill.ListAllDeployableUnitAdmittedAnchorReposQuery: DISTINCT ON
-- (anchor_id) ... ORDER BY anchor_id, decided_at DESC over
-- domain = 'deployable_unit_correlation' AND anchor_kind = 'repository').
-- Both existing indexes lead with (scope_id, generation_id)
-- (admission_decisions_scope_generation_domain_idx,
-- admission_decisions_anchor_idx), so neither serves that per-pass listing:
-- every pass seq-scans plus DISTINCT-sorts all retained admission history
-- until generation retention prunes it.
--
-- Partial and anchor-leading, shaped to the P1 predicate (domain +
-- anchor_kind only: the inner query scopes to the latest decision per
-- anchor regardless of state, and the admitted filter applies outside), with
-- decided_at following so the bitmap-fed sort stays small (EXPLAIN: one
-- Bitmap Index Scan, 18.9 ms at 120k rows). Only matching rows pay
-- maintenance; the predicate matches a small deployable-unit slice of
-- admission history.
--
-- CONCURRENTLY so bootstrap never blocks writers; IF NOT EXISTS for rerun
-- safety, matching the 073/086/118/121/152/155/159 precedent. One statement
-- per file so the migration coordinator can run it outside a transaction
-- (coordination.IsSoleConcurrentIndexStatement). A NEW migration file per
-- issue #7002: never edit an applied migration.
CREATE INDEX CONCURRENTLY IF NOT EXISTS deployable_unit_gap_admitted_anchor_idx
    ON admission_decisions (anchor_id, decided_at DESC)
    WHERE domain = 'deployable_unit_correlation'
      AND anchor_kind = 'repository';
