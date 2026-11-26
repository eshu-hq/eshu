// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// armedQueryer answers the armed read with a scripted state and counts the reads.
type armedQueryer struct {
	armed bool
	err   error
	reads int
	names []any
}

func (q *armedQueryer) QueryContext(_ context.Context, _ string, args ...any) (db.Rows, error) {
	q.reads++
	q.names = args
	if q.err != nil {
		return nil, q.err
	}
	return &flagRows{flags: []bool{q.armed}}, nil
}

// TestFactWriteStopWatchReadsTheNamedTriggerAndCaches pins the watch: it reads the trigger the
// migration creates (the classifier's constraint name), trusts a read for one TTL, and re-reads after.
func TestFactWriteStopWatchReadsTheNamedTriggerAndCaches(t *testing.T) {
	t.Parallel()

	clock := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	q := &armedQueryer{armed: true}
	w := &SupplyChainImpactFactWriteStopWatch{DB: q, Now: func() time.Time { return clock }}
	for i := 0; i < 3; i++ {
		armed, err := w.Armed(context.Background())
		if err != nil || !armed {
			t.Fatalf("Armed() = %v, %v, want true", armed, err)
		}
	}
	if q.reads != 1 {
		t.Fatalf("reads inside the TTL = %d, want 1 (cached)", q.reads)
	}
	if len(q.names) != 1 || q.names[0] != supplyChainImpactFactWriteRejectConstraint {
		t.Fatalf("the read bound %v, want the trigger name %q", q.names, supplyChainImpactFactWriteRejectConstraint)
	}
	q.armed = false
	clock = clock.Add(31 * time.Second)
	if armed, err := w.Armed(context.Background()); err != nil || armed {
		t.Fatalf("Armed() after the TTL = %v, %v, want false (the trigger was disabled)", armed, err)
	}
	if q.reads != 2 {
		t.Fatalf("reads after the TTL = %d, want 2", q.reads)
	}
}

// TestFactWriteStopWatchDoesNotCacheAFailedRead pins that an unreadable trigger is an error and is
// retried, never a cached "armed" or a cached "not armed".
func TestFactWriteStopWatchDoesNotCacheAFailedRead(t *testing.T) {
	t.Parallel()

	q := &armedQueryer{err: errors.New("boom")}
	w := &SupplyChainImpactFactWriteStopWatch{DB: q}
	if _, err := w.Armed(context.Background()); err == nil {
		t.Fatal("Armed() on a failed read = nil error")
	}
	q.err, q.armed = nil, true
	if armed, err := w.Armed(context.Background()); err != nil || !armed {
		t.Fatalf("Armed() after the read recovered = %v, %v, want true: the failure must not be cached", armed, err)
	}
	if _, err := (&SupplyChainImpactFactWriteStopWatch{}).Armed(context.Background()); err == nil {
		t.Fatal("Armed() with no database = nil error")
	}
}

// TestFactWriteStopWatchRefreshIgnoresTheCacheAndExportsTheGauge pins the poller's call: Refresh reads
// now whatever the cache holds, and a nil Instruments records nothing without failing.
func TestFactWriteStopWatchRefreshIgnoresTheCacheAndExportsTheGauge(t *testing.T) {
	t.Parallel()

	inst, reader := conflictTestInstruments(t)
	q := &armedQueryer{armed: true}
	w := &SupplyChainImpactFactWriteStopWatch{DB: q, Instruments: inst}
	if _, err := w.Armed(context.Background()); err != nil {
		t.Fatal(err)
	}
	q.armed = false
	if armed, err := w.Refresh(context.Background()); err != nil || armed {
		t.Fatalf("Refresh() = %v, %v, want false", armed, err)
	}
	if q.reads != 2 {
		t.Fatalf("reads = %d, want 2: Refresh must not use the cache", q.reads)
	}
	rm := collectConflicts(t, reader)
	found := false
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name == "eshu_dp_supply_chain_impact_fact_write_stop_armed" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("the armed gauge was not exported")
	}
	if _, err := (&SupplyChainImpactFactWriteStopWatch{DB: &armedQueryer{armed: true}}).Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh() with no instruments = %v, want nil", err)
	}
}

// TestFanOutGateDefersWhileTheTriggerIsNotArmed is the seeded RED/GREEN pair of the armed check: an
// enabled trigger passes, a disabled or missing one returns the typed non-counting refusal under the
// fact_write_stop reader, an unreadable one is an error (not an armed trigger), and no watch skips
// the check.
func TestFanOutGateDefersWhileTheTriggerIsNotArmed(t *testing.T) {
	t.Parallel()

	armed := &armedQueryer{armed: true}
	store := SupplyChainImpactFanOutStore{FactWriteStop: &SupplyChainImpactFactWriteStopWatch{DB: armed}}
	if err := store.requireFactWriteStopArmed(context.Background()); err != nil {
		t.Fatalf("an enabled trigger was refused: %v", err)
	}

	disarmed := &armedQueryer{armed: false}
	store = SupplyChainImpactFanOutStore{FactWriteStop: &SupplyChainImpactFactWriteStopWatch{DB: disarmed}}
	err := store.requireFactWriteStopArmed(context.Background())
	var notReady *supplychaincore.SupplyChainImpactSupportsNotReadyError
	if !errors.As(err, &notReady) || notReady.Reader != telemetry.SupplyChainImpactSupportsReaderFactWriteStop {
		t.Fatalf("a disabled trigger gave %v, want the not-ready refusal under reader %q", err, telemetry.SupplyChainImpactSupportsReaderFactWriteStop)
	}

	broken := &armedQueryer{err: errors.New("boom")}
	store = SupplyChainImpactFanOutStore{FactWriteStop: &SupplyChainImpactFactWriteStopWatch{DB: broken}}
	if err := store.requireFactWriteStopArmed(context.Background()); err == nil || errors.As(err, &notReady) {
		t.Fatalf("an unreadable trigger gave %v, want a plain error (not a not-ready deferral)", err)
	}

	if err := (SupplyChainImpactFanOutStore{}).requireFactWriteStopArmed(context.Background()); err != nil {
		t.Fatalf("no watch must skip the check, got %v", err)
	}
}
