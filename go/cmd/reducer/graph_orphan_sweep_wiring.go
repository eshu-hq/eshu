// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

type graphOrphanSweeper struct {
	store *sourcecypher.OrphanSweepStore
}

func graphOrphanSweepRunnerFor(
	executor sourcecypher.Executor,
	reader query.GraphQuery,
	leaseManager reducer.PartitionLeaseManager,
	cfg graphOrphanSweepConfig,
) *reducer.GraphOrphanSweepRunner {
	if !cfg.Enabled {
		return nil
	}
	store := sourcecypher.NewOrphanSweepStore(executor, reader)
	store.CountLimit = cfg.Runner.Policy.CountLimit
	return &reducer.GraphOrphanSweepRunner{
		Sweeper:      graphOrphanSweeper{store: store},
		LeaseManager: leaseManager,
		Config:       cfg.Runner,
	}
}

func (s graphOrphanSweeper) SweepOrphanNodes(
	ctx context.Context,
	policy reducer.GraphOrphanSweepPolicy,
) (reducer.GraphOrphanSweepResult, error) {
	result, err := s.store.SweepOrphanNodes(ctx, sourcecypher.OrphanSweepPolicy{
		OrphanTTL:  policy.OrphanTTL,
		BatchLimit: policy.BatchLimit,
		CountLimit: policy.CountLimit,
		Labels:     policy.Labels,
	})
	if err != nil {
		return reducer.GraphOrphanSweepResult{}, err
	}
	return reducer.GraphOrphanSweepResult{
		Counts:   result.Counts,
		Marked:   result.Marked,
		Deleted:  result.Deleted,
		Skipped:  result.Skipped,
		Duration: result.Duration,
	}, nil
}

func (s graphOrphanSweeper) GraphOrphanNodeCounts(ctx context.Context) (map[string]int64, error) {
	return s.store.GraphOrphanNodeCounts(ctx)
}
