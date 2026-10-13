// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/scope"
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

// TestReconcileDueUnvisitedReasonsCoverEveryDueReason links the
// due-unvisited gauge's closed set to its producers (#7987 review): it drives
// decide, decideGraphDirty, decideReindex, and the repository-watermark path
// of decideForScope to every Due=true reason and requires the gauge set to
// contain exactly those reasons. A ninth due reason fails here instead of
// silently missing the gauge.
func TestReconcileDueUnvisitedReasonsCoverEveryDueReason(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 12, 1, 0, 0, 0, time.UTC)
	policy := reconcilePolicy{Interval: 24 * time.Hour}
	due := map[string]bool{}
	note := func(dueFlag bool, reason string) {
		if dueFlag {
			due[reason] = true
		}
	}

	note(policy.decide(now, scope.FullReconcileState{}))
	note(policy.decide(now, scope.FullReconcileState{
		HasProjectedFull: true, LastProjectedFullAt: now.Add(-48 * time.Hour),
	}))
	note(policy.decide(now, scope.FullReconcileState{
		HasLatestFull: true, LatestFullStatus: scope.GenerationStatusPending,
		LatestFullAt: now.Add(-48 * time.Hour),
	}))
	note(policy.decide(now, scope.FullReconcileState{
		HasLatestFull: true, LatestFullStatus: scope.GenerationStatusFailed,
		LatestFullAt: now.Add(-7 * time.Hour),
	}))
	note(policy.decide(now, scope.FullReconcileState{
		HasProjectedFull: true, LastProjectedFullAt: now.Add(-time.Hour),
		ReconcileRequestedAt: now.Add(-30 * time.Minute), ReconcileProjectedAt: now.Add(-time.Hour),
		ReconcileDeadLetteredAt: now.Add(-10 * time.Minute),
	}))
	note(policy.decideGraphDirty(now, scope.FullReconcileState{}))
	note(policy.decideReindex(now, now.Add(-time.Hour), scope.FullReconcileState{}))
	repository := gitDeltaBaseline{
		Resolver:                     &stubBaselineResolver{},
		Reconcile:                    policy,
		RepositoryReindexRequestedAt: map[string]time.Time{"scope-1": now.Add(-time.Hour)},
		Now:                          func() time.Time { return now },
	}
	repositoryDecision := repository.decideForScope(context.Background(), "scope-1", discardLogger())
	note(repositoryDecision.Due, repositoryDecision.Reason)

	for reason := range due {
		if !isReconcileDueUnvisitedReason(reason) {
			t.Fatalf("decide family produced due reason %q missing from the due-unvisited gauge set", reason)
		}
	}
	if len(due) != len(reconcileDueUnvisitedReasons) {
		t.Fatalf("decide family produced %d due reasons %v, want exactly the %d closed gauge reasons",
			len(due), due, len(reconcileDueUnvisitedReasons))
	}
	for _, reason := range reconcileDueUnvisitedReasons {
		if !due[reason] {
			t.Fatalf("gauge reason %q was not produced by any decide-family case; extend the battery or drop the reason", reason)
		}
	}
}

// TestReconcileSweepRotationKeepsRequestedRepositoryFirst proves the pinned
// prefix survives rotation end to end (#7987 review): the last repository in
// input order carries a per-repository watermark on cycle 0, so it is forced
// first with the repository reason, while the rotating suffix still visits
// every due repository within ceil(due/limit) cycles.
func TestReconcileSweepRotationKeepsRequestedRepositoryFirst(t *testing.T) {
	reposDir, repos := rotationWorld(t)
	config := reconcileTestConfig(reposDir)
	config.ReconcileMaxPerCycle = 2
	now := time.Date(2026, 10, 12, 1, 0, 0, 0, time.UTC)
	last := repos[len(repos)-1]
	lastPath := filepath.Join(reposDir, filepath.FromSlash(last))
	requested := managedScopeID(t, reposDir, last)

	visited := map[string]bool{}
	next := 0
	for cycle := range 3 {
		baseline := gitDeltaBaseline{
			Resolver:             &stubBaselineResolver{},
			Reconcile:            reconcilePolicyFromConfig(config),
			ReconcileStartOffset: next,
			Now:                  func() time.Time { return now.Add(time.Duration(cycle) * 6 * time.Minute) },
		}
		if cycle == 0 {
			baseline.RepositoryReindexRequestedAt = map[string]time.Time{requested: now.Add(-time.Hour)}
		}
		synced, err := syncGitRepositoriesWithLogger(context.Background(), config, repos, discardLogger(), baseline)
		if err != nil {
			t.Fatalf("cycle %d: syncGitRepositoriesWithLogger() error = %v", cycle, err)
		}
		for path := range synced.ReconcileByRepoPath {
			visited[path] = true
		}
		if cycle == 0 {
			if !synced.ReconcileByRepoPath[lastPath] {
				t.Fatalf("cycle 0 forced %v, want the watermarked last repository %s forced first", synced.ReconcileByRepoPath, last)
			}
			if got := synced.ReconcileReasonByRepoPath[lastPath]; got != reconcileReasonRepositoryReindexRequested {
				t.Fatalf("cycle-0 reason for %s = %q, want %q", last, got, reconcileReasonRepositoryReindexRequested)
			}
		}
		next = synced.NextReconcileStartOffset
	}
	if len(visited) != len(repos) {
		t.Fatalf("visited %d of %d due repositories in 3 cycles with a pinned prefix, want every one", len(visited), len(repos))
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
