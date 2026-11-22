// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queuestore

// ReplayRetryAllowanceAssignments is the SET-list fragment of the four
// writers that move a dead-lettered or failed fact_work_items row back to
// pending while keeping attempt_count: the three operator replay paths (the
// admin replay store, the recovery replay and its bounded drain variant,
// #11182) and the reducer's poison-liveness auto-retry sweep
// (poison.RecoverDeadLettersQuery, #11217). Each carrier pairs it with an
// attempt_count assignment that raises a projector row to at least 1, so a
// dead letter at attempt 0 gets baseline 1 and its first claim counts 1. It
// gives a replayed or recovered projector row the retry allowance of a new
// row without lowering attempt_count, which stays the monotonic claim fence
// (every projector worker shares one lease owner, so a lowered attempt_count
// would let a stale holder of an old claim match a new claim at the same
// attempt). Writers that lower attempt_count (reproject, refinalize) reset the
// baseline to 0 instead and do not carry this fragment.
//
// replay_attempt_baseline takes the row's attempt_count at the replay (at
// least 1, matching the attempt_count raise each carrier writes), and
// ProjectorQueue.Fail subtracts it from the projection budget.
// admission_wait_attempts goes back to 0, so the admission budget starts fresh.
// The baseline is projector-only: the reducer budget does not read it yet, so
// a reducer row keeps its stored value and baseline <= attempt_count holds on
// every row.
//
// The right-hand sides read the row version before the UPDATE, so the
// fragment is the same whether it runs before or after the attempt_count
// assignment. Column references are unqualified so the fragment fits the
// aliased admin UPDATE (fact_work_items AS work), the unaliased recovery
// UPDATE and the aliased sweep UPDATE (fact_work_items AS target); no
// carrier's FROM list exposes a column with these names (the sweep has no
// FROM list: its CTEs are read only inside subqueries).
// The fragment has no trailing comma: writers append it as the last
// assignment of the SET list, after reopened_at, so the literal that opens the
// SET list still names reopened_at for the #9320 source guard
// (TestEveryTerminalReopenWriterStampsReopenedAt).
const ReplayRetryAllowanceAssignments = `replay_attempt_baseline = CASE
            WHEN stage = 'projector' THEN GREATEST(attempt_count, 1)
            ELSE replay_attempt_baseline
        END,
        admission_wait_attempts = 0`
