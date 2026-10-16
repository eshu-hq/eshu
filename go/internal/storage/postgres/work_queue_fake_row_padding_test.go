// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// padQueueFakeRow widens a staged queueFakeRows row to the column count a
// newer query scans, so older fixtures keep exercising the behavior they
// were written for. A row whose width has no padding rule is returned
// unchanged, and Scan then reports the count mismatch.
func padQueueFakeRow(row []any, columns int) []any {
	// Reducer claim rows gained a target-only claim epoch between attempt_count
	// and created_at. Older unrelated-domain fixtures intentionally omit it;
	// synthesize the zero opt-out value while target-domain fixtures exercise
	// the explicit ninth column.
	if columns == 9 && len(row) == 8 {
		row = append(row[:5:5], append([]any{int64(0)}, row[5:]...)...)
	}
	// Reducer claim rows now also return the persisted last_attempt_at token
	// between cycle_started_at and payload. Older fixtures predate that output;
	// use their cycle anchor as an explicit timestamp so they remain focused on
	// the queue behavior they were written to cover. Dedicated claim-fence tests
	// provide a distinct token and assert it exactly.
	if columns == 11 && len(row) == 10 {
		row = append(row[:9:9], append([]any{row[8]}, row[9:]...)...)
	}
	// Retention candidate rows carry ten columns: the #7981 identity triple
	// then the #7802 delta_protected flag. Six-column fixtures zero-fill the
	// triple (a zero identity never matches) and the flag; nine-column
	// fixtures staged before the flag existed zero-fill only the flag.
	if columns == 10 && len(row) == 6 {
		row = append(row[:6:6], "", false, "", false)
	}
	if columns == 10 && len(row) == 9 {
		row = append(row[:9:9], false)
	}
	return row
}
