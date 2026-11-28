// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func TestKeyPresenceFillConfigDefaultsAreOffAndWithinTheRulingLimits(t *testing.T) {
	t.Parallel()

	cfg := loadKeyPresenceFillConfig(func(string) string { return "" })
	if cfg.Enabled {
		t.Fatal("the key-presence fill is enabled by default: it must stay off until operators roll it")
	}
	if cfg.Pace.BytesPerRow != 120000 {
		t.Fatalf("default WAL estimate = %d bytes a row, want 120000", cfg.Pace.BytesPerRow)
	}
	if cfg.Pace.BytesPerHour > 8_000_000_000 || cfg.Pace.BytesPerHour <= 0 {
		t.Fatalf("default WAL budget = %d bytes an hour, want at most 8 GB", cfg.Pace.BytesPerHour)
	}
	if cfg.Limits.MaxReplayLag != 60*time.Second || cfg.Limits.MinFreeDiskBytes != 100_000_000_000 {
		t.Fatalf("default limits = %+v, want 60s replay lag and 100 GB free disk", cfg.Limits)
	}
	env := map[string]string{
		keyPresenceFillEnabledEnv:      "true",
		keyPresenceFillWALBytesHourEnv: "4000000000",
		keyPresenceFillWALBytesRowEnv:  "2048",
		keyPresenceFillMinFreeDiskEnv:  "0",
		keyPresenceFillMaxReplayLagEnv: "30s",
	}
	cfg = loadKeyPresenceFillConfig(func(k string) string { return env[k] })
	if !cfg.Enabled || cfg.Pace.BytesPerHour != 4e9 || cfg.Pace.BytesPerRow != 2048 || cfg.Limits.MinFreeDiskBytes != 0 || cfg.Limits.MaxReplayLag != 30*time.Second {
		t.Fatalf("parsed config = %+v", cfg)
	}
	bad := map[string]string{keyPresenceFillEnabledEnv: "maybe", keyPresenceFillWALBytesHourEnv: "-1", keyPresenceFillMaxReplayLagEnv: "soon"}
	cfg = loadKeyPresenceFillConfig(func(k string) string { return bad[k] })
	if cfg.Enabled || cfg.Pace.BytesPerHour != 8_000_000_000 || cfg.Limits.MaxReplayLag != 60*time.Second {
		t.Fatalf("unparsable values did not keep the defaults: %+v", cfg)
	}
}

type keyPresenceFillScript struct {
	reports []postgres.SupplyChainImpactKeyPresenceFillReport
	steps   int
	err     error
	state   postgres.StableKeyFillPauseState
	probeEr error
	sleeps  []time.Duration
}

func (s *keyPresenceFillScript) deps(in *telemetry.Instruments) keyPresenceFillDeps {
	return keyPresenceFillDeps{
		step: func(context.Context) (postgres.SupplyChainImpactKeyPresenceFillReport, error) {
			s.steps++
			if s.err != nil {
				return postgres.SupplyChainImpactKeyPresenceFillReport{}, s.err
			}
			r := s.reports[0]
			if len(s.reports) > 1 {
				s.reports = s.reports[1:]
			}
			return r, nil
		},
		probe: func(context.Context) (postgres.StableKeyFillPauseState, error) { return s.state, s.probeEr },
		cfg: keyPresenceFillConfig{
			Enabled: true,
			Pace:    postgres.StableKeyFillPace{BytesPerHour: 8_000_000_000, BytesPerRow: 1024},
			Limits:  postgres.StableKeyFillLimits{MaxReplayLag: 60 * time.Second, MinFreeDiskBytes: 100_000_000_000},
		},
		instruments: in,
		sleep:       func(_ context.Context, d time.Duration) error { s.sleeps = append(s.sleeps, d); return nil },
	}
}

func TestKeyPresenceFillPassPausesBeforeEveryStepAndCountsTheReason(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		state   postgres.StableKeyFillPauseState
		probeEr error
		reason  string
	}{
		"replay lag":      {postgres.StableKeyFillPauseState{ReplayLag: 61 * time.Second, FreeDiskBytes: 500e9, DiskKnown: true}, nil, telemetry.SupplyChainImpactKeyPresenceFillPauseReplayLag},
		"free disk":       {postgres.StableKeyFillPauseState{FreeDiskBytes: 50e9, DiskKnown: true}, nil, telemetry.SupplyChainImpactKeyPresenceFillPauseFreeDisk},
		"disk unreadable": {postgres.StableKeyFillPauseState{}, nil, telemetry.SupplyChainImpactKeyPresenceFillPauseDiskUnreadable},
		"index build":     {postgres.StableKeyFillPauseState{FreeDiskBytes: 500e9, DiskKnown: true, IndexBuilding: true}, nil, telemetry.SupplyChainImpactKeyPresenceFillPauseIndexBuild},
		"probe error":     {postgres.StableKeyFillPauseState{}, errors.New("permission denied"), telemetry.SupplyChainImpactKeyPresenceFillPauseProbeError},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in, reader := fillTestInstruments(t)
			script := &keyPresenceFillScript{reports: []postgres.SupplyChainImpactKeyPresenceFillReport{{Filled: 500}}, state: tc.state, probeEr: tc.probeEr}
			outcome, err := runKeyPresenceFillPass(context.Background(), script.deps(in))
			if err != nil || outcome != keyPresenceFillPaused {
				t.Fatalf("pass = %v, %v, want paused", outcome, err)
			}
			if script.steps != 0 {
				t.Fatalf("a paused pass ran %d steps", script.steps)
			}
			if got := fillMetric(t, reader, "eshu_dp_supply_chain_impact_key_presence_fill_pauses_total", tc.reason); got != 1 {
				t.Fatalf("pauses{%s} = %d, want 1", tc.reason, got)
			}
		})
	}
}

func TestKeyPresenceFillPassPacesStepsRecordsGapAndCompletion(t *testing.T) {
	t.Parallel()

	in, reader := fillTestInstruments(t)
	script := &keyPresenceFillScript{
		reports: []postgres.SupplyChainImpactKeyPresenceFillReport{
			{Filled: 500},
			{Filled: 200, Skipped: 30},
			{Verified: true, Missing: 2},
			{Verified: true},
			{Verified: true, Complete: true},
		},
		state: clearFillState(),
	}
	outcome, err := runKeyPresenceFillPass(context.Background(), script.deps(in))
	if err != nil || outcome != keyPresenceFillComplete {
		t.Fatalf("pass = %v, %v, want complete", outcome, err)
	}
	if script.steps != 5 {
		t.Fatalf("steps = %d, want 5", script.steps)
	}
	pace := postgres.StableKeyFillPace{BytesPerHour: 8_000_000_000, BytesPerRow: 1024}
	if len(script.sleeps) != 2 || script.sleeps[0] != pace.Delay(500) || script.sleeps[1] != pace.Delay(200) {
		t.Fatalf("waits = %v, want the pace of 500 then 200 rows and none for verify steps", script.sleeps)
	}
	if got := fillMetric(t, reader, "eshu_dp_supply_chain_impact_key_presence_fill_rows_total", telemetry.SupplyChainImpactKeyPresenceFillRowsFilled); got != 700 {
		t.Fatalf("rows filled = %d, want 700", got)
	}
	if got := fillMetric(t, reader, "eshu_dp_supply_chain_impact_key_presence_fill_rows_total", telemetry.SupplyChainImpactKeyPresenceFillRowsSkipped); got != 30 {
		t.Fatalf("rows skipped = %d, want 30", got)
	}
	if got := fillMetric(t, reader, "eshu_dp_supply_chain_impact_key_presence_fill_steps_total", telemetry.SupplyChainImpactKeyPresenceFillStepGap); got != 1 {
		t.Fatalf("gap steps = %d, want 1", got)
	}
	if got := fillMetric(t, reader, "eshu_dp_supply_chain_impact_key_presence_fill_steps_total", telemetry.SupplyChainImpactKeyPresenceFillStepVerify); got != 1 {
		t.Fatalf("verify steps = %d, want 1", got)
	}
	if got := fillMetric(t, reader, "eshu_dp_supply_chain_impact_key_presence_fill_complete", ""); got != 1 {
		t.Fatalf("complete gauge = %d, want 1", got)
	}
	if got := fillMetric(t, reader, "eshu_dp_supply_chain_impact_key_presence_fill_steps_total", telemetry.SupplyChainImpactKeyPresenceFillStepComplete); got != 1 {
		t.Fatalf("complete steps = %d, want 1", got)
	}
}

func TestKeyPresenceFillPassStepErrorIsCountedAndReturned(t *testing.T) {
	t.Parallel()

	in, reader := fillTestInstruments(t)
	boom := errors.New("boom")
	script := &keyPresenceFillScript{err: boom, state: clearFillState()}
	if _, err := runKeyPresenceFillPass(context.Background(), script.deps(in)); !errors.Is(err, boom) {
		t.Fatalf("pass error = %v, want the step error", err)
	}
	if got := fillMetric(t, reader, "eshu_dp_supply_chain_impact_key_presence_fill_steps_total", telemetry.SupplyChainImpactKeyPresenceFillStepFailed); got != 1 {
		t.Fatalf("failed steps = %d, want 1", got)
	}
}

func TestKeyPresenceFillStartsNothingWhenDisabled(t *testing.T) {
	t.Parallel()

	called := false
	wait := startSupplyChainImpactKeyPresenceFill(context.Background(), keyPresenceFillConfig{}, func(context.Context) error {
		called = true
		return nil
	}, nil)
	wait()
	if called {
		t.Fatal("a disabled key-presence fill started its runner")
	}
	ran := make(chan struct{})
	wait = startSupplyChainImpactKeyPresenceFill(context.Background(), keyPresenceFillConfig{Enabled: true}, func(context.Context) error {
		close(ran)
		return nil
	}, nil)
	wait()
	select {
	case <-ran:
	default:
		t.Fatal("an enabled key-presence fill did not start its runner")
	}
}

func TestKeyPresenceFillPassMakesProgressAtTheShippedPace(t *testing.T) {
	t.Parallel()

	cfg := loadKeyPresenceFillConfig(func(string) string { return "" })
	wait := cfg.Pace.Delay(500)
	if wait != 27*time.Second {
		t.Fatalf("a 500-row chunk waits %v at the defaults, want 27s", wait)
	}
	virtual := time.Date(2026, 11, 28, 6, 0, 0, 0, time.UTC)
	steps := 0
	script := &keyPresenceFillScript{state: clearFillState()}
	deps := script.deps(nil)
	deps.cfg = cfg
	deps.now = func() time.Time { return virtual }
	deps.sleep = func(_ context.Context, d time.Duration) error { virtual = virtual.Add(d); return nil }
	deps.step = func(context.Context) (postgres.SupplyChainImpactKeyPresenceFillReport, error) {
		steps++
		return postgres.SupplyChainImpactKeyPresenceFillReport{Filled: 500}, nil
	}
	outcome, err := runKeyPresenceFillPass(context.Background(), deps)
	if err != nil || outcome != keyPresenceFillRan {
		t.Fatalf("pass = %v, %v, want the running outcome", outcome, err)
	}
	if steps != 3 {
		t.Fatalf("a one-minute pass ran %d steps at 27 s a step, want 3", steps)
	}
	if keyPresenceFillAdvisoryLockID == stableKeyFillAdvisoryLockID || keyPresenceFillAdvisoryLockID == supportsBackfillAdvisoryLockID {
		t.Fatalf("key-presence fill lock id %d collides with existing backfill/stable-key lock", keyPresenceFillAdvisoryLockID)
	}
}
