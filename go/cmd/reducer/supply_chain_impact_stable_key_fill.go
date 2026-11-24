// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// The stable_fact_key fill runner (#10880 PR-C1k, arbiter ruling C1 sections 1.4 to
// 1.7). One elected reducer fills the keys a rewrite has not, in chunks of at most
// 500 supports, paced to a WAL budget and held back by the pause conditions. It
// never runs unless an operator turns it on: the fill must not run inside a counted
// window (W1b, W2) or before the lab run, so the default is off and the marker-complete
// gauge stays 0 until it is enabled and finishes.

const (
	stableKeyFillEnabledEnv       = "ESHU_SUPPLY_CHAIN_IMPACT_STABLE_KEY_FILL_ENABLED"
	stableKeyFillWALBytesHourEnv  = "ESHU_SUPPLY_CHAIN_IMPACT_STABLE_KEY_FILL_WAL_BYTES_PER_HOUR"
	stableKeyFillWALBytesRowEnv   = "ESHU_SUPPLY_CHAIN_IMPACT_STABLE_KEY_FILL_WAL_BYTES_PER_ROW"
	stableKeyFillMinFreeDiskEnv   = "ESHU_SUPPLY_CHAIN_IMPACT_STABLE_KEY_FILL_MIN_FREE_DISK_BYTES"
	stableKeyFillMaxReplayLagEnv  = "ESHU_SUPPLY_CHAIN_IMPACT_STABLE_KEY_FILL_MAX_REPLAY_LAG"
	stableKeyFillAdvisoryLockID   = 10881
	stableKeyFillPassSteps        = 200
	stableKeyFillPassBudget       = time.Minute
	stableKeyFillPausedPoll       = time.Minute
	stableKeyFillCompletePoll     = 5 * time.Minute
	defaultStableKeyFillBytesHour = int64(8_000_000_000)
	defaultStableKeyFillBytesRow  = int64(1024)
	defaultStableKeyFillMinDisk   = int64(100_000_000_000)
	defaultStableKeyFillMaxLag    = 60 * time.Second
)

// stableKeyFillConfig is the fill's operator configuration.
type stableKeyFillConfig struct {
	Enabled bool
	Pace    postgres.StableKeyFillPace
	Limits  postgres.StableKeyFillLimits
}

// loadStableKeyFillConfig reads the fill's environment. An unset, unparsable or
// non-positive value gives the default; for the two pause limits "0" is a valid,
// explicit way to turn the condition off. Enabled is false unless it parses true.
func loadStableKeyFillConfig(getenv func(string) string) stableKeyFillConfig {
	cfg := stableKeyFillConfig{
		Pace:   postgres.StableKeyFillPace{BytesPerHour: defaultStableKeyFillBytesHour, BytesPerRow: defaultStableKeyFillBytesRow},
		Limits: postgres.StableKeyFillLimits{MaxReplayLag: defaultStableKeyFillMaxLag, MinFreeDiskBytes: defaultStableKeyFillMinDisk},
	}
	if enabled, err := strconv.ParseBool(strings.TrimSpace(getenv(stableKeyFillEnabledEnv))); err == nil {
		cfg.Enabled = enabled
	}
	if v, err := strconv.ParseInt(strings.TrimSpace(getenv(stableKeyFillWALBytesHourEnv)), 10, 64); err == nil && v > 0 {
		cfg.Pace.BytesPerHour = v
	}
	if v, err := strconv.ParseInt(strings.TrimSpace(getenv(stableKeyFillWALBytesRowEnv)), 10, 64); err == nil && v > 0 {
		cfg.Pace.BytesPerRow = v
	}
	if v, err := strconv.ParseInt(strings.TrimSpace(getenv(stableKeyFillMinFreeDiskEnv)), 10, 64); err == nil && v >= 0 {
		cfg.Limits.MinFreeDiskBytes = v
	}
	if v, err := time.ParseDuration(strings.TrimSpace(getenv(stableKeyFillMaxReplayLagEnv))); err == nil && v >= 0 {
		cfg.Limits.MaxReplayLag = v
	}
	return cfg
}

// stableKeyFillDeps are the seams one pass runs through.
type stableKeyFillDeps struct {
	step        func(context.Context) (postgres.SupplyChainImpactStableKeyFillReport, error)
	probe       func(context.Context) (postgres.StableKeyFillPauseState, error)
	cfg         stableKeyFillConfig
	instruments *telemetry.Instruments
	logger      *slog.Logger
	// sleep waits d or returns early with ctx's error; defaults to a timer wait.
	sleep func(context.Context, time.Duration) error
	// now is the clock the pass budget reads; defaults to time.Now.
	now func() time.Time
}

func (d stableKeyFillDeps) wait(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return ctx.Err()
	}
	if d.sleep != nil {
		return d.sleep(ctx, delay)
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (d stableKeyFillDeps) clock() time.Time {
	if d.now != nil {
		return d.now()
	}
	return time.Now()
}

// stableKeyFillOutcome is how one pass ended.
type stableKeyFillOutcome int

const (
	stableKeyFillRan stableKeyFillOutcome = iota
	stableKeyFillComplete
	stableKeyFillPaused
)

// runStableKeyFillPass runs steps until the marker is set, the step or time budget
// runs out, a pause condition holds, or a step fails. The pause conditions are read
// before every step, so a lag spike or an index build stops the fill within one
// chunk; a condition that cannot be read pauses it. After a step that filled rows it
// waits the WAL pace.
func runStableKeyFillPass(ctx context.Context, d stableKeyFillDeps) (stableKeyFillOutcome, error) {
	deadline := d.clock().Add(stableKeyFillPassBudget)
	for i := 0; i < stableKeyFillPassSteps && d.clock().Before(deadline); i++ {
		state, probeErr := d.probe(ctx)
		reason := telemetry.SupplyChainImpactStableKeyFillPauseProbeError
		if probeErr == nil {
			reason = d.cfg.Limits.PauseReason(state)
		}
		if reason != "" {
			d.instruments.RecordSupplyChainImpactStableKeyFillPause(ctx, reason)
			if d.logger != nil {
				d.logger.Warn("supply chain impact stable key fill paused", "reason", reason, "error", probeErr)
			}
			return stableKeyFillPaused, nil
		}
		report, err := d.step(ctx)
		if err != nil {
			d.instruments.RecordSupplyChainImpactStableKeyFillStep(ctx, telemetry.SupplyChainImpactStableKeyFillStepFailed)
			return stableKeyFillRan, err
		}
		d.instruments.RecordSupplyChainImpactStableKeyFillStep(ctx, stableKeyFillStepOutcome(report))
		d.instruments.RecordSupplyChainImpactStableKeyFillRows(ctx, telemetry.SupplyChainImpactStableKeyFillRowsFilled, report.Filled)
		d.instruments.RecordSupplyChainImpactStableKeyFillRows(ctx, telemetry.SupplyChainImpactStableKeyFillRowsSkipped, report.Skipped)
		if report.Complete {
			d.instruments.RecordSupplyChainImpactStableKeyFillComplete(ctx, true)
			if d.logger != nil {
				d.logger.Info("supply chain impact stable key fill complete")
			}
			return stableKeyFillComplete, nil
		}
		d.instruments.RecordSupplyChainImpactStableKeyFillComplete(ctx, false)
		if err := d.wait(ctx, d.cfg.Pace.Delay(report.Filled)); err != nil {
			return stableKeyFillRan, err
		}
	}
	return stableKeyFillRan, nil
}

// stableKeyFillStepOutcome classifies a step into the closed outcome set.
func stableKeyFillStepOutcome(r postgres.SupplyChainImpactStableKeyFillReport) string {
	switch {
	case r.Complete:
		return telemetry.SupplyChainImpactStableKeyFillStepComplete
	case r.Verified && r.Missing > 0:
		return telemetry.SupplyChainImpactStableKeyFillStepGap
	case r.Verified:
		return telemetry.SupplyChainImpactStableKeyFillStepVerify
	default:
		return telemetry.SupplyChainImpactStableKeyFillStepChunk
	}
}

// startSupplyChainImpactStableKeyFill starts the fill when it is enabled and returns
// a wait function for shutdown before the database closes. Disabled, it starts
// nothing.
func startSupplyChainImpactStableKeyFill(ctx context.Context, cfg stableKeyFillConfig, run func(context.Context) error, logger *slog.Logger) func() {
	if !cfg.Enabled {
		return func() {}
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := run(ctx); err != nil && ctx.Err() == nil && logger != nil {
			logger.Error("supply chain impact stable key fill stopped", "error", err)
		}
	}()
	return func() { <-done }
}

// runSupplyChainImpactStableKeyFill repeats passes under one elected owner: a
// complete marker idles on a slow poll, a paused or contended or failed pass waits
// a minute, and a pass that ran steps goes again at once.
func runSupplyChainImpactStableKeyFill(
	ctx context.Context, database *sql.DB, cfg stableKeyFillConfig, instruments *telemetry.Instruments, logger *slog.Logger,
) error {
	fill := postgres.SupplyChainImpactStableKeyFill{DB: postgres.SQLDB{DB: database}}
	probe := postgres.StableKeyFillProbe{DB: postgres.SQLDB{DB: database}}
	deps := stableKeyFillDeps{
		step: fill.RunStep, probe: probe.Read, cfg: cfg, instruments: instruments, logger: logger,
	}
	for {
		outcome := stableKeyFillRan
		ran, err := withAdvisoryLeadership(ctx, database, supportsBackfillAdvisoryLockClass, stableKeyFillAdvisoryLockID,
			func(passCtx context.Context) error {
				var passErr error
				outcome, passErr = runStableKeyFillPass(passCtx, deps)
				return passErr
			})
		if ctx.Err() != nil {
			return ctx.Err()
		}
		wait := stableKeyFillPausedPoll
		switch {
		case err != nil:
			if logger != nil {
				logger.Error("supply chain impact stable key fill pass failed", "error", err)
			}
		case ran && outcome == stableKeyFillComplete:
			wait = stableKeyFillCompletePoll
		case ran && outcome == stableKeyFillRan:
			wait = supportsBackfillFastPoll
		}
		if err := deps.wait(ctx, wait); err != nil {
			return err
		}
	}
}
