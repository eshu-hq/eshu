// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import "slices"

// maxOwnedPackageTargetSlotCount bounds the slot count so the doubling loop in
// [OwnedPackageTargetSlotCount] cannot overflow int64. No real inventory comes
// near it: a slot count of 2^40 would need T above 2^39 * limit.
const maxOwnedPackageTargetSlotCount = int64(1) << 40

// OwnedPackageTargetSlotCount returns P, the number of hash slots the
// owned-package reader partitions T distinct targets into for a target limit
// L (#9422). P is the smallest power of two with P*L >= 2*T, so the mean slot
// holds at most L/2 targets and the largest slot stays below the read limit
// in normal operation. T <= 0 or L <= 0 gives one slot.
//
// The reader computes P in SQL from the same T in the same statement, and
// checks the value it gets back against this function, so the rule has one
// definition that both the Go tests and the live SQL tests pin.
func OwnedPackageTargetSlotCount(totalTargets int64, targetLimit int) int64 {
	if totalTargets <= 0 || targetLimit <= 0 {
		return 1
	}
	limit := int64(targetLimit)
	slots := int64(1)
	for slots*limit < 2*totalTargets && slots < maxOwnedPackageTargetSlotCount {
		slots *= 2
	}
	return slots
}

// OwnedPackageDependencyTargetWindow describes the hash-slot window one
// owned-package read selected (#9422). The coordinator logs it per read, and
// the derivation uses it to report budget exhaustion when the read could not
// take the whole slot.
type OwnedPackageDependencyTargetWindow struct {
	// TotalTargets is T, the distinct targets of the requested ecosystems
	// across every slot.
	TotalTargets int64
	// SlotCount is P, derived from T and the target limit by
	// [OwnedPackageTargetSlotCount].
	SlotCount int64
	// Slot is the selected slot, the rotation bucket mod P.
	Slot int64
	// SlotSize is the number of distinct targets in the selected slot.
	SlotSize int64
	// RowsRead is the number of rows the statement returned for the slot,
	// before the reader drops rows with a blank ecosystem or package name.
	RowsRead int64
}

// Truncated reports whether the read limit cut the slot short, leaving
// targets of the selected slot unread this window.
func (w OwnedPackageDependencyTargetWindow) Truncated() bool {
	return w.SlotSize > w.RowsRead
}

// UnreadTargets returns how many targets of the selected slot the read left
// out, or zero when it took the whole slot.
func (w OwnedPackageDependencyTargetWindow) UnreadTargets() int64 {
	if !w.Truncated() {
		return 0
	}
	return w.SlotSize - w.RowsRead
}

// OwnedPackageDependencyTargetPage is one owned-package read: the selected
// slot's targets in the global target order, and the window that produced
// them.
type OwnedPackageDependencyTargetPage struct {
	Targets []OwnedPackageDependencyTarget
	Window  OwnedPackageDependencyTargetWindow
}

// SelectedByEcosystem counts the page's targets per ecosystem. It returns the
// ecosystems in lexical order and their counts in the same order, so logs and
// span events stay byte-stable for the same read.
func (p OwnedPackageDependencyTargetPage) SelectedByEcosystem() ([]string, []int) {
	counts := map[string]int{}
	for _, target := range p.Targets {
		counts[target.Ecosystem]++
	}
	ecosystems := make([]string, 0, len(counts))
	for ecosystem := range counts {
		ecosystems = append(ecosystems, ecosystem)
	}
	slices.Sort(ecosystems)
	selected := make([]int, 0, len(ecosystems))
	for _, ecosystem := range ecosystems {
		selected = append(selected, counts[ecosystem])
	}
	return ecosystems, selected
}
