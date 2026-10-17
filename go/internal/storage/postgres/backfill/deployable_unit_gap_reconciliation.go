// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backfill

import (
	"context"
	"fmt"
)

// DeployableUnitGapScanLimit bounds one gap-reconciliation graph round
// trip: at most this many admitted source repositories are probed in one
// UNWIND (#8316). It matches the probe's fixed-size shape; the listing
// itself is the full admitted set, probed batch by batch, so no tail anchor
// starves (#8371 review F1).
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
// the orchestration hermetically testable: listAnchors reads the full
// admitted set from Postgres, countIncidentEdges probes the graph batch by
// batch, and invalidate deletes the fingerprint rows of the gap set.
//
// The listing carries no LIMIT — every pass must cover the whole set, or
// tail anchors starve behind a fixed first-N prefix (#8371 review F1) —
// while every graph round trip stays bounded by batchSize. The steady state
// performs no graph or delete work: no admitted anchors means no probe and
// no delete, and a fully connected admitted set means no delete. A seam
// error fails the run loudly instead of silently keeping lost edges closed:
// no invalidation runs after a probe error. It returns the gap repositories
// and the invalidated fingerprint row count.
func ReconcileDeployableUnitCorrelationGaps(
	ctx context.Context,
	listAnchors func(context.Context) ([]string, error),
	countIncidentEdges func(context.Context, []string) (map[string]int64, error),
	invalidate func(context.Context, []string) (int64, error),
	batchSize int,
) (gapRepos []string, invalidated int64, err error) {
	if batchSize <= 0 {
		batchSize = DeployableUnitGapScanLimit
	}
	anchors, err := listAnchors(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list deployable unit admitted anchors: %w", err)
	}
	if len(anchors) == 0 {
		return nil, 0, nil
	}
	for start := 0; start < len(anchors); start += batchSize {
		end := start + batchSize
		if end > len(anchors) {
			end = len(anchors)
		}
		counts, err := countIncidentEdges(ctx, anchors[start:end])
		if err != nil {
			return nil, 0, fmt.Errorf("probe deployable unit correlation edges: %w", err)
		}
		for _, anchor := range anchors[start:end] {
			if counts[anchor] == 0 {
				gapRepos = append(gapRepos, anchor)
			}
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
