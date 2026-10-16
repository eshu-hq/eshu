// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance/poison"
	pgpoison "github.com/eshu-hq/eshu/go/internal/storage/postgres/poison"
)

// postgresPoisonLivenessRecoverer adapts the Postgres poison-liveness store to
// the poison.Recoverer contract (#4740).
type postgresPoisonLivenessRecoverer struct {
	store pgpoison.Store
}

// poisonLivenessRunnerFor constructs the bounded poison-recovery runner only
// when auto-retry is enabled. The default posture (AutoRetryEnabled=false) is
// surface-only: this returns nil and no dead_letter row is ever re-driven by
// this runner, while the stuck-gauge (wired separately, see
// poisonLivenessObserverFor) remains active regardless.
func poisonLivenessRunnerFor(
	database db.ExecQueryer,
	cfg poisonLivenessConfig,
) *poison.Runner {
	if !cfg.Runner.AutoRetryEnabled {
		return nil
	}
	return &poison.Runner{
		Recoverer: postgresPoisonLivenessRecoverer{
			store: pgpoison.NewStore(database),
		},
		Config: cfg.Runner,
	}
}

func (r postgresPoisonLivenessRecoverer) RecoverPoisonDeadLetters(
	ctx context.Context,
	policy poison.Policy,
	now time.Time,
) (poison.Result, error) {
	result, err := r.store.RecoverDeadLetters(ctx, pgpoison.Policy{
		MaxRecoverAttempts: policy.MaxRecoverAttempts,
		BatchLimit:         policy.BatchLimit,
	}, now)
	if err != nil {
		return poison.Result{}, err
	}
	return poison.Result{
		Recovered: result.Recovered,
	}, nil
}

// poisonLivenessObserver adapts the Postgres poison-liveness store to the
// telemetry.PoisonLivenessObserver contract. Constructed unconditionally
// (unlike the runner) so the stuck-gauge always reports the poison class
// regardless of whether bounded auto-retry is enabled.
type poisonLivenessObserver struct {
	store pgpoison.Store
}

func poisonLivenessObserverFor(database db.ExecQueryer) poisonLivenessObserver {
	return poisonLivenessObserver{store: pgpoison.NewStore(database)}
}

// PoisonDeadLetterCounts implements telemetry.PoisonLivenessObserver.
func (o poisonLivenessObserver) PoisonDeadLetterCounts(ctx context.Context) (int64, int64, float64, error) {
	counts, err := o.store.CountDeadLetters(ctx, time.Now().UTC())
	if err != nil {
		return 0, 0, 0, err
	}
	return counts.PoisonScopes, counts.PoisonItems, counts.OldestPoisonAgeSeconds, nil
}
