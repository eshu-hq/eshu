// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"bytes"
	"context"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

type fakeKeyedRebuilder struct {
	fakeWinnersRebuilder
	mu     sync.Mutex
	keyed  int
	result SupplyChainImpactWinnersResweep
}

func (f *fakeKeyedRebuilder) ResweepWinnersKeyed(_ context.Context, _ any) (SupplyChainImpactWinnersResweep, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.keyed++
	return f.result, nil
}

func (f *fakeKeyedRebuilder) keyedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.keyed
}

type fakeWinnersAuditor struct {
	mu     sync.Mutex
	calls  int
	result SupplyChainImpactWinnersAudit
}

func (f *fakeWinnersAuditor) AuditWinners(context.Context) (SupplyChainImpactWinnersAudit, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.result, nil
}

func (f *fakeWinnersAuditor) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type fakeDirtyStats struct {
	stats SupplyChainImpactWinnersDirtyStats
}

func (f fakeDirtyStats) DirtyLogStats(context.Context) (SupplyChainImpactWinnersDirtyStats, error) {
	return f.stats, nil
}

// TestWinnersMaintainerRunsOneFullResweepThenKeyedTicks proves the startup
// reconcile stays a full resweep and every later tick is keyed.
func TestWinnersMaintainerRunsOneFullResweepThenKeyedTicks(t *testing.T) {
	t.Parallel()

	rebuilder := &fakeKeyedRebuilder{}
	lease := &fakeWinnersLeaseManager{claim: true}
	m := SupplyChainImpactWinnersMaintainer{
		Rebuilder: rebuilder, KeyedRebuilder: rebuilder, LeaseManager: lease,
		Interval: time.Millisecond,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- m.Run(ctx) }()
	deadline := time.After(5 * time.Second)
	for rebuilder.keyedCount() < 3 {
		select {
		case <-deadline:
			cancel()
			t.Fatalf("keyed ticks = %d, want at least 3", rebuilder.keyedCount())
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	<-done
	if got := rebuilder.count(); got != 1 {
		t.Fatalf("full resweeps = %d, want exactly the startup reconcile", got)
	}
}

// TestWinnersMaintainerRunAuditsOnItsOwnCadence proves the audit runs beside
// the keyed ticks and never in place of one.
func TestWinnersMaintainerRunAuditsOnItsOwnCadence(t *testing.T) {
	t.Parallel()

	rebuilder := &fakeKeyedRebuilder{}
	auditor := &fakeWinnersAuditor{}
	lease := &fakeWinnersLeaseManager{claim: true}
	m := SupplyChainImpactWinnersMaintainer{
		Rebuilder: rebuilder, KeyedRebuilder: rebuilder, LeaseManager: lease,
		Auditor: auditor, AuditInterval: time.Millisecond, Interval: time.Millisecond,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- m.Run(ctx) }()
	deadline := time.After(5 * time.Second)
	for auditor.count() < 2 || rebuilder.keyedCount() < 2 {
		select {
		case <-deadline:
			cancel()
			t.Fatalf("audits = %d keyed = %d, want at least 2 of each", auditor.count(), rebuilder.keyedCount())
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	<-done
}

// TestWinnersMaintainerNoAuditWhenIntervalIsZero pins the off switch.
func TestWinnersMaintainerNoAuditWhenIntervalIsZero(t *testing.T) {
	t.Parallel()

	rebuilder := &fakeKeyedRebuilder{}
	auditor := &fakeWinnersAuditor{}
	m := SupplyChainImpactWinnersMaintainer{
		Rebuilder: rebuilder, KeyedRebuilder: rebuilder, LeaseManager: &fakeWinnersLeaseManager{claim: true},
		Auditor: auditor, Interval: time.Millisecond,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- m.Run(ctx) }()
	for rebuilder.keyedCount() < 3 {
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done
	if auditor.count() != 0 {
		t.Fatalf("audits = %d, want 0 with AuditInterval unset", auditor.count())
	}
}

type keyedMetrics struct {
	durationByPath map[string]uint64
	keys           uint64
	keySum         int64
	identifiers    int64
	captureBySrc   map[string]int64
	repairs        int64
	depth          []float64
	oldestAge      []float64
}

func collectKeyedMetrics(t *testing.T, reader *sdkmetric.ManualReader) keyedMetrics {
	t.Helper()
	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	out := keyedMetrics{durationByPath: map[string]uint64{}, captureBySrc: map[string]int64{}}
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			switch data := m.Data.(type) {
			case metricdata.Histogram[float64]:
				if m.Name != "eshu_dp_supply_chain_impact_winners_resweep_duration_seconds" {
					continue
				}
				for _, dp := range data.DataPoints {
					path, _ := dp.Attributes.Value(attribute.Key(telemetry.MetricDimensionPath))
					out.durationByPath[path.AsString()] += dp.Count
				}
			case metricdata.Histogram[int64]:
				if m.Name != "eshu_dp_supply_chain_impact_winners_keys_per_sweep" {
					continue
				}
				for _, dp := range data.DataPoints {
					out.keys += dp.Count
					out.keySum += dp.Sum
				}
			case metricdata.Sum[int64]:
				for _, dp := range data.DataPoints {
					switch m.Name {
					case "eshu_dp_supply_chain_impact_winners_identifiers_consumed_total":
						out.identifiers += dp.Value
					case "eshu_dp_supply_chain_impact_winners_backstop_repairs_total":
						out.repairs += dp.Value
					case "eshu_dp_supply_chain_impact_winners_capture_rows_total":
						src, _ := dp.Attributes.Value(attribute.Key(telemetry.MetricDimensionSource))
						out.captureBySrc[src.AsString()] += dp.Value
					}
				}
			case metricdata.Gauge[float64]:
				for _, dp := range data.DataPoints {
					switch m.Name {
					case "eshu_dp_supply_chain_impact_winners_dirty_log_depth":
						out.depth = append(out.depth, dp.Value)
					case "eshu_dp_supply_chain_impact_winners_dirty_log_oldest_age_seconds":
						out.oldestAge = append(out.oldestAge, dp.Value)
					}
				}
			}
		}
	}
	return out
}

// TestWinnersMaintainerRecordsKeyedAndAuditSignals proves the ruling's 3 AM
// signals: duration with a path label, keys per sweep, identifiers consumed,
// capture rows by source, backstop repairs with a warn log, and the dirty-log
// gauges sampled every tick.
func TestWinnersMaintainerRecordsKeyedAndAuditSignals(t *testing.T) {
	t.Parallel()

	inst, reader := winnersSignalsHarness(t)
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	rebuilder := &fakeKeyedRebuilder{result: SupplyChainImpactWinnersResweep{
		Path: telemetry.SupplyChainImpactWinnersPathKeyed, Identifiers: 12, Keys: 15,
		Updated: 3, Unchanged: 12,
		TriggerCaptured: WinnersTriggerCaptured{Activation: 4, Operator: 1, Scanner: 2},
	}}
	auditor := &fakeWinnersAuditor{result: SupplyChainImpactWinnersAudit{Repairs: 2}}
	m := SupplyChainImpactWinnersMaintainer{
		Rebuilder: rebuilder, KeyedRebuilder: rebuilder, LeaseManager: &fakeWinnersLeaseManager{claim: true},
		Auditor: auditor, AuditInterval: time.Hour,
		DirtyLogReader: fakeDirtyStats{stats: SupplyChainImpactWinnersDirtyStats{Depth: 40, OldestAge: 17 * time.Second}},
		Instruments:    inst, Logger: logger,
	}

	if _, err := m.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if _, err := m.runKeyedTick(context.Background()); err != nil {
		t.Fatalf("runKeyedTick() error = %v", err)
	}
	if err := m.runAuditOnce(context.Background()); err != nil {
		t.Fatalf("runAuditOnce() error = %v", err)
	}

	got := collectKeyedMetrics(t, reader)
	if got.durationByPath[telemetry.SupplyChainImpactWinnersPathFull] != 1 || got.durationByPath[telemetry.SupplyChainImpactWinnersPathKeyed] != 1 {
		t.Errorf("duration samples by path = %v, want one full and one keyed", got.durationByPath)
	}
	if got.keys != 1 || got.keySum != 15 {
		t.Errorf("keys_per_sweep count=%d sum=%d, want one sample of 15", got.keys, got.keySum)
	}
	if got.identifiers != 12 {
		t.Errorf("identifiers consumed = %d, want 12", got.identifiers)
	}
	want := map[string]int64{
		telemetry.SupplyChainImpactWinnersSourceActivation: 4,
		telemetry.SupplyChainImpactWinnersSourceOperator:   1,
		telemetry.SupplyChainImpactWinnersSourceScanner:    2,
		telemetry.SupplyChainImpactWinnersSourceAudit:      2,
	}
	if !reflect.DeepEqual(got.captureBySrc, want) {
		t.Errorf("capture rows by source = %v, want %v: the maintainer counts the trigger sources it consumed and the audit's own rows; fact_upsert and fact_retract are the writers' to count", got.captureBySrc, want)
	}
	if got.repairs != 2 {
		t.Errorf("backstop repairs = %d, want 2", got.repairs)
	}
	if len(got.depth) == 0 || got.depth[len(got.depth)-1] != 40 || len(got.oldestAge) == 0 || got.oldestAge[len(got.oldestAge)-1] != 17 {
		t.Errorf("dirty-log gauges depth=%v oldest=%v, want 40 and 17", got.depth, got.oldestAge)
	}
	if !strings.Contains(logs.String(), "supply chain impact winners audit found stale keys") || !strings.Contains(logs.String(), "repairs=2") {
		t.Errorf("warn log missing or without the count:\n%s", logs.String())
	}
	if strings.Contains(logs.String(), "CVE-") || strings.Contains(logs.String(), "scope") {
		t.Errorf("log carries a key, scope or CVE:\n%s", logs.String())
	}
}

// TestWinnersMaintainerCountsTriggerSourcesOnAFullFallback pins that a keyed
// tick that fell back to the full rebuild past the cap still consumed its
// dirty-log rows, so the trigger sources are counted on that path too, while the
// keyed-only signals (keys per sweep, identifiers) are not.
func TestWinnersMaintainerCountsTriggerSourcesOnAFullFallback(t *testing.T) {
	t.Parallel()

	inst, reader := winnersSignalsHarness(t)
	rebuilder := &fakeKeyedRebuilder{result: SupplyChainImpactWinnersResweep{
		Path: telemetry.SupplyChainImpactWinnersPathFull, Keys: 99, Identifiers: 99,
		TriggerCaptured: WinnersTriggerCaptured{Activation: 7},
	}}
	m := SupplyChainImpactWinnersMaintainer{
		Rebuilder: rebuilder, KeyedRebuilder: rebuilder, LeaseManager: &fakeWinnersLeaseManager{claim: true},
		Instruments: inst,
	}
	if _, err := m.runKeyedTick(context.Background()); err != nil {
		t.Fatalf("runKeyedTick() error = %v", err)
	}
	got := collectKeyedMetrics(t, reader)
	if got.captureBySrc[telemetry.SupplyChainImpactWinnersSourceActivation] != 7 {
		t.Errorf("activation capture rows = %v, want 7 counted on the full fallback", got.captureBySrc)
	}
	if got.keys != 0 || got.identifiers != 0 {
		t.Errorf("keys_per_sweep count=%d identifiers=%d, want none on the full path", got.keys, got.identifiers)
	}
}

// TestWinnersMaintainerAuditStaysQuietWhenNothingIsStale pins that a clean
// audit raises no repair sample and no warn log.
func TestWinnersMaintainerAuditStaysQuietWhenNothingIsStale(t *testing.T) {
	t.Parallel()

	inst, reader := winnersSignalsHarness(t)
	var logs bytes.Buffer
	rebuilder := &fakeKeyedRebuilder{}
	m := SupplyChainImpactWinnersMaintainer{
		Rebuilder: rebuilder, KeyedRebuilder: rebuilder, LeaseManager: &fakeWinnersLeaseManager{claim: true},
		Auditor: &fakeWinnersAuditor{}, Instruments: inst,
		Logger: slog.New(slog.NewTextHandler(&logs, nil)),
	}
	if err := m.runAuditOnce(context.Background()); err != nil {
		t.Fatalf("runAuditOnce() error = %v", err)
	}
	if got := collectKeyedMetrics(t, reader); got.repairs != 0 {
		t.Errorf("repairs = %d, want 0", got.repairs)
	}
	if strings.Contains(logs.String(), "stale keys") {
		t.Errorf("clean audit logged a stale-key warning:\n%s", logs.String())
	}
}
