// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queuestore

// ReplayRetryAllowanceAssignments is the SET-list fragment the three operator
// replay paths of a terminal fact_work_items row carry: the admin replay
// store, the recovery replay and its bounded drain variant (#11182). The
// reducer's poison-liveness auto-retry sweep is a fourth dead_letter to
// pending writer and is out of scope here (#11217): the poison-liveness sweep
// does not carry the allowance; it keeps the three counters, so a recovered
// row resumes the budget it had left, and a row that used up its projection
// budget dead-letters again on its first projection failure (#11217). It gives a
// replayed projector row the retry allowance of a new row without lowering
// attempt_count, which stays the monotonic claim fence (every projector worker
// shares one lease owner, so a lowered attempt_count would let a stale holder
// of an old claim match a new claim at the same attempt).
//
// replay_attempt_baseline takes the row's attempt_count at the replay (at
// least 1, matching the attempt_count = GREATEST(attempt_count, 1) the replay
// writes), and ProjectorQueue.Fail subtracts it from the projection budget.
// admission_wait_attempts goes back to 0, so the admission budget starts fresh.
// The baseline is projector-only: the reducer budget does not read it yet, so
// a reducer row keeps its stored value and baseline <= attempt_count holds on
// every row.
//
// The right-hand sides read the row version before the UPDATE, so the
// fragment is the same whether it runs before or after the attempt_count
// assignment. Column references are unqualified so the fragment fits both the
// aliased admin UPDATE (fact_work_items AS work) and the unaliased recovery
// UPDATE; neither statement's FROM list exposes a column with these names.
// The fragment has no trailing comma: writers append it as the last
// assignment of the SET list, after reopened_at, so the literal that opens the
// SET list still names reopened_at for the #9320 source guard
// (TestEveryTerminalReopenWriterStampsReopenedAt).
const ReplayRetryAllowanceAssignments = `replay_attempt_baseline = CASE
            WHEN stage = 'projector' THEN GREATEST(attempt_count, 1)
            ELSE replay_attempt_baseline
        END,
        admission_wait_attempts = 0`
