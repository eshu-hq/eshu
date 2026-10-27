// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build race

package nesting

// hostileTableEntries scales the hostile fixtures down under the race
// detector. The full 100,000-entry documents allocate gigabytes per
// allocatedBytes run (one warmup plus four measured), which exceeds the
// package test timeout under race instrumentation. The scaled documents
// still exceed the 25M work limit by multiples, so every rejection
// assertion keeps its meaning; the full scale runs in the standard suite.
func hostileTableEntries() int {
	return 12_000
}

// repeatScale returns the sibling width, repeat count, and per-element
// key count for the repeated-header layouts, scaled for the race
// detector for the same reason as hostileTableEntries.
func repeatScale() (distinct, repeats, elemKeys int) {
	return 4900, 12_000, 0
}
