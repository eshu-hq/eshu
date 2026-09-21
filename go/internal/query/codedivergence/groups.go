// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codedivergence

import (
	"context"
)

// GroupStore is the narrow content-store surface group ranking needs:
// fingerprint-group stats plus drifted-finding stats. ContentReader
// satisfies it; the read surface passes its own store through.
type GroupStore interface {
	DivergenceGroupStats(ctx context.Context, repoID string, kind Kind, floor int) ([]GroupStat, error)
	DriftedFindingStats(ctx context.Context, repoID string) ([]GroupStat, error)
}

// PhaseOneStats ranks every multi-member group in the repo (narrow stat
// rows, cheap at corpus scale) across the requested kinds. Wrapper-bypass
// nominates from exact groups, re-stamped when the exact kind itself is not
// requested, so one family never double-reports. Convention-outlier sweeps
// cohorts from the graph instead; it never stats the content store, so no
// group can double-report as both a fingerprint family and an outlier.
func PhaseOneStats(
	ctx context.Context,
	store GroupStore,
	repoID string,
	kinds []Kind,
	wrapperRequested, exactRequested bool,
) ([]GroupStat, error) {
	stats := make([]GroupStat, 0, 64)
	for _, kind := range kinds {
		if kind == KindDrifted {
			driftedStats, err := store.DriftedFindingStats(ctx, repoID)
			if err != nil {
				return nil, err
			}
			stats = append(stats, driftedStats...)
			continue
		}
		if kind == KindWrapperBypass {
			continue
		}
		if kind == KindConventionOutlier {
			continue
		}
		kindStats, err := store.DivergenceGroupStats(ctx, repoID, kind, TokenFloor)
		if err != nil {
			return nil, err
		}
		stats = append(stats, kindStats...)
	}
	if wrapperRequested && !exactRequested {
		// Wrapper-only page: the exact stats still nominate, re-stamped
		// so finding ids derive from the wrapper kind.
		nominators, err := store.DivergenceGroupStats(ctx, repoID, KindExact, TokenFloor)
		if err != nil {
			return nil, err
		}
		for _, stat := range nominators {
			stat.Kind = KindWrapperBypass
			stats = append(stats, stat)
		}
	}
	return stats, nil
}
