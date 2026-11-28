// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func TestKeyPresenceFillConfigDefaultsAreOffAndWithinTheRulingLimits(t *testing.T) {
	t.Parallel()

	cfg := loadKeyPresenceFillConfig(func(string) string { return "" })
	if cfg.Enabled {
		t.Fatal("the fill is enabled by default: it must not run inside a counted window or before the lab run")
	}
	if cfg.Pace.BytesPerRow != 120000 {
		t.Fatalf("default WAL estimate = %d bytes a row, want the bound 14.1 x 8192 + 3100 = 120000, not a lab figure", cfg.Pace.BytesPerRow)
	}
	if cfg.Pace.BytesPerHour > 8_000_000_000 || cfg.Pace.BytesPerHour <= 0 {
		t.Fatalf("default WAL budget = %d bytes an hour, want at most the ruling's 8 GB", cfg.Pace.BytesPerHour)
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

func clearKeyPresenceFillState() postgres.StableKeyFillPauseState {
	return postgres.StableKeyFillPauseState{FreeDiskBytes: 500e9, DiskKnown: true}
}

// TestKeyPresenceFillPassPausesBeforeEveryStepAndCountsTheReason proves each
// pause condition stops the pass before a step runs, counts its closed reason,
// and that an unreadable condition pauses as probe_error.
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

// TestKeyPresenceFillPassPacesStepsAndRecordsProgress proves a running pass
// waits the WAL pace after a step that filled rows, counts steps and rows, and
// sets the complete gauge only on the step that completes.
func TestKeyPresenceFillPassPacesStepsAndRecordsProgress(t *testing.T) {
	t.Parallel()

	in, reader := fillTestInstruments(t)
	script := &keyPresenceFillScript{
		reports: []postgres.SupplyChainImpactKeyPresenceFillReport{
			{Filled: 500},
			{Filled: 200, Skipped: 30},
			{Verified: true},
			{Verified: true, Complete: true},
		},
		state: clearKeyPresenceFillState(),
	}
	outcome, err := runKeyPresenceFillPass(context.Background(), script.deps(in))
	if err != nil || outcome != keyPresenceFillComplete {
		t.Fatalf("pass = %v, %v, want complete", outcome, err)
	}
	if script.steps != 4 {
		t.Fatalf("steps = %d, want 4", script.steps)
	}
	pace := postgres.StableKeyFillPace{BytesPerHour: 8_000_000_000, BytesPerRow: 1024}
	if len(script.sleeps) != 2 || script.sleeps[0] != pace.Delay(500) || script.sleeps[1] != pace.Delay(200) {
		t.Fatalf("waits = %v, want the pace of 500 then 200 rows and none for the verification steps", script.sleeps)
	}
	if got := fillMetric(t, reader, "eshu_dp_supply_chain_impact_key_presence_fill_rows_total", telemetry.SupplyChainImpactKeyPresenceFillRowsFilled); got != 700 {
		t.Fatalf("rows filled = %d, want 700", got)
	}
	if got := fillMetric(t, reader, "eshu_dp_supply_chain_impact_key_presence_fill_rows_total", telemetry.SupplyChainImpactKeyPresenceFillRowsSkipped); got != 30 {
		t.Fatalf("rows skipped = %d, want 30", got)
	}
	if got := fillMetric(t, reader, "eshu_dp_supply_chain_impact_key_presence_fill_complete", ""); got != 1 {
		t.Fatalf("complete gauge = %d, want 1", got)
	}
	if got := fillMetric(t, reader, "eshu_dp_supply_chain_impact_key_presence_fill_steps_total", telemetry.SupplyChainImpactKeyPresenceFillStepComplete); got != 1 {
		t.Fatalf("complete steps = %d, want 1", got)
	}
}

// TestKeyPresenceFillPassRestartsOnDisagreementInsteadOfCompleting proves the
// fail-closed path: a verification slice that finds disagreeing bits restarts
// the sweep (the pass keeps stepping and counts a gap) instead of setting the
// marker, and only a later clean slice completes.
func TestKeyPresenceFillPassRestartsOnDisagreementInsteadOfCompleting(t *testing.T) {
	t.Parallel()

	in, reader := fillTestInstruments(t)
	script := &keyPresenceFillScript{
		reports: []postgres.SupplyChainImpactKeyPresenceFillReport{
			{Filled: 100},
			{Verified: true, Missing: 5},
			{Filled: 50},
			{Verified: true, Complete: true},
		},
		state: clearKeyPresenceFillState(),
	}
	outcome, err := runKeyPresenceFillPass(context.Background(), script.deps(in))
	if err != nil || outcome != keyPresenceFillComplete {
		t.Fatalf("pass = %v, %v, want complete after the restart", outcome, err)
	}
	if script.steps != 4 {
		t.Fatalf("steps = %d, want 4: the gap must not end the pass", script.steps)
	}
	if got := fillMetric(t, reader, "eshu_dp_supply_chain_impact_key_presence_fill_steps_total", telemetry.SupplyChainImpactKeyPresenceFillStepGap); got != 1 {
		t.Fatalf("gap steps = %d, want 1", got)
	}
	if got := fillMetric(t, reader, "eshu_dp_supply_chain_impact_key_presence_fill_rows_total", telemetry.SupplyChainImpactKeyPresenceFillRowsFilled); got != 150 {
		t.Fatalf("rows filled = %d, want 100 before the gap plus 50 after it", got)
	}
	if got := fillMetric(t, reader, "eshu_dp_supply_chain_impact_key_presence_fill_complete", ""); got != 1 {
		t.Fatalf("complete gauge = %d, want 1: only the clean slice sets the marker", got)
	}
}

func TestKeyPresenceFillPassStepErrorIsCountedAndReturned(t *testing.T) {
	t.Parallel()

	in, reader := fillTestInstruments(t)
	boom := errors.New("boom")
	script := &keyPresenceFillScript{err: boom, state: clearKeyPresenceFillState()}
	if _, err := runKeyPresenceFillPass(context.Background(), script.deps(in)); !errors.Is(err, boom) {
		t.Fatalf("pass error = %v, want the step error", err)
	}
	if got := fillMetric(t, reader, "eshu_dp_supply_chain_impact_key_presence_fill_steps_total", telemetry.SupplyChainImpactKeyPresenceFillStepFailed); got != 1 {
		t.Fatalf("failed steps = %d, want 1", got)
	}
}

// TestKeyPresenceFillStartsNothingWhenDisabled proves the default-off rule at
// the start: a disabled fill never calls its runner.
func TestKeyPresenceFillStartsNothingWhenDisabled(t *testing.T) {
	t.Parallel()

	called := false
	wait := startSupplyChainImpactKeyPresenceFill(context.Background(), keyPresenceFillConfig{}, func(context.Context) error {
		called = true
		return nil
	}, nil)
	wait()
	if called {
		t.Fatal("a disabled fill started its runner")
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
		t.Fatal("an enabled fill did not start its runner")
	}
}

// TestKeyPresenceFillPassMakesProgressAtTheShippedPace runs one pass on a
// virtual clock at the shipped defaults (8 GB an hour, 120,000 bytes a row): a
// 500-row chunk waits 27 s, so a one-minute pass runs three steps and ends with
// the outcome that polls again at once, which is 500 rows every 27 s, about
// 66,000 an hour: the rate the canary assumes.
func TestKeyPresenceFillPassMakesProgressAtTheShippedPace(t *testing.T) {
	t.Parallel()

	cfg := loadKeyPresenceFillConfig(func(string) string { return "" })
	wait := cfg.Pace.Delay(500)
	if wait != 27*time.Second {
		t.Fatalf("a 500-row chunk waits %v at the defaults, want 27s", wait)
	}
	virtual := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	steps := 0
	script := &keyPresenceFillScript{state: clearKeyPresenceFillState()}
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
	if perHour := float64(steps*500) / virtual.Sub(time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)).Hours(); perHour < 60000 || perHour > 72000 {
		t.Fatalf("the pass fills %.0f rows an hour, want about 66,000", perHour)
	}
}

// TestRunSupplyChainImpactKeyPresenceFillStaysAliveAndStopsOnCancel proves the
// election half of the loop: it survives a contended election and a failed
// pass (it logs the failure and polls again) and returns the context error,
// not nil, when it is stopped. The elected path runs the same shared
// withAdvisoryLeadership helper the backfill election test covers.
func TestRunSupplyChainImpactKeyPresenceFillStaysAliveAndStopsOnCancel(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		lock    func() (bool, error)
		wantLog string
	}{
		"contended": {lock: lockReply(false)},
		"failed acquire": {
			lock:    func() (bool, error) { return false, errors.New("connection reset") },
			wantLog: "supply chain impact key presence fill pass failed",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			probe := &supportsLockProbe{lock: tc.lock, unlock: lockReply(true), attempted: make(chan struct{}, 1)}
			database := sql.OpenDB(probe)
			defer database.Close()
			var logs syncBuffer
			logger := slog.New(slog.NewTextHandler(&logs, nil))
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				done <- runSupplyChainImpactKeyPresenceFill(ctx, database, keyPresenceFillConfig{}, nil, logger)
			}()
			select {
			case <-probe.attempted:
			case err := <-done:
				t.Fatalf("fill exited before its first election: %v", err)
			case <-ctx.Done():
				t.Fatal("fill never tried the election")
			}
			// The probe signals from inside the lock statement, before the loop has
			// seen the failure, so a test that cancels at once can stop the loop
			// before it logs. Wait for the effect itself, bounded, before stopping.
			if tc.wantLog != "" {
				waitForSupportsBackfillLog(t, &logs, tc.wantLog, 5*time.Second)
			}
			cancel()
			if err := <-done; !errors.Is(err, context.Canceled) {
				t.Fatalf("fill exit = %v, want context canceled", err)
			}
			if tc.wantLog == "" && strings.Contains(logs.String(), "level=ERROR") {
				t.Fatalf("a contended election must not log an error: %q", logs.String())
			}
		})
	}
}
