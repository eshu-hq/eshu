// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// loadGenerationLivenessConfig reads the ESHU_GENERATION_LIVENESS_* settings.
// It is pure so both the sweep and the age-bucket gauge can load it; the one
// startup warning for a clamped progress window is emitted by
// warnGenerationLivenessProgressWindowClamp.
//
// The progress window is clamped to at least the poll interval: a window
// shorter than the sweep cadence would let a domain queue look quiet between
// two polls even while it completes work every poll, re-opening the #7265
// false-replay class.
func loadGenerationLivenessConfig(getenv func(string) string) generationLivenessConfig {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	pollInterval := loadDurationOrDefault(getenv, generationLivenessPollIntervalEnv, defaultGenerationLivenessPollInterval)
	progressWindow := loadDurationOrDefault(getenv, generationLivenessProgressWindowEnv, defaultGenerationLivenessProgressWindow)
	var clampedFrom time.Duration
	if progressWindow < pollInterval {
		clampedFrom = progressWindow
		progressWindow = pollInterval
	}
	return generationLivenessConfig{
		Enabled: loadBoolOrDefault(getenv, generationLivenessEnabledEnv, true),
		Runner: maintenance.GenerationLivenessRunnerConfig{
			PollInterval: pollInterval,
			Policy: maintenance.GenerationLivenessPolicy{
				ActivationDeadline: loadDurationOrDefault(getenv, generationLivenessActivationDeadlineEnv, defaultGenerationLivenessActivationDeadline),
				MaxRecoverAttempts: loadPositiveIntOrDefault(getenv, generationLivenessMaxRecoverAttemptsEnv, defaultGenerationLivenessMaxRecoverAttempts),
				BatchLimit:         loadPositiveIntOrDefault(getenv, generationLivenessBatchLimitEnv, defaultGenerationLivenessBatchLimit),
				ProgressWindow:     progressWindow,
			},
		},
		ProgressWindowClampedFrom: clampedFrom,
	}
}

// warnGenerationLivenessProgressWindowClamp logs once, at reducer startup,
// when the configured progress window was raised to the poll interval.
func warnGenerationLivenessProgressWindowClamp(ctx context.Context, logger *slog.Logger, cfg generationLivenessConfig) {
	if logger == nil || cfg.ProgressWindowClampedFrom <= 0 {
		return
	}
	logger.WarnContext(
		ctx,
		"generation liveness progress window raised to the poll interval",
		slog.String("env", generationLivenessProgressWindowEnv),
		slog.String("configured_progress_window", cfg.ProgressWindowClampedFrom.String()),
		slog.String("effective_progress_window", cfg.Runner.Policy.ProgressWindow.String()),
		slog.String("poll_interval", cfg.Runner.PollInterval.String()),
	)
}

// postgresGenerationLivenessRecoverer adapts the Postgres liveness store to the
// reducer's GenerationLivenessRecoverer contract, translating the reducer-side
// policy into the storage-side policy.
type postgresGenerationLivenessRecoverer struct {
	store postgres.GenerationLivenessStore
}

func generationLivenessRunnerFor(
	database db.ExecQueryer,
	cfg generationLivenessConfig,
) *maintenance.GenerationLivenessRunner {
	if !cfg.Enabled {
		return nil
	}
	return &maintenance.GenerationLivenessRunner{
		Recoverer: postgresGenerationLivenessRecoverer{
			store: postgres.NewGenerationLivenessStore(database),
		},
		Config: cfg.Runner,
	}
}

func (r postgresGenerationLivenessRecoverer) RecoverWedgedGenerations(
	ctx context.Context,
	policy maintenance.GenerationLivenessPolicy,
	now time.Time,
) (maintenance.GenerationLivenessResult, error) {
	result, err := r.store.RecoverWedgedGenerations(ctx, postgres.GenerationLivenessPolicy{
		ActivationDeadline: policy.ActivationDeadline,
		MaxRecoverAttempts: policy.MaxRecoverAttempts,
		BatchLimit:         policy.BatchLimit,
		ProgressWindow:     policy.ProgressWindow,
	}, now)
	if err != nil {
		return maintenance.GenerationLivenessResult{}, err
	}
	recoveries := make([]maintenance.GenerationLivenessRecovery, 0, len(result.Recoveries))
	for _, recovery := range result.Recoveries {
		recoveries = append(recoveries, maintenance.GenerationLivenessRecovery{
			ScopeID:                  recovery.ScopeID,
			GenerationID:             recovery.GenerationID,
			LivenessRecoveryAttempts: recovery.LivenessRecoveryAttempts,
		})
	}
	return maintenance.GenerationLivenessResult{
		Superseded: result.Superseded,
		Recovered:  result.Recovered,
		Recoveries: recoveries,
	}, nil
}

// activeGenerationAgeObserver adapts the Postgres liveness store to the
// telemetry observer contract for the active-generation age-bucket gauge.
type activeGenerationAgeObserver struct {
	store  postgres.GenerationLivenessStore
	policy postgres.GenerationLivenessPolicy
}

func activeGenerationAgeObserverFor(
	database db.ExecQueryer,
	cfg generationLivenessConfig,
) activeGenerationAgeObserver {
	return activeGenerationAgeObserver{
		store: postgres.NewGenerationLivenessStore(database),
		policy: postgres.GenerationLivenessPolicy{
			ActivationDeadline: cfg.Runner.Policy.ActivationDeadline,
			MaxRecoverAttempts: cfg.Runner.Policy.MaxRecoverAttempts,
			BatchLimit:         cfg.Runner.Policy.BatchLimit,
			ProgressWindow:     cfg.Runner.Policy.ProgressWindow,
		},
	}
}

// ActiveGenerationsByAge returns active generation counts keyed by the closed
// fresh/aging/draining/stuck age buckets for the observable gauge callback.
func (o activeGenerationAgeObserver) ActiveGenerationsByAge(ctx context.Context) (map[string]int64, error) {
	return o.store.CountActiveGenerationsByAge(ctx, o.policy, time.Now().UTC())
}
