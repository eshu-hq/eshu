// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestNewInstrumentedStatusStoreWiresInstruments guards the #4533 follow-up to
// #4446/#4530: every readiness-probe call site must wire the shared
// meter-provider Instruments onto the StatusStore it constructs, or the
// status query cache metric (eshu_dp_status_stage_counts_cache_total,
// recorded in status_stage_counts_cache.go's listStageCounts) stays
// contract-complete but silent on that process. This test fails if a future
// edit drops the assignment inside NewInstrumentedStatusStore.
func TestNewInstrumentedStatusStoreWiresInstruments(t *testing.T) {
	t.Parallel()

	instruments := &telemetry.Instruments{}

	store := NewInstrumentedStatusStore(&fakeQueryer{}, instruments)

	if store.Instruments != instruments {
		t.Fatalf("store.Instruments = %p, want the same instance passed in (%p) — NewInstrumentedStatusStore must assign it, not leave it nil", store.Instruments, instruments)
	}
}

// TestNewInstrumentedStatusStoreAllowsNilInstruments proves the constructor
// does not require Instruments to be non-nil: recordStatusStageCountsCacheOutcome
// treats a nil Instruments as a no-op (never a panic), so a caller without a
// wired meter provider is unaffected.
func TestNewInstrumentedStatusStoreAllowsNilInstruments(t *testing.T) {
	t.Parallel()

	store := NewInstrumentedStatusStore(&fakeQueryer{}, nil)

	if store.Instruments != nil {
		t.Fatalf("store.Instruments = %v, want nil when the caller passes nil", store.Instruments)
	}
}
