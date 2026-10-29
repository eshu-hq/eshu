// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queuestore

// ReopenedAtUpsertValue is the reopened_at value (the right-hand side of
// "reopened_at = ") of every ON CONFLICT (work_item_id) DO UPDATE that
// re-drives a fact_work_items row to pending: the reconcile re-projection, the
// generation-liveness re-drive, the zombie heal, and the refinalize re-enqueue
// (#9320).
//
// It stamps the upsert's own timestamp (EXCLUDED.updated_at, the writer's
// "now") only when the conflicting row had finished (succeeded, failed,
// dead_letter, or superseded) and so becomes outstanding again. A row that is
// still pending, retrying, claimed, or running keeps its reopened_at: it was
// already outstanding, and an expired claimed or running row that a sweep
// re-drives must keep aging from when it last became outstanding, or the
// backlog age would understate a stall.
//
// The CASE reads fact_work_items.status, the conflicting row's status before
// this UPDATE, which ON CONFLICT DO UPDATE evaluates against the locked row
// version. A concurrent writer that commits first therefore decides the
// branch on the newest status, so the stamp is idempotent under concurrent
// re-drives.
//
// Writers name the column in their own SQL text and append this value, so the
// #9320 source guard (TestEveryTerminalReopenWriterStampsReopenedAt) can see
// the stamp in each writer.
const ReopenedAtUpsertValue = `CASE
            WHEN fact_work_items.status IN ('pending', 'retrying', 'claimed', 'running')
                THEN fact_work_items.reopened_at
            ELSE EXCLUDED.updated_at
        END`
