package main

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// postgresGenerationLivenessRecoverer adapts the Postgres liveness store to the
// reducer's GenerationLivenessRecoverer contract, translating the reducer-side
// policy into the storage-side policy.
type postgresGenerationLivenessRecoverer struct {
	store postgres.GenerationLivenessStore
}

func generationLivenessRunnerFor(
	database postgres.ExecQueryer,
	cfg generationLivenessConfig,
) *reducer.GenerationLivenessRunner {
	if !cfg.Enabled {
		return nil
	}
	return &reducer.GenerationLivenessRunner{
		Recoverer: postgresGenerationLivenessRecoverer{
			store: postgres.NewGenerationLivenessStore(database),
		},
		Config: cfg.Runner,
	}
}

func (r postgresGenerationLivenessRecoverer) RecoverWedgedGenerations(
	ctx context.Context,
	policy reducer.GenerationLivenessPolicy,
	now time.Time,
) (reducer.GenerationLivenessResult, error) {
	result, err := r.store.RecoverWedgedGenerations(ctx, postgres.GenerationLivenessPolicy{
		ActivationDeadline: policy.ActivationDeadline,
		MaxRecoverAttempts: policy.MaxRecoverAttempts,
		BatchLimit:         policy.BatchLimit,
	}, now)
	if err != nil {
		return reducer.GenerationLivenessResult{}, err
	}
	return reducer.GenerationLivenessResult{
		Superseded: result.Superseded,
		Recovered:  result.Recovered,
	}, nil
}

// activeGenerationAgeObserver adapts the Postgres liveness store to the
// telemetry observer contract for the active-generation age-bucket gauge.
type activeGenerationAgeObserver struct {
	store  postgres.GenerationLivenessStore
	policy postgres.GenerationLivenessPolicy
}

func activeGenerationAgeObserverFor(
	database postgres.ExecQueryer,
	cfg generationLivenessConfig,
) activeGenerationAgeObserver {
	return activeGenerationAgeObserver{
		store: postgres.NewGenerationLivenessStore(database),
		policy: postgres.GenerationLivenessPolicy{
			ActivationDeadline: cfg.Runner.Policy.ActivationDeadline,
			MaxRecoverAttempts: cfg.Runner.Policy.MaxRecoverAttempts,
			BatchLimit:         cfg.Runner.Policy.BatchLimit,
		},
	}
}

// ActiveGenerationsByAge returns active generation counts keyed by the closed
// fresh/aging/stuck age buckets for the observable gauge callback.
func (o activeGenerationAgeObserver) ActiveGenerationsByAge(ctx context.Context) (map[string]int64, error) {
	return o.store.CountActiveGenerationsByAge(ctx, o.policy, time.Now().UTC())
}
