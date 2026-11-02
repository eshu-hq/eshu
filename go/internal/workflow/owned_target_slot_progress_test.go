// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"slices"
	"testing"
)

func TestResolveSlotAdvance(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		picked        []int
		rowSlots      []int
		truncated     bool
		want          []int
		wantHeadTrunc bool
	}{
		{
			name:   "full read advances every picked slot including empty ones",
			picked: []int{3, 9, 40}, rowSlots: []int{3, 3, 40},
			want: []int{3, 9, 40},
		},
		{
			name:   "empty read advances the empty pick (T7)",
			picked: []int{17}, rowSlots: nil,
			want: []int{17},
		},
		{
			name:   "truncated read advances only whole slots before the last read slot",
			picked: []int{3, 9, 40}, rowSlots: []int{3, 3, 9}, truncated: true,
			want: []int{3},
		},
		{
			name:   "an empty slot before the cut is whole and advances, and so does the oversize head after it",
			picked: []int{3, 9, 40}, rowSlots: []int{9, 9}, truncated: true,
			want: []int{3, 9}, wantHeadTrunc: true,
		},
		{
			name:   "an empty slot before a partial later slot advances alone",
			picked: []int{3, 9, 40}, rowSlots: []int{9, 9, 40}, truncated: true,
			want: []int{3, 9},
		},
		{
			name:   "head slot that alone exceeds the limit advances anyway (T11)",
			picked: []int{3, 9, 40}, rowSlots: []int{3, 3, 3}, truncated: true,
			want: []int{3}, wantHeadTrunc: true,
		},
		{
			name:   "head slot with empty slots before it advances with them",
			picked: []int{1, 3, 9}, rowSlots: []int{3, 3}, truncated: true,
			want: []int{1, 3}, wantHeadTrunc: true,
		},
		{
			name:   "truncated read that returned no rows advances nothing",
			picked: []int{3}, rowSlots: nil, truncated: true,
			want: nil,
		},
		{
			name:   "no picked slots advances nothing",
			picked: nil, rowSlots: nil,
			want: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, head := ResolveSlotAdvance(tc.picked, tc.rowSlots, tc.truncated)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("ResolveSlotAdvance() slots = %v, want %v", got, tc.want)
			}
			if head != tc.wantHeadTrunc {
				t.Fatalf("ResolveSlotAdvance() head truncated = %v, want %v", head, tc.wantHeadTrunc)
			}
		})
	}
}

func TestVirtualSlotOfKeyHash(t *testing.T) {
	t.Parallel()

	for _, hash := range []uint32{0, 1, 1023, 1024, 1025, 1<<24 - 1} {
		if got, want := VirtualSlotOfKeyHash(hash), int(hash%OwnedPackageVirtualSlotCount); got != want {
			t.Fatalf("VirtualSlotOfKeyHash(%d) = %d, want %d", hash, got, want)
		}
	}
}

func TestOwnedTargetSlotSelectorEnabled(t *testing.T) {
	t.Parallel()

	if (OwnedTargetSlotSelector{}).Enabled() {
		t.Fatal("zero selector must be disabled so the clock reader stays the default")
	}
	if (OwnedTargetSlotSelector{CollectorInstanceID: "i"}).Enabled() {
		t.Fatal("selector without a derivation kind must be disabled")
	}
	if !(OwnedTargetSlotSelector{CollectorInstanceID: "i", DerivationKind: SlotDerivationKindVulnerability}).Enabled() {
		t.Fatal("selector with instance and kind must be enabled")
	}
}
