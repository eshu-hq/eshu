// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

type postgresGenerationRetentionPruner struct {
	store postgres.GenerationRetentionStore
}

func generationRetentionRunnerFor(
	database postgres.ExecQueryer,
	cfg generationRetentionConfig,
) *reducer.GenerationRetentionRunner {
	if !cfg.Enabled {
		return nil
	}
	return &reducer.GenerationRetentionRunner{
		Pruner: postgresGenerationRetentionPruner{
			store: postgres.NewGenerationRetentionStore(database),
		},
		Config: cfg.Runner,
	}
}

func (p postgresGenerationRetentionPruner) PruneSupersededGenerations(
	ctx context.Context,
	policy reducer.GenerationRetentionPolicy,
) (reducer.GenerationRetentionResult, error) {
	result, err := p.store.PruneSupersededGenerations(ctx, postgres.GenerationRetentionPolicy{
		MinSupersededGenerations: policy.MinSupersededGenerations,
		MaxSupersededAge:         policy.MaxSupersededAge,
		BatchGenerationLimit:     policy.BatchGenerationLimit,
		BatchRowLimit:            policy.BatchRowLimit,
		PolicyScope:              policy.PolicyScope,
		PolicyRevision:           policy.PolicyRevision,
	})
	if err != nil {
		return reducer.GenerationRetentionResult{}, err
	}
	return reducer.GenerationRetentionResult{
		GenerationsPruned: result.GenerationsPruned,
		RowsPruned:        result.RowsPruned,
		Skipped:           result.Skipped,
		OldestEligibleAge: result.OldestEligibleAge,
		Duration:          result.Duration,
	}, nil
}
