// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package lines

import "testing"

// TestFinalizeBatchByteBudgetDefaultsToFourMiB pins the default that bounds a
// live writer's wait behind a batch (about 0.5 s per 4 MiB). A zero or negative
// value takes the default, and a positive one is kept.
func TestFinalizeBatchByteBudgetDefaultsToFourMiB(t *testing.T) {
	t.Parallel()

	if got := (Options{}).withDefaults().MaxBatchBytes; got != 4<<20 {
		t.Fatalf("default MaxBatchBytes = %d, want %d", got, 4<<20)
	}
	if got := (Options{MaxBatchBytes: -1}).withDefaults().MaxBatchBytes; got != 4<<20 {
		t.Fatalf("negative MaxBatchBytes = %d, want the default", got)
	}
	if got := (Options{MaxBatchBytes: 123}).withDefaults().MaxBatchBytes; got != 123 {
		t.Fatalf("MaxBatchBytes = %d, want the configured 123", got)
	}
}
