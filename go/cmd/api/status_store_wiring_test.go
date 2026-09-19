// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"

	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestNewStatusStoreWiresInstruments guards the #4446 follow-up: the API's
// operator status-serving path must assign the shared meter-provider
// Instruments onto the StatusStore it constructs, or the status query cache
// metric (eshu_dp_status_snapshot_read_duration_seconds, recorded by
// internal/storage/postgres/status_read_telemetry.go's statusReadQueryer)
// stays contract-complete but never emits on the API path. NewStatusStore
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
// require Instruments to be non-nil: statusReadQueryer.record
// treats a nil Instruments as a no-op (never a panic), so a caller without a
// wired meter provider is unaffected.
func TestNewStatusStoreAllowsNilInstruments(t *testing.T) {
	store := newStatusStore(pgstatus.SQLQueryer{}, nil)

	if store.Instruments != nil {
		t.Fatalf("store.Instruments = %v, want nil when the caller passes nil", store.Instruments)
	}
}

// TestNewStatusQueryerInstrumentsStatusReads guards the #6794 follow-up: the
// status snapshot issues about thirty sequential Postgres reads per request,
// and on the raw SQLQueryer none of them produced a span or a
// eshu_dp_postgres_query_duration_seconds sample, so a 20s status route showed
// no storage signal at all. With instruments wired, the API must read status
// through the instrumented wrapper under the status_snapshot store label.
func TestNewStatusQueryerInstrumentsStatusReads(t *testing.T) {
	instruments := &telemetry.Instruments{}

	queryer := newStatusQueryer(nil, instruments)

	instrumented, ok := queryer.(*pgstatus.InstrumentedDB)
	if !ok {
		t.Fatalf("newStatusQueryer() = %T, want *postgres.InstrumentedDB", queryer)
	}
	if instrumented.StoreName != statusSnapshotStoreName {
		t.Fatalf("StoreName = %q, want %q", instrumented.StoreName, statusSnapshotStoreName)
	}
	if instrumented.Instruments != instruments || instrumented.Tracer == nil {
		t.Fatalf("instrumented wrapper must carry the shared instruments and a tracer: %+v", instrumented)
	}
}

// TestNewStatusQueryerWithoutInstrumentsUsesRawReads keeps the nil-instruments
// path (tests, local tools) on the plain SQLQueryer.
func TestNewStatusQueryerWithoutInstrumentsUsesRawReads(t *testing.T) {
	if _, ok := newStatusQueryer(nil, nil).(pgstatus.SQLQueryer); !ok {
		t.Fatalf("newStatusQueryer(nil, nil) must return the raw SQLQueryer")
	}
}
