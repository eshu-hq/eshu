// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// lockProjectorMarkerFenceQuery is the first statement of the #7389
// write-start marker's transaction (#7819). It locks the scope's
// projector_scope_claim_fences row non-blocking, before the marker UPDATE
// locks the generation row. A claim holds that fence row while its candidate
// step runs, so the marker commit and a claim's fence lock serialize: when
// the fence is busy the marker defers (ErrWorkWriteMarkerDeferred, re-run by
// the caller) instead of committing a marker the claim's snapshot cannot see,
// which closes the skip-blind split where the sweep spared a marked retry
// while the same statement claimed the newer row.
//
// NO KEY UPDATE matches the lock the bump UPDATE below takes. SKIP LOCKED
// keeps the marker out of every wait cycle: the marker already waits on the
// generation row under lock_timeout, and waiting on the fence too would let
// it join a cycle with a claim holding the fence and pulling generations.
// A missing row defers the same as a busy one: a claimed work row implies a
// fence row (the claim inner-joins it), so a missing row means the scope is
// gone and classifyWriteMarkerRefusal would refuse the marker anyway; the
// bounded retry loop caps the wait.
//
// $1 scope.
const lockProjectorMarkerFenceQuery = `
SELECT fence
FROM projector_scope_claim_fences
WHERE scope_id = $1
FOR NO KEY UPDATE SKIP LOCKED
`

// bumpProjectorMarkerFenceQuery is the last statement of the marker's
// transaction, run only when the marker set. The bump tells a claim whose
// snapshot predates the marker to drop its candidate through the #7115 fence
// recheck and re-read, so the snapshot-only hold guards see the marker on
// the next attempt. It updates only the row the lock statement already holds,
// so it never waits. A refused marker bumps nothing: nothing was written for
// a concurrent claim to re-read.
//
// $1 scope.
const bumpProjectorMarkerFenceQuery = `
UPDATE projector_scope_claim_fences
SET fence = fence + 1
WHERE scope_id = $1
`
