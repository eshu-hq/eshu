// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Bounded narrowed wait keys (#11297).
//
// reducer_waiting_items_pkey is a btree over (work_item_id, waits_for_kind,
// waits_for_key). Postgres refuses an index row above roughly 2704 bytes
// (SQLSTATE 54000), and the narrowed corpus key carried the whole verdict-time
// blocking scope set: a fleet with a few hundred holding scopes rendered a key
// of tens of kilobytes, the park INSERT failed inside WorkSink.Fail, and the
// batch worker reads a broken Fail as fatal. The budgets below cap the two set
// fields; the rest of the key (scope, generation, a few fixed fields) and the
// work_item_id stay well inside the row limit.
const (
	// narrowedWaitingBlockingBudgetBytes bounds the JSON bytes the blocking
	// scope set may add to a narrowed wait key.
	narrowedWaitingBlockingBudgetBytes = 1200
	// narrowedWaitingRepoBudgetBytes bounds the JSON bytes the read-set
	// repository ids may add. The wake never matches on them; they only name
	// the read set.
	narrowedWaitingRepoBudgetBytes = 400
)

// boundWaitingScopeIDs keeps the leading elements of a normalized (sorted,
// deduplicated) id set whose JSON rendering fits budget bytes, and reports
// whether anything was cut. The kept set is a deterministic sorted prefix, so
// equal inputs render equal keys and a repeated Fail stays idempotent.
//
// A bounded blocking set is still a correct wake hint: the wake matches on any
// retained scope, the retried handler re-checks the full restricted fence
// (over-wake costs one re-deferral), the parked wait is never satisfied at park
// time because the prefix is drawn from the scopes that hold the fence now, and
// the audit plus the 10-minute backstop bound a missed wake. Dropping the whole
// set instead would fall back to the coarse own-generation wait, which is
// already satisfied here and would reopen the waiter in a tight loop.
func boundWaitingScopeIDs(sorted []string, budget int) (kept []string, truncated bool) {
	used := 0
	for i, id := range sorted {
		raw, err := json.Marshal(id)
		if err != nil {
			return sorted[:i], true
		}
		used += len(raw) + 1 // the element and its separating comma
		if used > budget {
			return sorted[:i], true
		}
	}
	return sorted, false
}

// recordWaitingKeyBounded counts one narrowed wait parked with a cut key, on
// eshu_dp_reducer_waiting_key_bounded_total.
func recordWaitingKeyBounded(ctx context.Context, instruments *telemetry.Instruments) {
	if instruments == nil || instruments.ReducerWaitingKeyBounded == nil {
		return
	}
	instruments.ReducerWaitingKeyBounded.Add(ctx, 1)
}
