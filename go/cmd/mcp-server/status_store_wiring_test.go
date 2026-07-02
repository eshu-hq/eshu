// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"

	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestNewStatusStoreWiresInstruments guards the #4446 follow-up: the MCP
// server's operator status-serving path must assign the shared meter-provider
// Instruments onto the StatusStore it constructs, or the status query cache
// metric (eshu_dp_status_stage_counts_cache_total, recorded in
// internal/storage/postgres/status_stage_counts_cache.go's listStageCounts)
// stays contract-complete but never emits on the MCP path. NewStatusStore
// deliberately leaves Instruments nil for source-compatibility across the
// ~30 call sites; this test fails if a future edit drops the assignment
// inside newStatusStore.
func TestNewStatusStoreWiresInstruments(t *testing.T) {
	instruments := &telemetry.Instruments{}

	store := newStatusStore(pgstatus.SQLQueryer{}, instruments)

	if store.Instruments != instruments {
		t.Fatalf("store.Instruments = %p, want the same instance passed in (%p) — the production wiring must assign it, not leave it nil", store.Instruments, instruments)
	}
}

// TestNewStatusStoreAllowsNilInstruments proves the wiring helper does not
// require Instruments to be non-nil: recordStatusStageCountsCacheOutcome
// treats a nil Instruments as a no-op (never a panic), so a caller without a
// wired meter provider is unaffected.
func TestNewStatusStoreAllowsNilInstruments(t *testing.T) {
	store := newStatusStore(pgstatus.SQLQueryer{}, nil)

	if store.Instruments != nil {
		t.Fatalf("store.Instruments = %v, want nil when the caller passes nil", store.Instruments)
	}
}
