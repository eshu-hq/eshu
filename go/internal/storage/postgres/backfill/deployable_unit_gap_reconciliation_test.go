// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backfill

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"testing"
)

// TestReconcileDeployableUnitCorrelationGapsInvalidatesZeroEdgeAnchors proves
// the #8316 gap reconciliation: admitted source repositories with no incident
// CORRELATES_DEPLOYABLE_UNIT edge in the graph have their fingerprint rows
// deleted so the next maintenance pass re-opens them. Anchors the graph
// still connects keep their rows.
func TestReconcileDeployableUnitCorrelationGapsInvalidatesZeroEdgeAnchors(t *testing.T) {
	t.Parallel()

	var invalidated [][]string
	var probed [][]string
	reconciled, deleted, err := ReconcileDeployableUnitCorrelationGaps(
		context.Background(),
		func(context.Context) ([]string, error) {
			return []string{"repo-lost", "repo-healthy", "repo-node-gone"}, nil
		},
		func(_ context.Context, repos []string) (map[string]int64, error) {
			probed = append(probed, append([]string(nil), repos...))
			return map[string]int64{"repo-healthy": 2, "repo-lost": 0}, nil
		},
		func(_ context.Context, repos []string) (int64, error) {
			invalidated = append(invalidated, append([]string(nil), repos...))
			return int64(len(repos)), nil
		},
		10,
	)
	if err != nil {
		t.Fatalf("reconcile error = %v", err)
	}
	sort.Strings(reconciled)
	if !reflect.DeepEqual(reconciled, []string{"repo-lost", "repo-node-gone"}) {
		t.Fatalf("gap repos = %#v, want [repo-lost repo-node-gone]", reconciled)
	}
	if deleted != 2 {
		t.Fatalf("invalidated rows = %d, want 2", deleted)
	}
	if len(invalidated) != 1 {
		t.Fatalf("invalidate calls = %d, want exactly 1 with both gap repos", len(invalidated))
	}
	sort.Strings(invalidated[0])
	if !reflect.DeepEqual(invalidated[0], []string{"repo-lost", "repo-node-gone"}) {
		t.Fatalf("invalidated repos = %#v, want both gap repos", invalidated[0])
	}
	if len(probed) != 1 || len(probed[0]) != 3 {
		t.Fatalf("probe batches = %#v, want one batch of 3", probed)
	}
}

// TestReconcileDeployableUnitCorrelationGapsBatchesProbeAcrossScanBound
// proves the #8371 review F1 fix: the listing carries no LIMIT, so a set
// larger than the scan bound is probed batch by batch and tail anchors are
// covered. A fixed first-N prefix would deterministically starve them.
func TestReconcileDeployableUnitCorrelationGapsBatchesProbeAcrossScanBound(t *testing.T) {
	t.Parallel()

	const total = 1200
	anchors := make([]string, 0, total)
	for i := 0; i < total; i++ {
		anchors = append(anchors, fmt.Sprintf("repo-%04d", i))
	}
	var batchSizes []int
	var invalidated [][]string
	reconciled, deleted, err := ReconcileDeployableUnitCorrelationGaps(
		context.Background(),
		func(context.Context) ([]string, error) { return anchors, nil },
		func(_ context.Context, repos []string) (map[string]int64, error) {
			batchSizes = append(batchSizes, len(repos))
			// Only the very last anchor keeps its edge: every other
			// anchor, in every batch, must surface as a gap.
			if len(repos) > 0 && repos[len(repos)-1] == "repo-1199" {
				return map[string]int64{"repo-1199": 1}, nil
			}
			return map[string]int64{}, nil
		},
		func(_ context.Context, repos []string) (int64, error) {
			invalidated = append(invalidated, append([]string(nil), repos...))
			return int64(len(repos)), nil
		},
		// Non-positive batch size falls back to the fixed scan bound,
		// which this also proves: three bound-sized round trips.
		0,
	)
	if err != nil {
		t.Fatalf("reconcile error = %v", err)
	}
	if !reflect.DeepEqual(batchSizes, []int{500, 500, 200}) {
		t.Fatalf("probe batch sizes = %v, want [500 500 200]", batchSizes)
	}
	if len(reconciled) != total-1 {
		t.Fatalf("gap repos = %d, want %d (every anchor but the connected tail)", len(reconciled), total-1)
	}
	if reconciled[len(reconciled)-1] != "repo-1198" {
		t.Fatalf("last gap repo = %q, want repo-1198: the tail batch was not covered", reconciled[len(reconciled)-1])
	}
	if deleted != total-1 {
		t.Fatalf("invalidated rows = %d, want %d", deleted, total-1)
	}
	if len(invalidated) != 1 {
		t.Fatalf("invalidate calls = %d, want exactly 1 with the full gap set", len(invalidated))
	}
}

// TestReconcileDeployableUnitCorrelationGapsSkipsGraphAndDeleteWhenClean
// proves the bounded steady state: no admitted anchors means no graph probe
// and no fingerprint delete; a fully connected admitted set means no delete;
// a non-positive batch size falls back to the fixed scan bound.
func TestReconcileDeployableUnitCorrelationGapsSkipsGraphAndDeleteWhenClean(t *testing.T) {
	t.Parallel()

	deleted := false
	reconciled, rows, err := ReconcileDeployableUnitCorrelationGaps(
		context.Background(),
		func(context.Context) ([]string, error) { return nil, nil },
		func(context.Context, []string) (map[string]int64, error) {
			t.Fatal("graph probe ran with no admitted anchors, want no probe work")
			return map[string]int64{}, nil
		},
		func(context.Context, []string) (int64, error) {
			deleted = true
			return 0, nil
		},
		10,
	)
	if err != nil {
		t.Fatalf("reconcile error = %v", err)
	}
	if len(reconciled) != 0 || rows != 0 {
		t.Fatalf("clean reconcile = (%#v, %d), want (empty, 0)", reconciled, rows)
	}

	reconciled, _, err = ReconcileDeployableUnitCorrelationGaps(
		context.Background(),
		func(context.Context) ([]string, error) { return []string{"repo-healthy"}, nil },
		func(context.Context, []string) (map[string]int64, error) {
			return map[string]int64{"repo-healthy": 1}, nil
		},
		func(context.Context, []string) (int64, error) {
			deleted = true
			return 0, nil
		},
		10,
	)
	if err != nil {
		t.Fatalf("reconcile error = %v", err)
	}
	if len(reconciled) != 0 {
		t.Fatalf("connected reconcile gaps = %#v, want empty", reconciled)
	}
	if deleted {
		t.Fatalf("connected reconcile deleted fingerprint rows, want no delete")
	}
}

// TestReconcileDeployableUnitCorrelationGapsPropagatesSeamErrors proves a
// failing seam fails the run loudly instead of silently keeping lost edges
// closed: no invalidation runs after a probe error.
func TestReconcileDeployableUnitCorrelationGapsPropagatesSeamErrors(t *testing.T) {
	t.Parallel()

	probeErr := errors.New("graph probe failed")
	deleted := false
	_, _, err := ReconcileDeployableUnitCorrelationGaps(
		context.Background(),
		func(context.Context) ([]string, error) { return []string{"repo-a"}, nil },
		func(context.Context, []string) (map[string]int64, error) { return nil, probeErr },
		func(context.Context, []string) (int64, error) {
			deleted = true
			return 0, nil
		},
		10,
	)
	if !errors.Is(err, probeErr) {
		t.Fatalf("reconcile error = %v, want the probe error", err)
	}
	if deleted {
		t.Fatalf("reconcile deleted fingerprint rows after a probe error, want no delete")
	}
}
