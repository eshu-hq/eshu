// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package boundedset

import "sort"

// DedupeSortCap sorts a copy of items using less, drops an item that
// dedupeKey reports as a duplicate of its immediate sorted predecessor, caps
// the resulting distinct set at maxRows, and returns the capped slice plus
// the full distinct-item count computed BEFORE the cap.
//
// less must impose a total order (i.e. it must break every tie so no two
// distinct-by-dedupeKey items compare equal both ways) — the deterministic,
// input-order-independent guarantee this function exists to provide only
// holds when less fully orders items. When two items ARE duplicates per
// dedupeKey, less's final tiebreak decides which one survives dedupe: only
// the sorted predecessor of a duplicate run is kept.
//
// dedupeKey is checked only between sort-adjacent items, so it must agree
// with less: any two items dedupeKey treats as duplicates must sort next to
// each other under less (typically dedupeKey compares the same identity
// fields less's non-final comparisons use).
//
// maxRows <= 0 disables capping (the full deduped set is returned).
func DedupeSortCap[T any](items []T, less func(a, b T) bool, dedupeKey func(a, b T) bool, maxRows int) ([]T, int) {
	sorted := make([]T, len(items))
	copy(sorted, items)
	sort.SliceStable(sorted, func(i, j int) bool {
		return less(sorted[i], sorted[j])
	})

	deduped := make([]T, 0, len(sorted))
	for i, item := range sorted {
		if i > 0 && dedupeKey(sorted[i-1], item) {
			continue
		}
		deduped = append(deduped, item)
	}

	count := len(deduped)
	if maxRows > 0 && count > maxRows {
		deduped = deduped[:maxRows]
	}
	return deduped, count
}
