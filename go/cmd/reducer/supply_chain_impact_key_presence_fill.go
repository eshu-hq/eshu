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

// The key-presence fill runner (#11626, follow-up to #9975). One elected reducer
// fills the key_presence bits a rewrite has not, in chunks of at most 500
// supports, paced to a WAL budget and held back by the pause conditions. It
// never runs unless an operator turns it on: the fill must not run inside a
// counted window before old binaries drain (the marker completes only on a
// clean verification, so a lingering old writer holds the probe flip closed
// rather than corrupting it, but completion needs writers that fill bits), so
// the default is off and the marker-complete gauge stays 0 until it is enabled
// and finishes. The pace, pause, election, and run-loop shape mirror the
// stable-key fill; only the fill type, marker, instruments, and env names
// differ.

const (
	keyPresenceFillEnabledEnv       = "ESHU_SUPPLY_CHAIN_IMPACT_KEY_PRESENCE_FILL_ENABLED"
	keyPresenceFillWALBytesHourEnv  = "ESHU_SUPPLY_CHAIN_IMPACT_KEY_PRESENCE_FILL_WAL_BYTES_PER_HOUR"
	keyPresenceFillWALBytesRowEnv   = "ESHU_SUPPLY_CHAIN_IMPACT_KEY_PRESENCE_FILL_WAL_BYTES_PER_ROW"
	keyPresenceFillMinFreeDiskEnv   = "ESHU_SUPPLY_CHAIN_IMPACT_KEY_PRESENCE_FILL_MIN_FREE_DISK_BYTES"
	keyPresenceFillMaxReplayLagEnv  = "ESHU_SUPPLY_CHAIN_IMPACT_KEY_PRESENCE_FILL_MAX_REPLAY_LAG"
	keyPresenceFillAdvisoryLockID   = 9975
	keyPresenceFillPassSteps        = 200
	keyPresenceFillPassBudget       = time.Minute
	keyPresenceFillPausedPoll       = time.Minute
	keyPresenceFillCompletePoll     = 5 * time.Minute
	defaultKeyPresenceFillBytesHour = int64(8_000_000_000)
	// defaultKeyPresenceFillBytesRow is the estimated WAL one filled support
	// writes, the same bound the stable-key fill uses: the fill rewrites the
	// same TOASTed support rows, so the 14.1 x 8192 + 3100 estimate holds.
	defaultKeyPresenceFillBytesRow = int64(120000)
	defaultKeyPresenceFillMinDisk  = int64(100_000_000_000)
	defaultKeyPresenceFillMaxLag   = 60 * time.Second
)

// keyPresenceFillConfig is the fill's operator configuration. The pace and
// pause machinery is the shared stable-key fill machinery: replica lag, free
// disk, and index builds hold back every supports fill the same way, and the
// WAL budget paces the same rewritten rows.
type keyPresenceFillConfig struct {
	Enabled bool
	Pace    postgres.StableKeyFillPace
	Limits  postgres.StableKeyFillLimits
}

// loadKeyPresenceFillConfig reads the fill's environment. An unset, unparsable
// or non-positive value gives the default; for the two pause limits "0" is a
// valid, explicit way to turn the condition off. Enabled is false unless it
// parses true.
func loadKeyPresenceFillConfig(getenv func(string) string) keyPresenceFillConfig {
	cfg := keyPresenceFillConfig{
		Pace:   postgres.StableKeyFillPace{BytesPerHour: defaultKeyPresenceFillBytesHour, BytesPerRow: defaultKeyPresenceFillBytesRow},
		Limits: postgres.StableKeyFillLimits{MaxReplayLag: defaultKeyPresenceFillMaxLag, MinFreeDiskBytes: defaultKeyPresenceFillMinDisk},
	}
	if enabled, err := strconv.ParseBool(strings.TrimSpace(getenv(keyPresenceFillEnabledEnv))); err == nil {
		cfg.Enabled = enabled
	}
	if v, err := strconv.ParseInt(strings.TrimSpace(getenv(keyPresenceFillWALBytesHourEnv)), 10, 64); err == nil && v > 0 {
		cfg.Pace.BytesPerHour = v
	}
	if v, err := strconv.ParseInt(strings.TrimSpace(getenv(keyPresenceFillWALBytesRowEnv)), 10, 64); err == nil && v > 0 {
		cfg.Pace.BytesPerRow = v
	}
	if v, err := strconv.ParseInt(strings.TrimSpace(getenv(keyPresenceFillMinFreeDiskEnv)), 10, 64); err == nil && v >= 0 {
		cfg.Limits.MinFreeDiskBytes = v
	}
	if v, err := time.ParseDuration(strings.TrimSpace(getenv(keyPresenceFillMaxReplayLagEnv))); err == nil && v >= 0 {
		cfg.Limits.MaxReplayLag = v
	}
	return cfg
}

// keyPresenceFillDeps are the seams one pass runs through.
type keyPresenceFillDeps struct {
	step        func(context.Context) (postgres.SupplyChainImpactKeyPresenceFillReport, error)
	probe       func(context.Context) (postgres.StableKeyFillPauseState, error)
	cfg         keyPresenceFillConfig
	instruments *telemetry.Instruments
	logger      *slog.Logger
	// sleep waits d or returns early with ctx's error; defaults to a timer wait.
	sleep func(context.Context, time.Duration) error
	// now is the clock the pass budget reads; defaults to time.Now.
	now func() time.Time
}

func (d keyPresenceFillDeps) wait(ctx context.Context, delay time.Duration) error {
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

func (d keyPresenceFillDeps) clock() time.Time {
	if d.now != nil {
		return d.now()
	}
	return time.Now()
}

// keyPresenceFillOutcome is how one pass ended.
type keyPresenceFillOutcome int

const (
	keyPresenceFillRan keyPresenceFillOutcome = iota
	keyPresenceFillComplete
	keyPresenceFillPaused
)

// runKeyPresenceFillPass runs steps until the marker is set, the step or time
// budget runs out, a pause condition holds, or a step fails. The pause
// conditions are read before every step, so a lag spike or an index build stops
// the fill within one chunk; a condition that cannot be read pauses it. A
// verification slice that finds disagreeing bits restarts the sweep instead of
// completing the marker (fail-closed): the step counts a gap and the pass keeps
// stepping. After a step that filled rows it waits the WAL pace.
func runKeyPresenceFillPass(ctx context.Context, d keyPresenceFillDeps) (keyPresenceFillOutcome, error) {
	deadline := d.clock().Add(keyPresenceFillPassBudget)
	for i := 0; i < keyPresenceFillPassSteps && d.clock().Before(deadline); i++ {
		state, probeErr := d.probe(ctx)
		reason := telemetry.SupplyChainImpactKeyPresenceFillPauseProbeError
		if probeErr == nil {
			reason = pauseReasonForKeyPresenceFill(d.cfg.Limits, state)
		}
		if reason != "" {
			d.instruments.RecordSupplyChainImpactKeyPresenceFillPause(ctx, reason)
			if d.logger != nil {
				d.logger.Warn("supply chain impact key presence fill paused", "reason", reason, "error", probeErr)
			}
			return keyPresenceFillPaused, nil
		}
		report, err := d.step(ctx)
		if err != nil {
			d.instruments.RecordSupplyChainImpactKeyPresenceFillStep(ctx, telemetry.SupplyChainImpactKeyPresenceFillStepFailed)
			return keyPresenceFillRan, err
		}
		d.instruments.RecordSupplyChainImpactKeyPresenceFillStep(ctx, keyPresenceFillStepOutcome(report))
		d.instruments.RecordSupplyChainImpactKeyPresenceFillRows(ctx, telemetry.SupplyChainImpactKeyPresenceFillRowsFilled, report.Filled)
		d.instruments.RecordSupplyChainImpactKeyPresenceFillRows(ctx, telemetry.SupplyChainImpactKeyPresenceFillRowsSkipped, report.Skipped)
		if report.Complete {
			d.instruments.RecordSupplyChainImpactKeyPresenceFillComplete(ctx, true)
			if d.logger != nil {
				d.logger.Info("supply chain impact key presence fill complete")
			}
			return keyPresenceFillComplete, nil
		}
		d.instruments.RecordSupplyChainImpactKeyPresenceFillComplete(ctx, false)
		if err := d.wait(ctx, d.cfg.Pace.Delay(report.Filled)); err != nil {
			return keyPresenceFillRan, err
		}
	}
	return keyPresenceFillRan, nil
}

// pauseReasonForKeyPresenceFill maps the shared pause verdict onto the
// key-presence fill's closed reason set. The value sets are identical by
// construction; the mapping keeps the runner independent of the stable-key
// fill's telemetry names.
func pauseReasonForKeyPresenceFill(limits postgres.StableKeyFillLimits, state postgres.StableKeyFillPauseState) string {
	switch limits.PauseReason(state) {
	case telemetry.SupplyChainImpactStableKeyFillPauseIndexBuild:
		return telemetry.SupplyChainImpactKeyPresenceFillPauseIndexBuild
	case telemetry.SupplyChainImpactStableKeyFillPauseReplayLag:
		return telemetry.SupplyChainImpactKeyPresenceFillPauseReplayLag
	case telemetry.SupplyChainImpactStableKeyFillPauseDiskUnreadable:
		return telemetry.SupplyChainImpactKeyPresenceFillPauseDiskUnreadable
	case telemetry.SupplyChainImpactStableKeyFillPauseFreeDisk:
		return telemetry.SupplyChainImpactKeyPresenceFillPauseFreeDisk
	default:
		return ""
	}
}

// keyPresenceFillStepOutcome classifies a step into the closed outcome set.
func keyPresenceFillStepOutcome(r postgres.SupplyChainImpactKeyPresenceFillReport) string {
	switch {
	case r.Complete:
		return telemetry.SupplyChainImpactKeyPresenceFillStepComplete
	case r.Verified && r.Missing > 0:
		return telemetry.SupplyChainImpactKeyPresenceFillStepGap
	case r.Verified:
		return telemetry.SupplyChainImpactKeyPresenceFillStepVerify
	default:
		return telemetry.SupplyChainImpactKeyPresenceFillStepChunk
	}
}

// startSupplyChainImpactKeyPresenceFill starts the fill when it is enabled and
// returns a wait function for shutdown before the database closes. Disabled,
// it starts nothing.
func startSupplyChainImpactKeyPresenceFill(ctx context.Context, cfg keyPresenceFillConfig, run func(context.Context) error, logger *slog.Logger) func() {
	if !cfg.Enabled {
		return func() {}
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := run(ctx); err != nil && ctx.Err() == nil && logger != nil {
			logger.Error("supply chain impact key presence fill stopped", "error", err)
		}
	}()
	return func() { <-done }
}

// runSupplyChainImpactKeyPresenceFill repeats passes under one elected owner: a
// complete marker idles on a slow poll, a paused or contended or failed pass
// waits a minute, and a pass that ran steps goes again at once.
func runSupplyChainImpactKeyPresenceFill(
	ctx context.Context, database *sql.DB, cfg keyPresenceFillConfig, instruments *telemetry.Instruments, logger *slog.Logger,
) error {
	fill := postgres.SupplyChainImpactKeyPresenceFill{DB: postgres.SQLDB{DB: database}}
	probe := postgres.StableKeyFillProbe{DB: postgres.SQLDB{DB: database}}
	deps := keyPresenceFillDeps{
		step: fill.RunStep, probe: probe.Read, cfg: cfg, instruments: instruments, logger: logger,
	}
	for {
		outcome := keyPresenceFillRan
		ran, err := withAdvisoryLeadership(ctx, database, supportsBackfillAdvisoryLockClass, keyPresenceFillAdvisoryLockID,
			func(passCtx context.Context) error {
				var passErr error
				outcome, passErr = runKeyPresenceFillPass(passCtx, deps)
				return passErr
			})
		if ctx.Err() != nil {
			return ctx.Err()
		}
		wait := keyPresenceFillPausedPoll
		switch {
		case err != nil:
			if logger != nil {
				logger.Error("supply chain impact key presence fill pass failed", "error", err)
			}
		case ran && outcome == keyPresenceFillComplete:
			wait = keyPresenceFillCompletePoll
		case ran && outcome == keyPresenceFillRan:
			wait = supportsBackfillFastPoll
		}
		if err := deps.wait(ctx, wait); err != nil {
			return err
		}
	}
}
