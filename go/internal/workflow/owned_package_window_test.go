// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"slices"
	"testing"
)

func TestOwnedPackageTargetSlotCountFillRule(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		total int64
		limit int
		want  int64
	}{
		{name: "empty inventory is one slot", total: 0, limit: 2000, want: 1},
		{name: "negative total is one slot", total: -5, limit: 2000, want: 1},
		{name: "zero limit is one slot", total: 100, limit: 0, want: 1},
		{name: "one target", total: 1, limit: 2000, want: 1},
		{name: "exactly half the limit fits one slot", total: 1000, limit: 2000, want: 1},
		{name: "just above half the limit needs two", total: 1001, limit: 2000, want: 2},
		{name: "QA inventory at limit 2000", total: 30264, limit: 2000, want: 32},
		{name: "power-of-two boundary stays", total: 32000, limit: 2000, want: 32},
		{name: "just past the boundary doubles", total: 32001, limit: 2000, want: 64},
		{name: "QA inventory at limit 1000", total: 30264, limit: 1000, want: 64},
		{name: "QA inventory at default limit 100", total: 30264, limit: 100, want: 1024},
		{name: "just below a power-of-two quotient", total: 1599, limit: 100, want: 32},
		{name: "exactly a power-of-two quotient", total: 1600, limit: 100, want: 32},
		{name: "just above a power-of-two quotient", total: 1601, limit: 100, want: 64},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := OwnedPackageTargetSlotCount(tc.total, tc.limit)
			if got != tc.want {
				t.Fatalf("OwnedPackageTargetSlotCount(%d, %d) = %d, want %d", tc.total, tc.limit, got, tc.want)
			}
			if got&(got-1) != 0 {
				t.Fatalf("slot count %d is not a power of two", got)
			}
			if tc.total > 0 && tc.limit > 0 {
				if got*int64(tc.limit) < 2*tc.total {
					t.Fatalf("mean slot %d/%d exceeds half the limit %d", tc.total, got, tc.limit)
				}
				if got > 1 && (got/2)*int64(tc.limit) >= 2*tc.total {
					t.Fatalf("slot count %d is not the smallest power of two for T=%d L=%d", got, tc.total, tc.limit)
				}
			}
		})
	}
}

func TestOwnedPackageDependencyTargetWindowTruncation(t *testing.T) {
	t.Parallel()

	whole := OwnedPackageDependencyTargetWindow{SlotSize: 946, RowsRead: 946}
	if whole.Truncated() || whole.UnreadTargets() != 0 {
		t.Fatalf("whole slot read reports truncation: %#v", whole)
	}
	cut := OwnedPackageDependencyTargetWindow{SlotSize: 2002, RowsRead: 2001}
	if !cut.Truncated() || cut.UnreadTargets() != 1 {
		t.Fatalf("cut slot = truncated %v unread %d, want true 1", cut.Truncated(), cut.UnreadTargets())
	}
	if (OwnedPackageDependencyTargetWindow{}).Truncated() {
		t.Fatal("zero window reports truncation")
	}
}

func TestOwnedPackageDependencyTargetPageSelectedByEcosystem(t *testing.T) {
	t.Parallel()

	page := OwnedPackageDependencyTargetPage{Targets: []OwnedPackageDependencyTarget{
		{Ecosystem: "rubygems"}, {Ecosystem: "npm"}, {Ecosystem: "npm"}, {Ecosystem: "go"},
	}}
	ecosystems, selected := page.SelectedByEcosystem()
	if !slices.Equal(ecosystems, []string{"go", "npm", "rubygems"}) || !slices.Equal(selected, []int{1, 2, 1}) {
		t.Fatalf("SelectedByEcosystem() = %v %v, want [go npm rubygems] [1 2 1]", ecosystems, selected)
	}
	if ecosystems, selected := (OwnedPackageDependencyTargetPage{}).SelectedByEcosystem(); len(ecosystems) != 0 || len(selected) != 0 {
		t.Fatalf("empty page = %v %v, want none", ecosystems, selected)
	}
}
