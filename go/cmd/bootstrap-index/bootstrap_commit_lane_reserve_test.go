// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import "testing"

// TestCommitLaneReserveHoldsTheBulkLoadLockConnection pins the #7125 pool
// budget: the run-scoped bulk-load lock pins one connection for the whole run,
// so the reserve is the projection workers plus maintenance plus that one,
// never fewer than three.
func TestCommitLaneReserveHoldsTheBulkLoadLockConnection(t *testing.T) {
	t.Parallel()
	for workers, want := range map[int]int{0: 3, 1: 3, 2: 4, 4: 6, 8: 10} {
		if got := commitLaneReserve(workers); got != want {
			t.Fatalf("commitLaneReserve(%d) = %d, want %d", workers, got, want)
		}
	}
}
