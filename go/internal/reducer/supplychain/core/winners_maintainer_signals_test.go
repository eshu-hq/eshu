// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func winnersSignalsHarness(t *testing.T) (*telemetry.Instruments, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	return inst, reader
}

// winnersMetrics flattens the winners metrics: row counts by action, the
// number of duration samples, and the watermark-age sample, if any.
func winnersMetrics(t *testing.T, reader *sdkmetric.ManualReader) (rows map[string]int64, durations uint64, ages []float64) {
	t.Helper()
	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	rows = map[string]int64{}
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			switch data := m.Data.(type) {
			case metricdata.Sum[int64]:
				if m.Name == "eshu_dp_supply_chain_impact_winners_resweep_skipped_total" {
					for _, dp := range data.DataPoints {
						rows["skipped"] += dp.Value
					}
					continue
				}
				if m.Name != "eshu_dp_supply_chain_impact_winners_rows_total" {
					continue
				}
				for _, dp := range data.DataPoints {
					action, _ := dp.Attributes.Value(attribute.Key(telemetry.MetricDimensionAction))
					rows[action.AsString()] += dp.Value
				}
			case metricdata.Histogram[float64]:
				if m.Name != "eshu_dp_supply_chain_impact_winners_resweep_duration_seconds" {
					continue
				}
				for _, dp := range data.DataPoints {
					durations += dp.Count
				}
			case metricdata.Gauge[float64]:
				if m.Name != "eshu_dp_supply_chain_impact_winners_watermark_age_seconds" {
					continue
				}
				for _, dp := range data.DataPoints {
					ages = append(ages, dp.Value)
				}
			}
		}
	}
	return rows, durations, ages
}

func TestWinnersMaintainerRecordsResweepSignals(t *testing.T) {
	t.Parallel()

	inst, reader := winnersSignalsHarness(t)
	stamp := time.Now().Add(-31 * time.Second)
	rebuilder := &fakeWinnersRebuilder{result: SupplyChainImpactWinnersResweep{
		Inserted: 2, Updated: 3, Deleted: 5, Unchanged: 7,
		PreviousWatermark: stamp.Add(-31 * time.Second),
	}}
	var logs bytes.Buffer
	spans := tracetest.NewSpanRecorder()
	m := SupplyChainImpactWinnersMaintainer{
		Rebuilder:    rebuilder,
		LeaseManager: &fakeWinnersLeaseManager{claim: true},
		Now:          func() time.Time { return stamp },
		Instruments:  inst,
		Tracer:       sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans)).Tracer("test"),
		Logger:       slog.New(slog.NewJSONHandler(&logs, nil)),
	}

	if _, err := m.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}

	rows, durations, ages := winnersMetrics(t, reader)
	for action, want := range map[string]int64{"inserted": 2, "updated": 3, "deleted": 5, "unchanged": 7} {
		if rows[action] != want {
			t.Errorf("rows_total{action=%s} = %d, want %d (all: %v)", action, rows[action], want, rows)
		}
	}
	if durations != 1 {
		t.Errorf("resweep duration samples = %d, want 1", durations)
	}
	if len(ages) != 1 || ages[0] < 31 || ages[0] > 60 {
		t.Errorf("watermark age samples = %v, want the age of the committed stamp, about 31 s", ages)
	}

	ended := spans.Ended()
	if len(ended) != 1 || ended[0].Name() != telemetry.SpanSupplyChainImpactWinnersResweep {
		t.Fatalf("spans = %v, want one %s span", ended, telemetry.SpanSupplyChainImpactWinnersResweep)
	}
	attrs := map[string]int64{}
	for _, kv := range ended[0].Attributes() {
		attrs[string(kv.Key)] = kv.Value.AsInt64()
	}
	if attrs["inserted"] != 2 || attrs["updated"] != 3 || attrs["deleted"] != 5 || attrs["unchanged"] != 7 {
		t.Errorf("span attributes = %v, want the four row counts", attrs)
	}
	line := logs.String()
	for _, want := range []string{
		`"msg":"supply chain impact winners resweep committed"`, `"level":"INFO"`,
		`"inserted":2`, `"updated":3`, `"deleted":5`, `"unchanged":7`, `"duration_seconds"`,
	} {
		if !strings.Contains(line, want) {
			t.Errorf("log line %q is missing %s", line, want)
		}
	}
}

// TestWinnersMaintainerSkipsZeroCounts pins that an unchanged resweep adds only
// the unchanged count, and that the first committed resweep starts the age gauge.
func TestWinnersMaintainerSkipsZeroCounts(t *testing.T) {
	t.Parallel()

	inst, reader := winnersSignalsHarness(t)
	m := SupplyChainImpactWinnersMaintainer{
		Rebuilder:    &fakeWinnersRebuilder{result: SupplyChainImpactWinnersResweep{Unchanged: 4}},
		LeaseManager: &fakeWinnersLeaseManager{claim: true},
		Instruments:  inst,
	}
	if _, err := m.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	rows, durations, ages := winnersMetrics(t, reader)
	if len(rows) != 1 || rows["unchanged"] != 4 {
		t.Errorf("rows_total = %v, want only unchanged=4", rows)
	}
	if durations != 1 {
		t.Errorf("duration samples = %d, want 1", durations)
	}
	if len(ages) != 1 {
		t.Errorf("watermark age samples = %v, want one once a resweep committed", ages)
	}
}

// fakeWinnersWatermark is a scripted watermark reader.
type fakeWinnersWatermark struct {
	at    time.Time
	ok    bool
	err   error
	reads int
}

func (f *fakeWinnersWatermark) Watermark(context.Context) (time.Time, bool, error) {
	f.reads++
	return f.at, f.ok, f.err
}

// TestWinnersGaugeClimbsThroughAStall pins the signal the 3 AM page depends on:
// on every tick that commits nothing, whatever the reason, the gauge reports the
// age of the durable watermark, and a later scrape reads older with no tick at
// all. A gauge written only on commit would stay at its last healthy value here.
func TestWinnersGaugeClimbsThroughAStall(t *testing.T) {
	t.Parallel()

	stalledFor := 10 * time.Minute
	for name, tick := range map[string]struct {
		lease     *fakeWinnersLeaseManager
		rebuilder *fakeWinnersRebuilder
		wantErr   bool
	}{
		"resweep fails":     {&fakeWinnersLeaseManager{claim: true}, &fakeWinnersRebuilder{err: errors.New("boom")}, true},
		"lease not held":    {&fakeWinnersLeaseManager{claim: false}, &fakeWinnersRebuilder{}, false},
		"lease claim fails": {&fakeWinnersLeaseManager{claimErr: errors.New("boom")}, &fakeWinnersRebuilder{}, true},
		"resweep skipped":   {&fakeWinnersLeaseManager{claim: true}, &fakeWinnersRebuilder{result: SupplyChainImpactWinnersResweep{Skipped: true}}, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			inst, reader := winnersSignalsHarness(t)
			durable := &fakeWinnersWatermark{at: time.Now().Add(-stalledFor), ok: true}
			m := SupplyChainImpactWinnersMaintainer{
				Rebuilder: tick.rebuilder, LeaseManager: tick.lease, WatermarkReader: durable, Instruments: inst,
			}
			if _, err := m.RunOnce(context.Background()); (err != nil) != tick.wantErr {
				t.Fatalf("RunOnce() error = %v, want error %v", err, tick.wantErr)
			}
			_, _, first := winnersMetrics(t, reader)
			if len(first) != 1 || first[0] < stalledFor.Seconds() {
				t.Fatalf("age after the tick = %v, want at least %v", first, stalledFor.Seconds())
			}
			time.Sleep(20 * time.Millisecond)
			_, _, second := winnersMetrics(t, reader)
			if len(second) != 1 || second[0] <= first[0] {
				t.Fatalf("age on a later scrape = %v after %v, want it to keep climbing", second, first)
			}
		})
	}
}

// TestWinnersGaugeSampledBeforeTheResweepRuns pins that a statement that never
// returns still leaves the gauge a watermark to age: the read happens first.
func TestWinnersGaugeSampledBeforeTheResweepRuns(t *testing.T) {
	t.Parallel()

	inst, reader := winnersSignalsHarness(t)
	durable := &fakeWinnersWatermark{at: time.Now().Add(-time.Hour), ok: true}
	var agesDuringResweep []float64
	m := SupplyChainImpactWinnersMaintainer{
		Rebuilder: winnersRebuilderFunc(func() {
			_, _, agesDuringResweep = winnersMetrics(t, reader)
		}),
		LeaseManager: &fakeWinnersLeaseManager{claim: true}, WatermarkReader: durable, Instruments: inst,
	}
	if _, err := m.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if len(agesDuringResweep) != 1 || agesDuringResweep[0] < time.Hour.Seconds() {
		t.Fatalf("age while the resweep ran = %v, want at least an hour", agesDuringResweep)
	}
}

type winnersRebuilderFunc func()

func (f winnersRebuilderFunc) ResweepWinners(context.Context, any) (SupplyChainImpactWinnersResweep, error) {
	f()
	return SupplyChainImpactWinnersResweep{}, nil
}

// TestWinnersGaugeSurvivesAFailedWatermarkRead pins that a failed read neither
// stops the tick nor touches the gauge: with nothing committing, the gauge must
// still age the last watermark it saw, and the failure is logged.
func TestWinnersGaugeSurvivesAFailedWatermarkRead(t *testing.T) {
	t.Parallel()

	inst, reader := winnersSignalsHarness(t)
	lastSeen := 5 * time.Minute
	inst.RecordSupplyChainImpactWinnersWatermark(time.Now().Add(-lastSeen))
	var logs bytes.Buffer
	failing := &fakeWinnersWatermark{err: errors.New("read failed")}
	m := SupplyChainImpactWinnersMaintainer{
		Rebuilder:       &fakeWinnersRebuilder{},
		LeaseManager:    &fakeWinnersLeaseManager{claim: false},
		WatermarkReader: failing,
		Instruments:     inst,
		Logger:          slog.New(slog.NewJSONHandler(&logs, nil)),
	}
	start := time.Now()
	_, _, before := winnersMetrics(t, reader)
	if len(before) != 1 || before[0] < lastSeen.Seconds() {
		t.Fatalf("age before the failed read = %v, want at least %v", before, lastSeen.Seconds())
	}
	if _, err := m.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	_, _, after := winnersMetrics(t, reader)
	elapsed := time.Since(start).Seconds()
	if len(after) != 1 {
		t.Fatalf("age after the failed read = %v, want the last watermark still reporting", after)
	}
	// The gauge recomputes the age at each scrape, so the same value here means
	// the same watermark: two ages differ only by the time between the scrapes.
	// The second of slack absorbs a wall-clock slew, which time.Since sees here.
	const slack = 1.0
	if moved := after[0] - before[0]; moved < -slack || moved > elapsed+slack {
		t.Errorf("age moved %v s across a failed read in %v s, want the same watermark", moved, elapsed)
	}
	time.Sleep(20 * time.Millisecond)
	if _, _, later := winnersMetrics(t, reader); len(later) != 1 || later[0] <= after[0] {
		t.Errorf("age on a later scrape = %v after %v, want it to keep climbing", later, after)
	}
	if !strings.Contains(logs.String(), "watermark read failed") {
		t.Errorf("logs = %q, want the watermark read failure", logs.String())
	}

	m.LeaseManager = &fakeWinnersLeaseManager{claim: true}
	if rebuilt, err := m.RunOnce(context.Background()); err != nil || !rebuilt {
		t.Fatalf("RunOnce() = %v, %v, want the resweep to run despite the failed read", rebuilt, err)
	}
}

// TestWinnersGaugeHasNoValueBeforeAWatermarkExists pins that a never-swept table
// reports nothing rather than an invented age.
func TestWinnersGaugeHasNoValueBeforeAWatermarkExists(t *testing.T) {
	t.Parallel()

	inst, reader := winnersSignalsHarness(t)
	m := SupplyChainImpactWinnersMaintainer{
		Rebuilder: &fakeWinnersRebuilder{}, LeaseManager: &fakeWinnersLeaseManager{claim: false},
		WatermarkReader: &fakeWinnersWatermark{ok: false}, Instruments: inst,
	}
	if _, err := m.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if _, _, ages := winnersMetrics(t, reader); len(ages) != 0 {
		t.Errorf("age = %v, want no sample before any watermark exists", ages)
	}
}

// TestWinnersMaintainerCountsASkippedResweep pins that a skip is a normal
// outcome: no error, no backoff, no commit signals, one skip count and a log.
func TestWinnersMaintainerCountsASkippedResweep(t *testing.T) {
	t.Parallel()

	inst, reader := winnersSignalsHarness(t)
	var logs bytes.Buffer
	spans := tracetest.NewSpanRecorder()
	m := SupplyChainImpactWinnersMaintainer{
		Rebuilder:    &fakeWinnersRebuilder{result: SupplyChainImpactWinnersResweep{Skipped: true}},
		LeaseManager: &fakeWinnersLeaseManager{claim: true},
		Instruments:  inst,
		Tracer:       sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans)).Tracer("test"),
		Logger:       slog.New(slog.NewJSONHandler(&logs, nil)),
	}
	rebuilt, err := m.RunOnce(context.Background())
	if err != nil || rebuilt {
		t.Fatalf("RunOnce() = %v, %v, want not rebuilt and no error", rebuilt, err)
	}
	rows, durations, ages := winnersMetrics(t, reader)
	if len(rows) != 1 || rows["skipped"] != 1 || durations != 0 || len(ages) != 0 {
		t.Errorf("rows=%v durations=%d ages=%v, want only skipped=1", rows, durations, ages)
	}
	if !strings.Contains(logs.String(), "resweep skipped") || strings.Contains(logs.String(), "committed") {
		t.Errorf("logs = %q, want the skip line and no commit line", logs.String())
	}
	if ended := spans.Ended(); len(ended) != 1 || ended[0].Attributes()[0].Key != "skipped" {
		t.Fatalf("spans = %v, want one span marked skipped", ended)
	}
}

// TestWinnersMaintainerRecordsNothingOnFailure pins that a failed resweep adds
// no duration sample or row count, and that its span carries the error.
func TestWinnersMaintainerRecordsNothingOnFailure(t *testing.T) {
	t.Parallel()

	inst, reader := winnersSignalsHarness(t)
	spans := tracetest.NewSpanRecorder()
	m := SupplyChainImpactWinnersMaintainer{
		Rebuilder:    &fakeWinnersRebuilder{err: errors.New("boom"), result: SupplyChainImpactWinnersResweep{Inserted: 9}},
		LeaseManager: &fakeWinnersLeaseManager{claim: true},
		Instruments:  inst,
		Tracer:       sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans)).Tracer("test"),
	}
	if _, err := m.RunOnce(context.Background()); err == nil {
		t.Fatal("RunOnce() error = nil, want the resweep error")
	}
	rows, durations, ages := winnersMetrics(t, reader)
	if len(rows) != 0 || durations != 0 || len(ages) != 0 {
		t.Errorf("failed resweep recorded rows=%v durations=%d ages=%v, want nothing", rows, durations, ages)
	}
	ended := spans.Ended()
	if len(ended) != 1 || len(ended[0].Events()) == 0 {
		t.Fatalf("spans = %v, want one span with a recorded error event", ended)
	}
}
