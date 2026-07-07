// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// postgresPoisonLivenessRecoverer adapts the Postgres poison-liveness store to
// the reducer's PoisonLivenessRecoverer contract (#4740).
type postgresPoisonLivenessRecoverer struct {
	store postgres.PoisonLivenessStore
}

// poisonLivenessRunnerFor constructs the bounded poison-recovery runner only
// when auto-retry is enabled. The default posture (AutoRetryEnabled=false) is
// surface-only: this returns nil and no dead_letter row is ever re-driven by
// this runner, while the stuck-gauge (wired separately, see
// poisonLivenessObserverFor) remains active regardless.
func poisonLivenessRunnerFor(
	database postgres.ExecQueryer,
	cfg poisonLivenessConfig,
) *reducer.PoisonLivenessRunner {
	if !cfg.Runner.AutoRetryEnabled {
		return nil
	}
	return &reducer.PoisonLivenessRunner{
		Recoverer: postgresPoisonLivenessRecoverer{
			store: postgres.NewPoisonLivenessStore(database),
		},
		Config: cfg.Runner,
	}
}

func (r postgresPoisonLivenessRecoverer) RecoverPoisonDeadLetters(
	ctx context.Context,
	policy reducer.PoisonLivenessPolicy,
	now time.Time,
) (reducer.PoisonLivenessResult, error) {
	result, err := r.store.RecoverPoisonDeadLetters(ctx, postgres.PoisonLivenessPolicy{
		MaxRecoverAttempts: policy.MaxRecoverAttempts,
		BatchLimit:         policy.BatchLimit,
	}, now)
	if err != nil {
		return reducer.PoisonLivenessResult{}, err
	}
	return reducer.PoisonLivenessResult{
		Recovered: result.Recovered,
	}, nil
}

// poisonLivenessObserver adapts the Postgres poison-liveness store to the
// telemetry.PoisonLivenessObserver contract. Constructed unconditionally
// (unlike the runner) so the stuck-gauge always reports the poison class
// regardless of whether bounded auto-retry is enabled.
type poisonLivenessObserver struct {
	store postgres.PoisonLivenessStore
}

func poisonLivenessObserverFor(database postgres.ExecQueryer) poisonLivenessObserver {
	return poisonLivenessObserver{store: postgres.NewPoisonLivenessStore(database)}
}

// PoisonDeadLetterCounts implements telemetry.PoisonLivenessObserver.
func (o poisonLivenessObserver) PoisonDeadLetterCounts(ctx context.Context) (int64, int64, float64, error) {
	counts, err := o.store.CountPoisonDeadLetters(ctx, time.Now().UTC())
	if err != nil {
		return 0, 0, 0, err
	}
	return counts.PoisonScopes, counts.PoisonItems, counts.OldestPoisonAgeSeconds, nil
}
