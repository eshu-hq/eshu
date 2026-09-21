// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schedule

import (
	"testing"
	"time"
)

func TestTargetCreatedAtSpacingSurvivesPostgresTimestampPrecision(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 26, 18, 15, 0, 0, time.UTC)
	first := TargetCreatedAt(observedAt, 0).Truncate(time.Microsecond)
	second := TargetCreatedAt(observedAt, 1).Truncate(time.Microsecond)
	if !first.Before(second) {
		t.Fatalf("TargetCreatedAt spacing collapses at Postgres precision: first=%s second=%s",
			first.Format(time.RFC3339Nano), second.Format(time.RFC3339Nano))
	}
}
