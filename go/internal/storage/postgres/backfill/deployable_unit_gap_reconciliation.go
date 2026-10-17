// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backfill

import (
	"context"
	"fmt"
)

// DeployableUnitGapScanLimit bounds one gap-reconciliation run: at most
// this many admitted source repositories are listed from Postgres and
// probed in one graph UNWIND (#8316). It matches the probe's fixed-size
// shape: the scan is a safeguard over admitted decisions, not a backlog.
const DeployableUnitGapScanLimit = 500

// ReconcileDeployableUnitCorrelationGaps is the #8316 gap reconciliation:
// admitted deployable_unit_correlation source repositories with no incident
// outgoing CORRELATES_DEPLOYABLE_UNIT edge in the graph have their reopen
// fingerprint rows deleted, so the next maintenance pass re-opens them and
// re-projects the lost edges. It runs in the bootstrap-index maintenance
// pass, strictly before the correlation reopen, never in the reducer sweep
// cycle: the sweep shares its process with the deployable_unit_correlation
// handler, and any sweep-cycle statement against admission_decisions parks a
// second waiter behind the fault-injection kill cell's exclusive lock, which
// its exact-equality census can never align (#8371). The three seams keep
// the orchestration hermetically testable: listAnchors reads the bounded
// admitted set from Postgres, countIncidentEdges probes the graph once for
// the whole set, and invalidate deletes the fingerprint rows of the gap set.
//
// The steady state performs no graph or delete work: no admitted anchors
// means no probe and no delete, and a fully connected admitted set means no
// delete. A seam error fails the run loudly instead of silently keeping
// lost edges closed: no invalidation runs after a probe error. It returns
// the gap repositories and the invalidated fingerprint row count.
func ReconcileDeployableUnitCorrelationGaps(
	ctx context.Context,
	listAnchors func(context.Context, int) ([]string, error),
	countIncidentEdges func(context.Context, []string) (map[string]int64, error),
	invalidate func(context.Context, []string) (int64, error),
	limit int,
) (gapRepos []string, invalidated int64, err error) {
	if limit <= 0 {
		limit = DeployableUnitGapScanLimit
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
