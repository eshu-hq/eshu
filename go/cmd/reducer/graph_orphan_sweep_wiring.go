// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance/orphan"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	sweeporphan "github.com/eshu-hq/eshu/go/internal/storage/cypher/sweep/orphan"
)

// deployableUnitGapPostgres is the #8316 Postgres seam the sweep cycle uses
// after the graph commit: push-invalidate the fingerprint rows of swept
// repositories. *postgres.RelationshipStore is the production
// implementation. A nil seam disables the push invalidation, leaving the
// sweep exactly as before. Gap reconciliation lives in the bootstrap-index
// maintenance pass, never here: any sweep-cycle statement against
// admission_decisions parks a second waiter behind the fault-injection kill
// cell's exclusive lock, which its exact-equality census can never align
// (#8371).
type deployableUnitGapPostgres interface {
	InvalidateDeployableUnitReopenFingerprintsForRepos(ctx context.Context, repoIDs []string) (int64, error)
}

type graphOrphanSweeper struct {
	store        *sweeporphan.OrphanSweepStore
	fingerprints deployableUnitGapPostgres
}

func graphOrphanSweepRunnerFor(
	executor sourcecypher.Executor,
	reader query.GraphQuery,
	leaseManager reducer.PartitionLeaseManager,
	cfg graphOrphanSweepConfig,
	fingerprints deployableUnitGapPostgres,
) *orphan.Runner {
	if !cfg.Enabled {
		return nil
	}
	store := sweeporphan.NewOrphanSweepStore(executor, reader)
	store.CountLimit = cfg.Runner.Policy.CountLimit
	return &orphan.Runner{
		Sweeper:      graphOrphanSweeper{store: store, fingerprints: fingerprints},
		LeaseManager: leaseManager,
		Config:       cfg.Runner,
	}
}

func (s graphOrphanSweeper) SweepOrphanNodes(
	ctx context.Context,
	policy orphan.Policy,
) (orphan.Result, error) {
	result, err := s.store.SweepOrphanNodes(ctx, sweeporphan.OrphanSweepPolicy{
		OrphanTTL:  policy.OrphanTTL,
		BatchLimit: policy.BatchLimit,
		CountLimit: policy.CountLimit,
		Labels:     policy.Labels,
	})
	if err != nil {
		return orphan.Result{}, err
	}
	out := orphan.Result{
		Counts:   result.Counts,
		Marked:   result.Marked,
		Deleted:  result.Deleted,
		Skipped:  result.Skipped,
		Duration: result.Duration,
	}
	if s.fingerprints == nil {
		return out, nil
	}
	// Push invalidation (#8316): the Repository nodes the cycle just
	// deleted lost every incident edge, so the fingerprint rows whose
	// repo_ids contain them go now, under the same sweep lease.
	if len(result.DeletedRepositoryIDs) > 0 {
		deleted, err := s.fingerprints.InvalidateDeployableUnitReopenFingerprintsForRepos(ctx, result.DeletedRepositoryIDs)
		if err != nil {
			return orphan.Result{}, fmt.Errorf("invalidate deployable unit reopen fingerprints for swept repositories: %w", err)
		}
		out.InvalidatedFingerprints += deleted
	}
	return out, nil
}

func (s graphOrphanSweeper) GraphOrphanNodeCounts(ctx context.Context) (map[string]int64, error) {
	return s.store.GraphOrphanNodeCounts(ctx)
}
