// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"strings"
	"testing"
)

// TestMaintenancePassReconcilesGapsBeforeCorrelationReopen proves the #8316
// ordering invariant: the maintenance pass runs gap reconciliation strictly
// before the correlation reopen, so invalidated fingerprint rows re-open in
// the same pass. The stub fails the run if the reopen already happened when
// the gap step runs.
func TestMaintenancePassReconcilesGapsBeforeCorrelationReopen(t *testing.T) {
	t.Parallel()

	source := &fakeSource{generations: nil}
	ws := &concurrentWorkSource{items: nil}
	sink := &concurrentWorkSink{}
	committer := &fakeCommitter{}
	gap := &stubGapReconciliation{
		gaps:       []string{"repo-lost"},
		invalidate: 1,
		orderCheck: func() {
			for _, call := range committer.snapshotCalls() {
				if call == "reopen_correlation" {
					t.Errorf("gap reconciliation ran after the correlation reopen, want strictly before")
				}
			}
		},
	}

	cd := collectorDeps{source: source, committer: committer, gapReconciler: gap}
	pd := projectorDeps{
		workSource:    ws,
		factStore:     &fakeFactStore{},
		runner:        &fakeProjectionRunner{},
		workSink:      sink,
		baselineFence: passBootstrapBaselineFence{},
		writeMarker:   passBootstrapBaselineFence{},
	}

	if err := runPipelined(context.Background(), cd, pd, 2, nil, nil, nil); err != nil {
		t.Fatalf("runPipelined() error = %v, want nil", err)
	}
	if gap.calls != 1 {
		t.Fatalf("gap reconcile calls = %d, want exactly 1", gap.calls)
	}
	reopened := false
	for _, call := range committer.snapshotCalls() {
		if call == "reopen_correlation" {
			reopened = true
		}
	}
	if !reopened {
		t.Fatal("correlation reopen did not run after gap reconciliation")
	}
}

// TestMaintenancePassWithoutGapReconcilerFailsClosed proves a miswire can
// never silently disable the repair: a nil gap reconciler fails the
// maintenance pass instead of skipping the gap step.
func TestMaintenancePassWithoutGapReconcilerFailsClosed(t *testing.T) {
	t.Parallel()

	source := &fakeSource{generations: nil}
	ws := &concurrentWorkSource{items: nil}
	sink := &concurrentWorkSink{}

	cd := collectorDeps{source: source, committer: &fakeCommitter{}}
	pd := projectorDeps{
		workSource:    ws,
		factStore:     &fakeFactStore{},
		runner:        &fakeProjectionRunner{},
		workSink:      sink,
		baselineFence: passBootstrapBaselineFence{},
		writeMarker:   passBootstrapBaselineFence{},
	}

	err := runPipelined(context.Background(), cd, pd, 2, nil, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "gap reconciler is required") {
		t.Fatalf("runPipelined() error = %v, want the missing-reconciler failure", err)
	}
}
