// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Horizon and drain-capacity proofs for #10706 (winners keyed resweep:
// watermark horizon + immediate re-tick on Bounded). A bounded tick stamps
// the horizon, so every committed tick refreshes the watermark gauge; a tick
// that leaves rows queued re-ticks immediately instead of waiting out the
// full cadence.

// TestWinnersMaintainerBoundedTickAdvancesTheWatermarkGauge pins the #10706
// flip: a bounded tick stamps the horizon, so the gauge advances on it. (It
// replaces TestWinnersMaintainerBoundedTickSkipsTheWatermarkGauge, which
// pinned the stamp-on-drain-only behavior the horizon removes.)
func TestWinnersMaintainerBoundedTickAdvancesTheWatermarkGauge(t *testing.T) {
	t.Parallel()

	inst, reader := winnersSignalsHarness(t)
	m := SupplyChainImpactWinnersMaintainer{
		Rebuilder: &fakeWinnersRebuilder{}, KeyedRebuilder: &fakeKeyedRebuilder{result: SupplyChainImpactWinnersResweep{
			Path: telemetry.SupplyChainImpactWinnersPathKeyed, Bounded: true, Remaining: 7,
			PreviousWatermark: time.Now().Add(-93 * time.Second),
		}}, LeaseManager: &fakeWinnersLeaseManager{claim: true},
		Now:         func() time.Time { return time.Now().Add(-62 * time.Second) },
		Instruments: inst,
	}
	if _, err := m.runKeyedTick(context.Background()); err != nil {
		t.Fatalf("runKeyedTick() error = %v", err)
	}
	if _, _, ages := winnersMetrics(t, reader); len(ages) != 1 || ages[0] < 31 || ages[0] > 120 {
		t.Errorf("watermark age samples = %v, want one of about 62 s: a bounded tick stamps its horizon", ages)
	}
}

// scriptedKeyedRebuilder replays one keyed result per tick in order, holding
// the last one once the script runs out.
type scriptedKeyedRebuilder struct {
	mu      sync.Mutex
	results []SupplyChainImpactWinnersResweep
	calls   int
}

func (f *scriptedKeyedRebuilder) ResweepWinnersKeyed(context.Context) (SupplyChainImpactWinnersResweep, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.calls > len(f.results) {
		return f.results[len(f.results)-1], nil
	}
	return f.results[f.calls-1], nil
}

func (f *scriptedKeyedRebuilder) keyedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// TestWinnersMaintainerImmediateRetickOnBounded proves the #10706 drain
// capacity: a keyed tick that leaves rows queued re-ticks without waiting
// out the cadence, while an idle startup full and a drained keyed tick keep
// the configured interval.
func TestWinnersMaintainerImmediateRetickOnBounded(t *testing.T) {
	t.Parallel()

	const interval = time.Minute
	keyed := &scriptedKeyedRebuilder{results: []SupplyChainImpactWinnersResweep{
		{Path: telemetry.SupplyChainImpactWinnersPathKeyed, Bounded: true, Remaining: 7},
		{Path: telemetry.SupplyChainImpactWinnersPathKeyed, Bounded: true, Remaining: 3},
		{Path: telemetry.SupplyChainImpactWinnersPathKeyed},
	}}
	var mu sync.Mutex
	var sleeps []time.Duration
	ctx, cancel := context.WithCancel(context.Background())
	m := SupplyChainImpactWinnersMaintainer{
		Rebuilder: &fakeWinnersRebuilder{}, KeyedRebuilder: keyed,
		LeaseManager: &fakeWinnersLeaseManager{claim: true}, Interval: interval,
		Sleep: func(ctx context.Context, d time.Duration) error {
			mu.Lock()
			sleeps = append(sleeps, d)
			n := len(sleeps)
			mu.Unlock()
			if n >= 4 {
				cancel()
				return ctx.Err()
			}
			return nil
		},
	}
	done := make(chan error, 1)
	go func() { done <- m.Run(ctx) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v, want nil on cancel", err)
		}
	case <-time.After(15 * time.Second):
		cancel()
		t.Fatal("Run did not reach four sleeps in 15 s")
	}
	mu.Lock()
	defer mu.Unlock()
	want := []time.Duration{interval, 0, 0, interval}
	if len(sleeps) != len(want) {
		t.Fatalf("sleeps = %v, want %v (interval after the startup full, immediate re-ticks on Bounded, interval after the drain)", sleeps, want)
	}
	for i, w := range want {
		if sleeps[i] != w {
			t.Errorf("sleep[%d] = %v, want %v", i, sleeps[i], w)
		}
	}
	if got := keyed.keyedCount(); got != 3 {
		t.Errorf("keyed ticks = %d, want the 3 scripted before the cancel", got)
	}
}

// TestWinnersMaintainerRunKeyedTickReportsDbCommittedStamp proves the
// maintainer records the watermark the tick committed (the store's
// DB-sourced stamp), not the Go clock: with a skewed Go clock the gauge age
// still tracks the committed stamp.
func TestWinnersMaintainerRunKeyedTickReportsDbCommittedStamp(t *testing.T) {
	t.Parallel()

	inst, reader := winnersSignalsHarness(t)
	committed := time.Now().Add(-31 * time.Second)
	m := SupplyChainImpactWinnersMaintainer{
		Rebuilder: &fakeWinnersRebuilder{}, KeyedRebuilder: &fakeKeyedRebuilder{result: SupplyChainImpactWinnersResweep{
			Path:              telemetry.SupplyChainImpactWinnersPathKeyed,
			PreviousWatermark: committed.Add(-31 * time.Second), Watermark: committed,
		}}, LeaseManager: &fakeWinnersLeaseManager{claim: true},
		Now:         func() time.Time { return time.Now().Add(time.Hour) },
		Instruments: inst,
	}
	if _, err := m.runKeyedTick(context.Background()); err != nil {
		t.Fatalf("runKeyedTick() error = %v", err)
	}
	if _, _, ages := winnersMetrics(t, reader); len(ages) != 1 || ages[0] < 31 || ages[0] > 120 {
		t.Errorf("watermark age samples = %v, want one of about 31 s from the committed stamp despite the skewed Go clock", ages)
	}
}
