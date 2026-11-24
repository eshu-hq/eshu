// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// The winners maintainer reads the supports table from C1 on (#10880 PR-C1 d), so
// it must not run while the table is not ready (R2 of the C1 arbiter ruling): a
// not-ready tick writes nothing, leaves the watermark alone and is counted; the
// first ready tick after a not-ready period runs one full. The decision comes from
// durable state, never from process memory: the fake below stands in for the two
// durable rows the Postgres gate compares, the supports marker's completion stamp
// and the winners watermark.

// durableWinners is the durable state shared by every maintainer instance of a
// test, so a "restart" is a new maintainer value over the same state.
type durableWinners struct {
	mu          sync.Mutex
	ready       bool
	gateErr     error
	markerStamp int // supports marker completed_at, as a logical clock
	watermark   int // winners watermark, as the same logical clock
	clock       int
	fulls       int
	keyed       int
	writes      int
}

func (d *durableWinners) WinnersSupportsGate(context.Context) (SupplyChainImpactWinnersSupportsGate, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.gateErr != nil {
		return SupplyChainImpactWinnersSupportsGate{}, d.gateErr
	}
	return SupplyChainImpactWinnersSupportsGate{Ready: d.ready, FullNeeded: d.ready && d.markerStamp > d.watermark}, nil
}

func (d *durableWinners) ResweepWinners(context.Context) (SupplyChainImpactWinnersResweep, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.fulls++
	d.writes++
	// The logical clock is shared by both stamps, as the database clock is: a full
	// stamps a watermark later than everything committed before it.
	d.clock = max(d.clock, d.markerStamp, d.watermark) + 1
	d.watermark = d.clock
	return SupplyChainImpactWinnersResweep{Path: telemetry.SupplyChainImpactWinnersPathFull, Watermark: time.Unix(int64(d.clock), 0)}, nil
}

func (d *durableWinners) ResweepWinnersKeyed(context.Context) (SupplyChainImpactWinnersResweep, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.keyed++
	return SupplyChainImpactWinnersResweep{Path: telemetry.SupplyChainImpactWinnersPathKeyed}, nil
}

// WinnersContract reports a stored contract equal to the binary's, so only the
// supports gate can ask for a full.
func (d *durableWinners) WinnersContract(context.Context) (SupplyChainImpactWinnersContract, error) {
	return SupplyChainImpactWinnersContract{
		Present: true, HasVersion: true, StoredVersion: 2, StoredHash: "h", BinaryVersion: 2, BinaryHash: "h",
	}, nil
}

func (d *durableWinners) ResweepWinnersUnlessCurrent(ctx context.Context) (SupplyChainImpactWinnersResweep, error) {
	return SupplyChainImpactWinnersResweep{Skipped: true, ContractCurrent: true}, nil
}

func (d *durableWinners) state() (fulls, keyed, writes, watermark int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.fulls, d.keyed, d.writes, d.watermark
}

func gatedMaintainer(d *durableWinners, lease *fakeWinnersLeaseManager, in *telemetry.Instruments) SupplyChainImpactWinnersMaintainer {
	return SupplyChainImpactWinnersMaintainer{
		Rebuilder: d, KeyedRebuilder: d, Contract: d, SupportsGate: d, LeaseManager: lease, Instruments: in,
		Interval: time.Millisecond,
	}
}

func supportsNotReadyCounts(t *testing.T, reader *sdkmetric.ManualReader) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	out := map[string]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_supply_chain_impact_winners_supports_not_ready_total" {
				continue
			}
			for _, point := range m.Data.(metricdata.Sum[int64]).DataPoints {
				path, _ := point.Attributes.Value("path")
				out[path.AsString()] += point.Value
			}
		}
	}
	return out
}

func newGateInstruments(t *testing.T) (*telemetry.Instruments, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	in, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	return in, reader
}

// TestWinnersNotReadyTickWritesNothingAndKeepsTheWatermark: while the supports
// table is not ready no tick claims the lease, runs a full or a keyed resweep, or
// moves the watermark, and every refused tick is counted on its path.
func TestWinnersNotReadyTickWritesNothingAndKeepsTheWatermark(t *testing.T) {
	t.Parallel()

	d := &durableWinners{ready: false, markerStamp: 0, watermark: 7}
	lease := &fakeWinnersLeaseManager{claim: true}
	in, reader := newGateInstruments(t)
	m := gatedMaintainer(d, lease, in)

	for i := 0; i < 3; i++ {
		res, _, _, err := m.runContractTick(context.Background())
		if err != nil {
			t.Fatalf("not-ready tick error = %v", err)
		}
		if !res.SupportsNotReady {
			t.Fatalf("tick %d did not report the supports gate", i)
		}
	}
	if _, err := m.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce error = %v", err)
	}
	fulls, keyed, writes, watermark := d.state()
	if fulls != 0 || keyed != 0 || writes != 0 || watermark != 7 {
		t.Fatalf("a not-ready period ran fulls=%d keyed=%d writes=%d, watermark=%d; want nothing and watermark 7", fulls, keyed, writes, watermark)
	}
	if lease.claims != 0 {
		t.Fatalf("a not-ready tick claimed the resweep lease %d times, want 0", lease.claims)
	}
	if got := supportsNotReadyCounts(t, reader); got[telemetry.SupplyChainImpactWinnersPathKeyed] != 3 || got[telemetry.SupplyChainImpactWinnersPathFull] != 1 {
		t.Fatalf("not-ready counter = %v, want keyed 3 and full 1", got)
	}
}

// TestWinnersFirstReadyTickAfterNotReadyRunsExactlyOneFull: the marker completes
// after the watermark, so the first ready tick runs one full and every later tick
// is keyed.
func TestWinnersFirstReadyTickAfterNotReadyRunsExactlyOneFull(t *testing.T) {
	t.Parallel()

	d := &durableWinners{ready: false, watermark: 3}
	m := gatedMaintainer(d, &fakeWinnersLeaseManager{claim: true}, nil)
	for i := 0; i < 2; i++ {
		if _, _, _, err := m.runContractTick(context.Background()); err != nil {
			t.Fatalf("not-ready tick: %v", err)
		}
	}
	d.mu.Lock()
	d.ready, d.markerStamp = true, 5 // the backfill completes after the last committed resweep
	d.mu.Unlock()

	for i := 0; i < 4; i++ {
		if _, _, _, err := m.runContractTick(context.Background()); err != nil {
			t.Fatalf("ready tick %d: %v", i, err)
		}
	}
	fulls, keyed, _, watermark := d.state()
	if fulls != 1 || keyed != 3 {
		t.Fatalf("after becoming ready: fulls=%d keyed=%d, want exactly one full then 3 keyed", fulls, keyed)
	}
	if watermark <= 5 {
		t.Fatalf("watermark %d did not move past the marker stamp 5", watermark)
	}
}

// TestWinnersFullAfterNotReadySurvivesARestart: the decision is durable. A new
// maintainer over the same state, as after a restart between the not-ready period
// and the first ready tick, still runs the one full, and a second new maintainer
// after that full runs none.
func TestWinnersFullAfterNotReadySurvivesARestart(t *testing.T) {
	t.Parallel()

	d := &durableWinners{ready: false, watermark: 3}
	if _, _, _, err := gatedMaintainer(d, &fakeWinnersLeaseManager{claim: true}, nil).runContractTick(context.Background()); err != nil {
		t.Fatal(err)
	}
	d.mu.Lock()
	d.ready, d.markerStamp = true, 5
	d.mu.Unlock()

	// A fresh maintainer (a restart) decides from the durable state alone.
	if _, _, _, err := gatedMaintainer(d, &fakeWinnersLeaseManager{claim: true}, nil).runContractTick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fulls, _, _, _ := d.state(); fulls != 1 {
		t.Fatalf("fulls after the restart = %d, want 1", fulls)
	}
	if _, _, _, err := gatedMaintainer(d, &fakeWinnersLeaseManager{claim: true}, nil).runContractTick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fulls, keyed, _, _ := d.state(); fulls != 1 || keyed != 1 {
		t.Fatalf("after a second restart: fulls=%d keyed=%d, want the one full and one keyed tick", fulls, keyed)
	}
}

// TestWinnersReadyFromTheStartRunsNoExtraFull: a table that was ready and
// older than the watermark runs keyed ticks only.
func TestWinnersReadyFromTheStartRunsNoExtraFull(t *testing.T) {
	t.Parallel()

	d := &durableWinners{ready: true, markerStamp: 2, watermark: 9}
	m := gatedMaintainer(d, &fakeWinnersLeaseManager{claim: true}, nil)
	for i := 0; i < 3; i++ {
		if _, _, _, err := m.runContractTick(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if fulls, keyed, _, _ := d.state(); fulls != 0 || keyed != 3 {
		t.Fatalf("fulls=%d keyed=%d, want 0 fulls and 3 keyed", fulls, keyed)
	}
}

// TestWinnersAuditWaitsForSupportsAndAProbeErrorWritesNothing: the audit reads
// the maintainer's inputs too, so it waits for readiness; a failed gate read is an
// error, not a pass, and writes nothing.
func TestWinnersAuditWaitsForSupportsAndAProbeErrorWritesNothing(t *testing.T) {
	t.Parallel()

	d := &durableWinners{ready: false}
	m := gatedMaintainer(d, &fakeWinnersLeaseManager{claim: true}, nil)
	if m.auditAllowed(context.Background()) {
		t.Fatal("the audit is allowed while the supports table is not ready")
	}
	d.mu.Lock()
	d.ready = true
	d.mu.Unlock()
	if !m.auditAllowed(context.Background()) {
		t.Fatal("the audit is refused while the supports table is ready and the contract current")
	}

	d.mu.Lock()
	d.gateErr = errors.New("probe failed")
	d.mu.Unlock()
	if _, _, _, err := m.runContractTick(context.Background()); err == nil {
		t.Fatal("a failed readiness probe did not surface as an error")
	}
	if m.auditAllowed(context.Background()) {
		t.Fatal("the audit is allowed after a failed readiness probe")
	}
	if fulls, keyed, writes, _ := d.state(); fulls != 0 || keyed != 0 || writes != 0 {
		t.Fatalf("a probe error ran fulls=%d keyed=%d writes=%d", fulls, keyed, writes)
	}
}
