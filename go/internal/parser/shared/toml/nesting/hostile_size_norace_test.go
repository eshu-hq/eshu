// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build !race

package nesting

// hostileTableEntries is the full hostile fixture scale for standard test
// runs: 100,000-entry documents that hold the go-toml key lookup for about
// 18 seconds each. See hostile_size_race_test.go for the race-detector
// scale.
func hostileTableEntries() int {
	return 100_000
}

// repeatScale returns the sibling width, repeat count, and per-element
// key count for the repeated-header layouts at full scale.
func repeatScale() (distinct, repeats, elemKeys int) {
	return 4900, 100_000, 0
}
