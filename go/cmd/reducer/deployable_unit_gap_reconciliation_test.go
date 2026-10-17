// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
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
	reconciled, deleted, err := reconcileDeployableUnitCorrelationGaps(
		context.Background(),
		func(context.Context, int) ([]string, error) {
			return []string{"repo-lost", "repo-healthy", "repo-node-gone"}, nil
		},
		func(context.Context, []string) (map[string]int64, error) {
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
}

// TestReconcileDeployableUnitCorrelationGapsSkipsGraphAndDeleteWhenClean
// proves the bounded steady state: no admitted anchors means no graph probe
// and no fingerprint delete; a fully connected admitted set means no delete.
func TestReconcileDeployableUnitCorrelationGapsSkipsGraphAndDeleteWhenClean(t *testing.T) {
	t.Parallel()

	probed := false
	deleted := false
	gotLimit := 0
	reconciled, rows, err := reconcileDeployableUnitCorrelationGaps(
		context.Background(),
		func(_ context.Context, limit int) ([]string, error) {
			gotLimit = limit
			return nil, nil
		},
		func(context.Context, []string) (map[string]int64, error) {
			probed = true
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
	if gotLimit != 10 {
		t.Fatalf("listing limit = %d, want the passed bound 10", gotLimit)
	}

	// A non-positive limit falls back to the fixed scan bound.
	_, _, err = reconcileDeployableUnitCorrelationGaps(
		context.Background(),
		func(_ context.Context, limit int) ([]string, error) {
			gotLimit = limit
			return nil, nil
		},
		func(context.Context, []string) (map[string]int64, error) {
			probed = true
			return map[string]int64{}, nil
		},
		func(context.Context, []string) (int64, error) {
			deleted = true
			return 0, nil
		},
		0,
	)
	if err != nil {
		t.Fatalf("reconcile error = %v", err)
	}
	if gotLimit != deployableUnitGapScanLimit {
		t.Fatalf("listing limit = %d, want the fixed bound %d", gotLimit, deployableUnitGapScanLimit)
	}
	if probed || deleted {
		t.Fatalf("empty reconcile probed graph = %v, deleted = %v, want no work", probed, deleted)
	}
	if probed || deleted {
		t.Fatalf("clean reconcile probed graph = %v, deleted = %v, want no graph or delete work", probed, deleted)
	}

	reconciled, _, err = reconcileDeployableUnitCorrelationGaps(
		context.Background(),
		func(context.Context, int) ([]string, error) { return []string{"repo-healthy"}, nil },
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
// failing seam fails the cycle loudly instead of silently keeping lost edges
// closed: no invalidation runs after a probe error.
func TestReconcileDeployableUnitCorrelationGapsPropagatesSeamErrors(t *testing.T) {
	t.Parallel()

	probeErr := errors.New("graph probe failed")
	deleted := false
	_, _, err := reconcileDeployableUnitCorrelationGaps(
		context.Background(),
		func(context.Context, int) ([]string, error) { return []string{"repo-a"}, nil },
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
