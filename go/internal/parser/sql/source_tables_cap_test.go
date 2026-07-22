// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sql

import (
	"fmt"
	"testing"
)

// TestSelectReadTargetsCapTruncatesDeterministically proves the
// sqlSourceTablesCap bound on view/function source_tables (#5345): a
// pathological view that selects from more than sqlSourceTablesCap distinct
// tables must stamp exactly the first sqlSourceTablesCap names in sorted order,
// so the payload stays bounded and golden-corpus/cassette output stays stable
// regardless of mention order.
func TestSelectReadTargetsCapTruncatesDeterministically(t *testing.T) {
	const overCap = sqlSourceTablesCap + 6

	// Build distinct select mentions in REVERSE lexical order to prove the
	// sort-before-cap ordering (a naive first-N-seen cap would keep the wrong
	// names). table_000..table_(overCap-1) exist; appended reversed.
	mentions := make([]sqlMention, 0, overCap)
	for i := overCap - 1; i >= 0; i-- {
		mentions = append(mentions, sqlMention{
			name:      fmt.Sprintf("table_%03d", i),
			operation: "select",
			offset:    i,
		})
	}

	got := selectReadTargets(mentions)

	if len(got) != sqlSourceTablesCap {
		t.Fatalf("selectReadTargets len = %d, want %d (cap)", len(got), sqlSourceTablesCap)
	}
	for i := 0; i < sqlSourceTablesCap; i++ {
		want := fmt.Sprintf("table_%03d", i)
		if got[i] != want {
			t.Fatalf("selectReadTargets[%d] = %q, want %q (sorted-then-capped)", i, got[i], want)
		}
	}
}

// TestSelectReadTargetsDedupesAndFiltersNonSelect proves the pre-cap
// dedupe + select-only filter so a write target (INSERT/UPDATE) or a repeated
// read never inflates or pollutes source_tables (#5345).
func TestSelectReadTargetsDedupesAndFiltersNonSelect(t *testing.T) {
	// "orders" appears twice (duplicate read -> collapsed); "audit_log" is an
	// insert (write target -> excluded); result must be sorted, deduped,
	// select-only.
	mentions := []sqlMention{
		{name: "orders", operation: "select", offset: 0},
		{name: "orders", operation: "select", offset: 10},
		{name: "audit_log", operation: "insert", offset: 20},
		{name: "customers", operation: "select", offset: 30},
	}

	got := selectReadTargets(mentions)

	want := []string{"customers", "orders"} // sorted, deduped, select-only
	if len(got) != len(want) {
		t.Fatalf("selectReadTargets = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("selectReadTargets[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestRoutineWriteTargetsCapTruncatesDeterministically proves routine write
// metadata shares the same bounded, sorted contract as source_tables while
// excluding SELECT-only mentions.
func TestRoutineWriteTargetsCapTruncatesDeterministically(t *testing.T) {
	const overCap = sqlSourceTablesCap + 6

	mentions := make([]sqlMention, 0, overCap+1)
	for i := overCap - 1; i >= 0; i-- {
		mentions = append(mentions, sqlMention{
			name:      fmt.Sprintf("table_%03d", i),
			operation: []string{"insert", "update", "delete"}[i%3],
			offset:    i,
		})
	}
	mentions = append(mentions, sqlMention{name: "ignored_read", operation: "select"})

	got := routineWriteTargets(mentions)
	if len(got) != sqlSourceTablesCap {
		t.Fatalf("routineWriteTargets len = %d, want %d (cap)", len(got), sqlSourceTablesCap)
	}
	for i := 0; i < sqlSourceTablesCap; i++ {
		want := fmt.Sprintf("table_%03d", i)
		if got[i] != want {
			t.Fatalf("routineWriteTargets[%d] = %q, want %q (sorted-then-capped)", i, got[i], want)
		}
	}
}
