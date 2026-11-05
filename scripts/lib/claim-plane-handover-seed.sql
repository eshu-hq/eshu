-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- Token high-water seed for the claim-plane cutover handover (#9376).
-- Run against the SATELLITE database. The caller creates the TEMP table
-- first (CREATE TEMP TABLE handover_snapshot(work_item_id TEXT PRIMARY
-- KEY, h BIGINT NOT NULL)), loads the snapshot rows into it, then runs
-- this file in the same session. Each overlapping item advances to
-- max(snapshot H, satellite H) and never goes back: re-seeding an older
-- snapshot cannot lower a token. Items in the snapshot but absent from the
-- satellite are reported, not invented: the verify step fails on them so
-- the operator resolves unmigrated items before the flip.
UPDATE workflow_work_items AS item
   SET current_fencing_token = GREATEST(item.current_fencing_token, snap.h),
       updated_at = now()
  FROM handover_snapshot AS snap
 WHERE item.work_item_id = snap.work_item_id
   AND snap.h > item.current_fencing_token;

-- Report: seeded rows carry the snapshot H or higher; the second row must
-- be zero or verify refuses the flip.
SELECT count(*)
  FROM workflow_work_items AS item
  JOIN handover_snapshot AS snap
    ON snap.work_item_id = item.work_item_id
 WHERE item.current_fencing_token >= snap.h;

SELECT snap.work_item_id
  FROM handover_snapshot AS snap
  LEFT JOIN workflow_work_items AS item
    ON item.work_item_id = snap.work_item_id
 WHERE item.work_item_id IS NULL
 ORDER BY snap.work_item_id;
