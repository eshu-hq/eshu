// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/poison"
)

// TestPoisonBatchDefaultMatchesGenerationLiveness pins the poison-leaf sweep
// batch default to the generation-liveness sweep default (F1 of the #6693
// poison-leaf review, #8208). The leaf keeps its own exported constant so
// production code stays decoupled from the parent package; this test-only
// import is the pin that fails loudly if either default drifts.
func TestPoisonBatchDefaultMatchesGenerationLiveness(t *testing.T) {
	t.Parallel()
	if poison.DefaultBatchLimit != defaultGenerationLivenessBatchLimit {
		t.Fatalf("poison.DefaultBatchLimit = %d, want %d (defaultGenerationLivenessBatchLimit)",
			poison.DefaultBatchLimit, defaultGenerationLivenessBatchLimit)
	}
}
