// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func TestStableKeyFillConfigDefaultsAreOffAndWithinTheRulingLimits(t *testing.T) {
	t.Parallel()

	cfg := loadStableKeyFillConfig(func(string) string { return "" })
	if cfg.Enabled {
		t.Fatal("the fill is enabled by default: it must not run inside a counted window or before the lab run")
	}
	if cfg.Pace.BytesPerHour > 8_000_000_000 || cfg.Pace.BytesPerHour <= 0 {
		t.Fatalf("default WAL budget = %d bytes an hour, want at most the ruling's 8 GB", cfg.Pace.BytesPerHour)
	}
	if cfg.Limits.MaxReplayLag != 60*time.Second || cfg.Limits.MinFreeDiskBytes != 100_000_000_000 {
		t.Fatalf("default limits = %+v, want 60s replay lag and 100 GB free disk", cfg.Limits)
	}
	env := map[string]string{
		stableKeyFillEnabledEnv:      "true",
		stableKeyFillWALBytesHourEnv: "4000000000",
		stableKeyFillWALBytesRowEnv:  "2048",
		stableKeyFillMinFreeDiskEnv:  "0",
		stableKeyFillMaxReplayLagEnv: "30s",
	}
	cfg = loadStableKeyFillConfig(func(k string) string { return env[k] })
	if !cfg.Enabled || cfg.Pace.BytesPerHour != 4e9 || cfg.Pace.BytesPerRow != 2048 || cfg.Limits.MinFreeDiskBytes != 0 || cfg.Limits.MaxReplayLag != 30*time.Second {
		t.Fatalf("parsed config = %+v", cfg)
	}
	bad := map[string]string{stableKeyFillEnabledEnv: "maybe", stableKeyFillWALBytesHourEnv: "-1", stableKeyFillMaxReplayLagEnv: "soon"}
	cfg = loadStableKeyFillConfig(func(k string) string { return bad[k] })
	if cfg.Enabled || cfg.Pace.BytesPerHour != 8_000_000_000 || cfg.Limits.MaxReplayLag != 60*time.Second {
		t.Fatalf("unparsable values did not keep the defaults: %+v", cfg)
	}
}

func fillTestInstruments(t *testing.T) (*telemetry.Instruments, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	in, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatal(err)
	}
	return in, reader
}

func fillMetric(t *testing.T, reader *sdkmetric.ManualReader, name, label string) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatal(err)
	}
	var total int64
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != name {
				continue
			}
			switch data := m.Data.(type) {
			case metricdata.Sum[int64]:
				for _, p := range data.DataPoints {
					if label == "" || attrValue(p.Attributes.ToSlice()) == label {
						total += p.Value
					}
				}
			case metricdata.Gauge[int64]:
				for _, p := range data.DataPoints {
					total = p.Value
				}
			}
		}
	}
	return total
}

func attrValue(kvs []attribute.KeyValue) string {
	for _, kv := range kvs {
		return kv.Value.AsString()
	}
	return ""
}

type fillScript struct {
	reports []postgres.SupplyChainImpactStableKeyFillReport
	steps   int
	err     error
	state   postgres.StableKeyFillPauseState
	probeEr error
	sleeps  []time.Duration
}

func (s *fillScript) deps(in *telemetry.Instruments) stableKeyFillDeps {
	return stableKeyFillDeps{
		step: func(context.Context) (postgres.SupplyChainImpactStableKeyFillReport, error) {
			s.steps++
			if s.err != nil {
				return postgres.SupplyChainImpactStableKeyFillReport{}, s.err
			}
			r := s.reports[0]
			if len(s.reports) > 1 {
				s.reports = s.reports[1:]
			}
			return r, nil
		},
		probe: func(context.Context) (postgres.StableKeyFillPauseState, error) { return s.state, s.probeEr },
		cfg: stableKeyFillConfig{
			Enabled: true,
			Pace:    postgres.StableKeyFillPace{BytesPerHour: 8_000_000_000, BytesPerRow: 1024},
			Limits:  postgres.StableKeyFillLimits{MaxReplayLag: 60 * time.Second, MinFreeDiskBytes: 100_000_000_000},
		},
		instruments: in,
		sleep:       func(_ context.Context, d time.Duration) error { s.sleeps = append(s.sleeps, d); return nil },
	}
}

func clearFillState() postgres.StableKeyFillPauseState {
	return postgres.StableKeyFillPauseState{FreeDiskBytes: 500e9, DiskKnown: true}
}

// TestStableKeyFillPassPausesBeforeEveryStepAndCountsTheReason proves each pause
// condition stops the pass before a step runs, counts its closed reason, and that an
// unreadable condition pauses as probe_error.
func TestStableKeyFillPassPausesBeforeEveryStepAndCountsTheReason(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		state   postgres.StableKeyFillPauseState
		probeEr error
		reason  string
	}{
		"replay lag":      {postgres.StableKeyFillPauseState{ReplayLag: 61 * time.Second, FreeDiskBytes: 500e9, DiskKnown: true}, nil, telemetry.SupplyChainImpactStableKeyFillPauseReplayLag},
		"free disk":       {postgres.StableKeyFillPauseState{FreeDiskBytes: 50e9, DiskKnown: true}, nil, telemetry.SupplyChainImpactStableKeyFillPauseFreeDisk},
		"disk unreadable": {postgres.StableKeyFillPauseState{}, nil, telemetry.SupplyChainImpactStableKeyFillPauseDiskUnreadable},
		"index build":     {postgres.StableKeyFillPauseState{FreeDiskBytes: 500e9, DiskKnown: true, IndexBuilding: true}, nil, telemetry.SupplyChainImpactStableKeyFillPauseIndexBuild},
		"probe error":     {postgres.StableKeyFillPauseState{}, errors.New("permission denied"), telemetry.SupplyChainImpactStableKeyFillPauseProbeError},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in, reader := fillTestInstruments(t)
			script := &fillScript{reports: []postgres.SupplyChainImpactStableKeyFillReport{{Filled: 500}}, state: tc.state, probeEr: tc.probeEr}
			outcome, err := runStableKeyFillPass(context.Background(), script.deps(in))
			if err != nil || outcome != stableKeyFillPaused {
				t.Fatalf("pass = %v, %v, want paused", outcome, err)
			}
			if script.steps != 0 {
				t.Fatalf("a paused pass ran %d steps", script.steps)
			}
			if got := fillMetric(t, reader, "eshu_dp_supply_chain_impact_stable_key_fill_pauses_total", tc.reason); got != 1 {
				t.Fatalf("pauses{%s} = %d, want 1", tc.reason, got)
			}
		})
	}
}

// TestStableKeyFillPassPacesStepsAndRecordsProgress proves a running pass waits the
// WAL pace after a step that filled rows, counts steps and rows, and sets the
// complete gauge only on the step that completes.
func TestStableKeyFillPassPacesStepsAndRecordsProgress(t *testing.T) {
	t.Parallel()

	in, reader := fillTestInstruments(t)
	script := &fillScript{
		reports: []postgres.SupplyChainImpactStableKeyFillReport{
			{Filled: 500},
			{Filled: 200, Skipped: 30},
			{Verified: true},
			{Verified: true, Complete: true},
		},
		state: clearFillState(),
	}
	outcome, err := runStableKeyFillPass(context.Background(), script.deps(in))
	if err != nil || outcome != stableKeyFillComplete {
		t.Fatalf("pass = %v, %v, want complete", outcome, err)
	}
	if script.steps != 4 {
		t.Fatalf("steps = %d, want 4", script.steps)
	}
	pace := postgres.StableKeyFillPace{BytesPerHour: 8_000_000_000, BytesPerRow: 1024}
	if len(script.sleeps) != 2 || script.sleeps[0] != pace.Delay(500) || script.sleeps[1] != pace.Delay(200) {
		t.Fatalf("waits = %v, want the pace of 500 then 200 rows and none for the verification steps", script.sleeps)
	}
	if got := fillMetric(t, reader, "eshu_dp_supply_chain_impact_stable_key_fill_rows_total", telemetry.SupplyChainImpactStableKeyFillRowsFilled); got != 700 {
		t.Fatalf("rows filled = %d, want 700", got)
	}
	if got := fillMetric(t, reader, "eshu_dp_supply_chain_impact_stable_key_fill_rows_total", telemetry.SupplyChainImpactStableKeyFillRowsSkipped); got != 30 {
		t.Fatalf("rows skipped = %d, want 30", got)
	}
	if got := fillMetric(t, reader, "eshu_dp_supply_chain_impact_stable_key_fill_complete", ""); got != 1 {
		t.Fatalf("complete gauge = %d, want 1", got)
	}
	if got := fillMetric(t, reader, "eshu_dp_supply_chain_impact_stable_key_fill_steps_total", telemetry.SupplyChainImpactStableKeyFillStepComplete); got != 1 {
		t.Fatalf("complete steps = %d, want 1", got)
	}
}

func TestStableKeyFillPassStepErrorIsCountedAndReturned(t *testing.T) {
	t.Parallel()

	in, reader := fillTestInstruments(t)
	boom := errors.New("boom")
	script := &fillScript{err: boom, state: clearFillState()}
	if _, err := runStableKeyFillPass(context.Background(), script.deps(in)); !errors.Is(err, boom) {
		t.Fatalf("pass error = %v, want the step error", err)
	}
	if got := fillMetric(t, reader, "eshu_dp_supply_chain_impact_stable_key_fill_steps_total", telemetry.SupplyChainImpactStableKeyFillStepFailed); got != 1 {
		t.Fatalf("failed steps = %d, want 1", got)
	}
}

// TestStableKeyFillStartsNothingWhenDisabled proves the default-off rule at the
// start: a disabled fill never calls its runner.
func TestStableKeyFillStartsNothingWhenDisabled(t *testing.T) {
	t.Parallel()

	called := false
	wait := startSupplyChainImpactStableKeyFill(context.Background(), stableKeyFillConfig{}, func(context.Context) error {
		called = true
		return nil
	}, nil)
	wait()
	if called {
		t.Fatal("a disabled fill started its runner")
	}
	ran := make(chan struct{})
	wait = startSupplyChainImpactStableKeyFill(context.Background(), stableKeyFillConfig{Enabled: true}, func(context.Context) error {
		close(ran)
		return nil
	}, nil)
	wait()
	select {
	case <-ran:
	default:
		t.Fatal("an enabled fill did not start its runner")
	}
}
