// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fake

// RowAdapter reshapes one staged row before Rows.Scan checks it against
// destCount (len(dest) at the call site). It returns the row unchanged for
// any shape it does not recognize, including a genuine column-count
// mismatch, so the caller's own length check still catches that case.
//
// This exists so a caller with legacy fixtures whose row shape predates a
// schema change does not need its own Rows/Scan reimplementation: it sets
// Adapt once (on a specific Rows, or on ExecQueryer for every Rows the fake
// hands out) instead. See LegacyQueueRowAdapter for the reducer-queue case
// this replaced.
type RowAdapter func(destCount int, row []any) []any

// LegacyQueueRowAdapter reproduces the two column-count reshapes moved
// reducer-queue fixtures relied on before the work-queue schema grew two
// columns.
//
// Reducer claim rows gained a target-only claim epoch between attempt_count
// and created_at; older unrelated-domain fixtures intentionally omit it, so
// an 8-column row scanned into 9 destinations gets a synthesized zero
// opt-out value spliced in at index 5.
//
// Reducer claim rows later also gained a persisted last_attempt_at token
// between cycle_started_at and payload; a 10-column row scanned into 11
// destinations gets its cycle_started_at value (index 8) duplicated into
// that slot, so pre-existing fixtures stay focused on the queue behavior
// they were written to test instead of needing an extra column added.
// Dedicated claim-fence tests provide a distinct last_attempt_at value and
// construct their own row instead of using this adapter.
//
// Any other destination-count/row-length combination, including a genuine
// mismatch, passes through unchanged. Moved queue tests opt into this by
// setting it as ExecQueryer.Adapt or a specific Rows.Adapt; it is not the
// fake's default, since a package with no legacy queue fixtures should never
// have destination-count-dependent row reshaping applied silently.
func LegacyQueueRowAdapter(destCount int, row []any) []any {
	switch {
	case destCount == 9 && len(row) == 8:
		return append(row[:5:5], append([]any{int64(0)}, row[5:]...)...)
	case destCount == 11 && len(row) == 10:
		return append(row[:9:9], append([]any{row[8]}, row[9:]...)...)
	default:
		return row
	}
}
