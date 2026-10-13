// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"fmt"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// reconcileGaugeByReason reads an Int64 gauge's datapoints by their reason
// attribute, mirroring reconcileCounterByReason for gauges.
func reconcileGaugeByReason(t *testing.T, reader *sdkmetric.ManualReader, name string) map[string]int64 {
	t.Helper()
	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	out := map[string]int64{}
	for _, scopeMetrics := range metrics.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			if m.Name != name {
				continue
			}
			gauge, ok := m.Data.(metricdata.Gauge[int64])
			if !ok {
				t.Fatalf("%s data = %T, want Gauge[int64]", name, m.Data)
			}
			for _, point := range gauge.DataPoints {
				attrs := point.Attributes.ToSlice()
				if len(attrs) != 1 || string(attrs[0].Key) != "reason" {
					t.Fatalf("%s attributes = %v, want only the bounded reason", name, attrs)
				}
				out[attrs[0].Value.AsString()] += point.Value
			}
		}
	}
	return out
}

// rotationWorld builds five always-due repositories: the stub resolver reports
// no full generation for any scope, so every scope is due with
// never_reconciled on every cycle, no matter how many fulls were forced.
func rotationWorld(t *testing.T) (reposDir string, repos []string) {
	t.Helper()
	reposDir = t.TempDir()
	repos = make([]string, 0, 5)
	for i := range 5 {
		repos = append(repos, fmt.Sprintf("github/org/repo-%02d", i))
	}
	writeFreshScopesFakeGit(t, reposDir, repos)
	return reposDir, repos
}

// TestReconcileSweepRotationVisitsEveryDueRepoWithinBound is the #7987
// regression: with more due repositories than the per-cycle limit, the sweep
// rotates its start position so every due repository is forced within
// ceil(due/limit) cycles, and the due-unvisited gauge reports the remainder.
func TestReconcileSweepRotationVisitsEveryDueRepoWithinBound(t *testing.T) {
	reposDir, repos := rotationWorld(t)
	config := reconcileTestConfig(reposDir)
	config.ReconcileMaxPerCycle = 2
	now := time.Date(2026, 10, 12, 1, 0, 0, 0, time.UTC)
	instruments, reader := newCollisionTestInstruments(t)

	visited := map[string]bool{}
	next := 0
	var firstGauge map[string]int64
	for cycle := range 3 {
		var synced GitSyncSelection
		var err error
		synced, err = syncGitRepositoriesWithLogger(context.Background(), config, repos, discardLogger(),
			gitDeltaBaseline{
				Resolver:             &stubBaselineResolver{},
				Instruments:          instruments,
				Reconcile:            reconcilePolicyFromConfig(config),
				ReconcileStartOffset: next,
				Now:                  func() time.Time { return now.Add(time.Duration(cycle) * 6 * time.Minute) },
			})
		if err != nil {
			t.Fatalf("cycle %d: syncGitRepositoriesWithLogger() error = %v", cycle, err)
		}
		for path := range synced.ReconcileByRepoPath {
			visited[path] = true
		}
		if cycle == 0 {
			firstGauge = reconcileGaugeByReason(t, reader, "eshu_dp_collector_reconciliation_due_unvisited")
		}
		next = synced.NextReconcileStartOffset
	}

	if len(visited) != len(repos) {
		t.Fatalf("visited %d of %d due repositories in 3 cycles (bound ceil(5/2)), want every one", len(visited), len(repos))
	}
	if got := firstGauge[reconcileReasonNeverReconciled]; got != 3 {
		t.Fatalf("cycle-0 due-unvisited gauge = %v, want never_reconciled=3 (5 due, limit 2)", firstGauge)
	}
	if len(firstGauge) != 8 {
		t.Fatalf("cycle-0 due-unvisited gauge reasons = %v, want all 8 closed reasons recorded including zero", firstGauge)
	}
	for _, reason := range reconcileDueUnvisitedReasons {
		if reason == reconcileReasonNeverReconciled {
			continue
		}
		if got := firstGauge[reason]; got != 0 {
			t.Fatalf("cycle-0 due-unvisited gauge[%s] = %d, want 0", reason, got)
		}
	}
	if next != 1 {
		t.Fatalf("offset after 3 cycles = %d, want 1 (0 -> 2 -> 4 -> 1)", next)
	}
}

// TestReconcileSweepFixedOrderStarvesLateRepositories is the seeded violation
// for #7987: with the start position pinned at zero every cycle (the old
// fixed order), the last repository never reaches a slot, so this documents
// the starvation the rotation test above must catch.
func TestReconcileSweepFixedOrderStarvesLateRepositories(t *testing.T) {
	reposDir, repos := rotationWorld(t)
	config := reconcileTestConfig(reposDir)
	config.ReconcileMaxPerCycle = 2
	now := time.Date(2026, 10, 12, 1, 0, 0, 0, time.UTC)

	visited := map[string]bool{}
	for cycle := range 3 {
		synced, err := syncGitRepositoriesWithLogger(context.Background(), config, repos, discardLogger(),
			gitDeltaBaseline{
				Resolver:             &stubBaselineResolver{},
				Reconcile:            reconcilePolicyFromConfig(config),
				ReconcileStartOffset: 0,
				Now:                  func() time.Time { return now.Add(time.Duration(cycle) * 6 * time.Minute) },
			})
		if err != nil {
			t.Fatalf("cycle %d: syncGitRepositoriesWithLogger() error = %v", cycle, err)
		}
		for path := range synced.ReconcileByRepoPath {
			visited[path] = true
		}
	}
	if len(visited) != 2 {
		t.Fatalf("fixed order visited %d repositories in 3 cycles, want exactly the first 2 (late repositories starve)", len(visited))
	}
}

// TestNativeSelectorAdvancesReconcileWalkAcrossCycles proves the production
// threading: consecutive SelectRepositories calls on one selector resume the
// walk where the previous cycle stopped.
func TestNativeSelectorAdvancesReconcileWalkAcrossCycles(t *testing.T) {
	reposDir, repos := rotationWorld(t)
	config := reconcileTestConfig(reposDir)
	config.ReconcileMaxPerCycle = 2
	now := time.Date(2026, 10, 12, 1, 0, 0, 0, time.UTC)
	selector := NativeRepositorySelector{
		Config: config,
		Now:    func() time.Time { return now },
		DiscoverSelection: func(context.Context, RepoSyncConfig, string) (RepositorySelection, error) {
			return RepositorySelection{RepositoryIDs: repos}, nil
		},
		Logger:           discardLogger(),
		BaselineResolver: &stubBaselineResolver{},
	}

	forcedSets := make([]map[string]bool, 0, 2)
	for range 2 {
		batch, err := selector.SelectRepositories(context.Background())
		if err != nil {
			t.Fatalf("SelectRepositories() error = %v", err)
		}
		forced := map[string]bool{}
		for _, repo := range batch.Repositories {
			if repo.Reconcile {
				forced[repo.RepoPath] = true
			}
		}
		forcedSets = append(forcedSets, forced)
	}
	if len(forcedSets[0]) != 2 || len(forcedSets[1]) != 2 {
		t.Fatalf("forced sets = %v, want 2 forced repositories per cycle", forcedSets)
	}
	for path := range forcedSets[0] {
		if forcedSets[1][path] {
			t.Fatalf("second cycle re-forced %s, want the walk to resume past the first cycle", path)
		}
	}
}
