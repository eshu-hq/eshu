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
)

// deployableUnitGapPostgres is the #8316 Postgres seam the sweep cycle uses
// after the graph commit: push-invalidate the fingerprint rows of swept
// repositories, list the bounded admitted set for the gap probe, and
// invalidate the gap set. *postgres.RelationshipStore is the production
// implementation. A nil seam disables both the push invalidation and the
// gap reconciliation, leaving the sweep exactly as before.
type deployableUnitGapPostgres interface {
	ListDeployableUnitAdmittedAnchorRepos(ctx context.Context, limit int) ([]string, error)
	InvalidateDeployableUnitReopenFingerprintsForRepos(ctx context.Context, repoIDs []string) (int64, error)
}

type graphOrphanSweeper struct {
	store        *sourcecypher.OrphanSweepStore
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
	store := sourcecypher.NewOrphanSweepStore(executor, reader)
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
	result, err := s.store.SweepOrphanNodes(ctx, sourcecypher.OrphanSweepPolicy{
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
	// Gap reconciliation (#8316): admitted anchors with no incident edge
	// cover every loss the push paths miss — the NornicDB bare-shape
	// retirement, a crash between the graph commit and the push delete,
	// and any older loss. Same lease, same bounded cycle, no new worker.
	gaps, deleted, err := reconcileDeployableUnitCorrelationGaps(
		ctx,
		s.fingerprints.ListDeployableUnitAdmittedAnchorRepos,
		s.countIncidentCorrelationEdges,
		s.fingerprints.InvalidateDeployableUnitReopenFingerprintsForRepos,
		deployableUnitGapScanLimit,
	)
	if err != nil {
		return orphan.Result{}, err
	}
	out.InvalidatedFingerprints += deleted
	out.DeployableUnitGapRepos = len(gaps)
	return out, nil
}

// countIncidentCorrelationEdges probes which of repos still own an outgoing
// CORRELATES_DEPLOYABLE_UNIT edge, through the sweep's own graph reader: one
// bounded UNWIND round trip, no new connection or worker.
func (s graphOrphanSweeper) countIncidentCorrelationEdges(
	ctx context.Context,
	repos []string,
) (map[string]int64, error) {
	stmt := sourcecypher.BuildDeployableUnitCorrelationIncidentRepos(repos)
	rows, err := s.store.Reader.Run(ctx, stmt.Cypher, stmt.Parameters)
	if err != nil {
		return nil, err
	}
	counts := make(map[string]int64, len(rows))
	for _, row := range rows {
		repoID, _ := row["repo_id"].(string)
		if repoID == "" {
			continue
		}
		counts[repoID]++
	}
	return counts, nil
}

func (s graphOrphanSweeper) GraphOrphanNodeCounts(ctx context.Context) (map[string]int64, error) {
	return s.store.GraphOrphanNodeCounts(ctx)
}

// deployableUnitGapScanLimit bounds one gap-reconciliation cycle: at most
// this many admitted source repositories are listed from Postgres and
// probed in one graph UNWIND (#8316). It matches the probe's fixed-size
// shape, not the sweep's BatchLimit: the scan is a safeguard over admitted
// decisions, not the orphan backlog.
const deployableUnitGapScanLimit = 500

// reconcileDeployableUnitCorrelationGaps is the #8316 gap reconciliation:
// admitted deployable_unit_correlation source repositories with no incident
// outgoing CORRELATES_DEPLOYABLE_UNIT edge in the graph have their reopen
// fingerprint rows deleted, so the next maintenance pass re-opens them and
// re-projects the lost edges. The three seams keep the orchestration
// hermetically testable: listAnchors reads the bounded admitted set from
// Postgres, countIncidentEdges probes the graph once for the whole set, and
// invalidate deletes the fingerprint rows of the gap set.
//
// The steady state performs no graph or delete work: no admitted anchors
// means no probe and no delete, and a fully connected admitted set means no
// delete. A seam error fails the cycle loudly instead of silently keeping
// lost edges closed: no invalidation runs after a probe error. It returns
// the gap repositories and the invalidated fingerprint row count.
func reconcileDeployableUnitCorrelationGaps(
	ctx context.Context,
	listAnchors func(context.Context, int) ([]string, error),
	countIncidentEdges func(context.Context, []string) (map[string]int64, error),
	invalidate func(context.Context, []string) (int64, error),
	limit int,
) (gapRepos []string, invalidated int64, err error) {
	if limit <= 0 {
		limit = deployableUnitGapScanLimit
	}
	anchors, err := listAnchors(ctx, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("list deployable unit admitted anchors: %w", err)
	}
	if len(anchors) == 0 {
		return nil, 0, nil
	}
	counts, err := countIncidentEdges(ctx, anchors)
	if err != nil {
		return nil, 0, fmt.Errorf("probe deployable unit correlation edges: %w", err)
	}
	for _, anchor := range anchors {
		if counts[anchor] == 0 {
			gapRepos = append(gapRepos, anchor)
		}
	}
	if len(gapRepos) == 0 {
		return nil, 0, nil
	}
	invalidated, err = invalidate(ctx, gapRepos)
	if err != nil {
		return nil, 0, fmt.Errorf("invalidate deployable unit reopen fingerprints for gap repos: %w", err)
	}
	return gapRepos, invalidated, nil
}
