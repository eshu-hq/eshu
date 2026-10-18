// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"errors"
	"fmt"
	"math"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/webhook"
)

// stubQueuedTriggerProbe is a programmable QueuedTriggerProbe for the
// #8123 sweep-yield tests.
type stubQueuedTriggerProbe struct {
	queued bool
	err    error
	calls  int
}

func (p *stubQueuedTriggerProbe) HasQueuedAcceptedTriggers(context.Context) (bool, error) {
	p.calls++
	return p.queued, p.err
}

// freshBaselineResolver answers every scope with the fake remote head and a
// projected full one hour old, so a visit records one baseline call.
func freshBaselineResolver(now time.Time) *stubBaselineResolver {
	return &stubBaselineResolver{
		sha: "samesha",
		state: scope.FullReconcileState{
			HasProjectedFull:    true,
			LastProjectedFullAt: now.Add(-time.Hour),
			HasLatestFull:       true,
			LatestFullAt:        now.Add(-time.Hour),
			LatestFullStatus:    scope.GenerationStatus("active"),
			LatestFullProjected: true,
		},
	}
}

func sweepYieldWorld(t *testing.T, n int) (reposDir string, repos []string) {
	t.Helper()
	reposDir = t.TempDir()
	repos = make([]string, 0, n)
	for i := range n {
		repos = append(repos, fmt.Sprintf("github/org/repo-%02d", i))
	}
	writeFreshScopesFakeGit(t, reposDir, repos)
	return reposDir, repos
}

func distinctStrings(values []string) int {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		seen[value] = struct{}{}
	}
	return len(seen)
}

// TestSweepYieldReturnsPartialSelectionAndResumeCoversTail is the sync-level
// #8123 regression: with a trigger queued, the sweep stops at a repository
// boundary and reports where to resume; the resumed walks cover the tail
// exactly once instead of re-fetching the visited prefix.
func TestSweepYieldReturnsPartialSelectionAndResumeCoversTail(t *testing.T) {
	reposDir, repos := sweepYieldWorld(t, 5)
	config := reconcileTestConfig(reposDir)
	now := time.Date(2026, 10, 14, 6, 37, 0, 0, time.UTC)
	resolver := freshBaselineResolver(now)
	probe := &stubQueuedTriggerProbe{queued: true}

	run := func(offset, limit int) GitSyncSelection {
		synced, err := syncGitRepositoriesWithLogger(context.Background(), config, repos, discardLogger(),
			gitDeltaBaseline{
				Resolver:             resolver,
				Reconcile:            reconcilePolicyFromConfig(config),
				ReconcileStartOffset: offset,
				Now:                  func() time.Time { return now },
				SweepYield:           sweepYieldPolicy{Probe: probe, CheckEvery: 2},
				SweepWalkLimit:       limit,
			})
		if err != nil {
			t.Fatalf("syncGitRepositoriesWithLogger() error = %v", err)
		}
		return synced
	}

	first := run(0, 0)
	if !first.YieldedToWebhook {
		t.Fatal("first walk did not yield with a trigger queued from the start")
	}
	if got := len(resolver.scopeIDs); got != 2 {
		t.Fatalf("first walk visited %d repositories, want 2 (check interval) of 5", got)
	}
	if first.NextReconcileStartOffset != 2 || first.SweepRemainingRepositories != 3 {
		t.Fatalf("first walk resume = (offset %d, remaining %d), want (2, 3)",
			first.NextReconcileStartOffset, first.SweepRemainingRepositories)
	}

	second := run(first.NextReconcileStartOffset, first.SweepRemainingRepositories)
	if !second.YieldedToWebhook {
		t.Fatal("resumed walk did not yield with the trigger still queued")
	}
	if second.NextReconcileStartOffset != 4 || second.SweepRemainingRepositories != 1 {
		t.Fatalf("resumed walk resume = (offset %d, remaining %d), want (4, 1)",
			second.NextReconcileStartOffset, second.SweepRemainingRepositories)
	}

	third := run(second.NextReconcileStartOffset, second.SweepRemainingRepositories)
	if third.YieldedToWebhook {
		t.Fatal("final one-repository walk yielded instead of completing the sweep")
	}
	if got := len(resolver.scopeIDs); got != 5 {
		t.Fatalf("logical sweep visited %d repositories, want exactly 5 (no re-fetch past the wrap)", got)
	}
	if got := distinctStrings(resolver.scopeIDs); got != 5 {
		t.Fatalf("logical sweep visited %d distinct repositories, want all 5", got)
	}
}

// TestSweepYieldProbeErrorCompletesSweep pins the degradation contract: a
// probe failure never fails selection; the sweep runs to completion exactly
// as it did before #8123.
func TestSweepYieldProbeErrorCompletesSweep(t *testing.T) {
	reposDir, repos := sweepYieldWorld(t, 5)
	config := reconcileTestConfig(reposDir)
	now := time.Date(2026, 10, 14, 6, 37, 0, 0, time.UTC)
	resolver := freshBaselineResolver(now)
	probe := &stubQueuedTriggerProbe{err: errors.New("probe unavailable")}

	synced, err := syncGitRepositoriesWithLogger(context.Background(), config, repos, discardLogger(),
		gitDeltaBaseline{
			Resolver:   resolver,
			Reconcile:  reconcilePolicyFromConfig(config),
			Now:        func() time.Time { return now },
			SweepYield: sweepYieldPolicy{Probe: probe, CheckEvery: 2},
		})
	if err != nil {
		t.Fatalf("syncGitRepositoriesWithLogger() error = %v, want nil on probe failure", err)
	}
	if synced.YieldedToWebhook {
		t.Fatal("sweep yielded on a probe error")
	}
	if got := len(resolver.scopeIDs); got != 5 {
		t.Fatalf("sweep visited %d repositories after a probe error, want all 5", got)
	}
}

// TestNativeSelectorSweepYieldBoundCompletesSweepUnderTriggerStorm is the
// starvation proof: with a trigger queued at every probe and a bound of one
// yield per sweep, the second cycle runs the remaining tail to completion,
// and the next sweep may yield again.
func TestNativeSelectorSweepYieldBoundCompletesSweepUnderTriggerStorm(t *testing.T) {
	reposDir, repos := sweepYieldWorld(t, 5)
	config := reconcileTestConfig(reposDir)
	now := time.Date(2026, 10, 14, 6, 37, 0, 0, time.UTC)
	resolver := freshBaselineResolver(now)
	probe := &stubQueuedTriggerProbe{queued: true}
	selector := NativeRepositorySelector{
		Config: config,
		Now:    func() time.Time { return now },
		DiscoverSelection: func(context.Context, RepoSyncConfig, string) (RepositorySelection, error) {
			return RepositorySelection{RepositoryIDs: repos}, nil
		},
		Logger:                discardLogger(),
		BaselineResolver:      resolver,
		SweepYieldProbe:       probe,
		SweepYieldMaxPerSweep: 1,
		SweepYieldCheckEvery:  2,
	}

	cycle := func() {
		if _, err := selector.SelectRepositories(context.Background()); err != nil {
			t.Fatalf("SelectRepositories() error = %v", err)
		}
	}

	cycle()
	if got := len(resolver.scopeIDs); got != 2 {
		t.Fatalf("cycle 1 visited %d repositories, want 2 (yielded)", got)
	}
	cycle()
	if got := len(resolver.scopeIDs); got != 5 {
		t.Fatalf("after cycle 2 visited %d repositories, want 5 (yield budget spent, tail completed)", got)
	}
	if got := distinctStrings(resolver.scopeIDs); got != 5 {
		t.Fatalf("sweep under storm visited %d distinct repositories, want all 5", got)
	}
	cycle()
	if got := len(resolver.scopeIDs); got != 7 {
		t.Fatalf("after cycle 3 visited %d repositories, want 7 (new sweep yielded again)", got)
	}
}

// TestNativeSelectorWithoutYieldProbeWalksEveryRepository is the control:
// without a probe the sweep is the pre-#8123 full walk.
func TestNativeSelectorWithoutYieldProbeWalksEveryRepository(t *testing.T) {
	reposDir, repos := sweepYieldWorld(t, 5)
	config := reconcileTestConfig(reposDir)
	now := time.Date(2026, 10, 14, 6, 37, 0, 0, time.UTC)
	resolver := freshBaselineResolver(now)
	selector := NativeRepositorySelector{
		Config: config,
		Now:    func() time.Time { return now },
		DiscoverSelection: func(context.Context, RepoSyncConfig, string) (RepositorySelection, error) {
			return RepositorySelection{RepositoryIDs: repos}, nil
		},
		Logger:           discardLogger(),
		BaselineResolver: resolver,
	}

	if _, err := selector.SelectRepositories(context.Background()); err != nil {
		t.Fatalf("SelectRepositories() error = %v", err)
	}
	if got := len(resolver.scopeIDs); got != 5 {
		t.Fatalf("sweep without probe visited %d repositories, want all 5 in one cycle", got)
	}
}

// queuedTriggerStore is a queue-semantics WebhookTriggerStore plus probe.
type queuedTriggerStore struct {
	queued    []webhook.StoredTrigger
	claimedAt []string
	handedOff []string
}

func (s *queuedTriggerStore) HasQueuedAcceptedTriggers(context.Context) (bool, error) {
	for _, trigger := range s.queued {
		if trigger.Decision == webhook.DecisionAccepted {
			return true, nil
		}
	}
	return false, nil
}

func (s *queuedTriggerStore) ClaimQueuedTriggers(_ context.Context, _ string, _ time.Time, _ int) ([]webhook.StoredTrigger, error) {
	claimed := append([]webhook.StoredTrigger(nil), s.queued...)
	s.queued = nil
	s.claimedAt = triggerIDsFromWebhookTriggers(claimed)
	return claimed, nil
}

func (s *queuedTriggerStore) MarkTriggersHandedOff(_ context.Context, triggers []webhook.StoredTrigger, _ time.Time) error {
	s.handedOff = triggerIDsFromWebhookTriggers(triggers)
	return nil
}

func (s *queuedTriggerStore) MarkTriggersFailed(context.Context, []webhook.StoredTrigger, time.Time, string, string) error {
	return nil
}

func (s *queuedTriggerStore) MarkIgnoredDecisionTriggersFailed(_ context.Context, triggers []webhook.StoredTrigger, _ time.Time, _ string, _ string) ([]webhook.StoredTrigger, error) {
	return triggers, nil
}

func (s *queuedTriggerStore) ReapExpiredTriggerClaims(context.Context, time.Time, int, int, time.Time) ([]webhook.StoredTrigger, []webhook.StoredTrigger, []webhook.StoredTrigger, error) {
	return nil, nil, nil, nil
}

func (s *queuedTriggerStore) CountStaleClaims(context.Context, time.Time) (int64, error) {
	return 0, nil
}

// midSweepTriggerProbe delivers its trigger on the first probe call.
type midSweepTriggerProbe struct {
	store     *queuedTriggerStore
	trigger   webhook.StoredTrigger
	delivered bool
}

func (p *midSweepTriggerProbe) HasQueuedAcceptedTriggers(ctx context.Context) (bool, error) {
	if !p.delivered {
		p.store.queued = append(p.store.queued, p.trigger)
		p.delivered = true
	}
	return p.store.HasQueuedAcceptedTriggers(ctx)
}

// TestPrioritySelectorClaimsTriggerQueuedMidSweepBeforeSweepEnds is the
// #8123 acceptance regression: a trigger that arrives while the native
// sweep walks is claimed at the next cycle, before the sweep tail is
// visited, and the sweep then resumes and still covers every repository.
func TestPrioritySelectorClaimsTriggerQueuedMidSweepBeforeSweepEnds(t *testing.T) {
	reposDir, repos := sweepYieldWorld(t, 5)
	config := reconcileTestConfig(reposDir)
	now := time.Date(2026, 10, 14, 6, 37, 53, 0, time.UTC)
	resolver := freshBaselineResolver(now)
	store := &queuedTriggerStore{}
	trigger := webhook.StoredTrigger{
		TriggerID:  "trigger-mid-sweep",
		ReceivedAt: now,
		Trigger: webhook.Trigger{
			Provider:             webhook.ProviderGitHub,
			Decision:             webhook.DecisionAccepted,
			RepositoryExternalID: "42",
			RepositoryFullName:   "org/hot",
			DefaultBranch:        "main",
			TargetSHA:            "2222222222222222222222222222222222222222",
		},
	}
	webhookSelector := WebhookTriggerRepositorySelector{
		Config: config,
		Store:  store,
		Owner:  "ingester",
		Now:    func() time.Time { return now },
		SyncGit: func(_ context.Context, _ RepoSyncConfig, repositoryIDs []string) (GitSyncSelection, error) {
			if len(repositoryIDs) != 1 || repositoryIDs[0] != "org/hot" {
				t.Fatalf("webhook sync repositoryIDs = %v, want [org/hot]", repositoryIDs)
			}
			return GitSyncSelection{SelectedRepoPaths: []string{t.TempDir()}}, nil
		},
		Logger: discardLogger(),
	}
	nativeSelector := &NativeRepositorySelector{
		Config: config,
		Now:    func() time.Time { return now },
		DiscoverSelection: func(context.Context, RepoSyncConfig, string) (RepositorySelection, error) {
			return RepositorySelection{RepositoryIDs: repos}, nil
		},
		Logger:               discardLogger(),
		BaselineResolver:     resolver,
		SweepYieldProbe:      &midSweepTriggerProbe{store: store, trigger: trigger},
		SweepYieldCheckEvery: 2,
	}
	selector := PriorityRepositorySelector{Selectors: []RepositorySelector{webhookSelector, nativeSelector}}

	// Cycle 1: queue empty at start; the trigger lands mid-walk.
	batch, err := selector.SelectRepositories(context.Background())
	if err != nil {
		t.Fatalf("cycle 1 SelectRepositories() error = %v", err)
	}
	if len(batch.Repositories) != 0 {
		t.Fatalf("cycle 1 selected %d repositories, want 0 (unchanged heads)", len(batch.Repositories))
	}
	if got := len(resolver.scopeIDs); got != 2 {
		t.Fatalf("cycle 1 visited %d of 5 repositories, want 2 (sweep yielded mid-walk)", got)
	}
	if len(store.claimedAt) != 0 {
		t.Fatalf("cycle 1 claimed %v, want no claim before the yield", store.claimedAt)
	}

	// Cycle 2: claimed while three sweep repositories are unvisited.
	batch, err = selector.SelectRepositories(context.Background())
	if err != nil {
		t.Fatalf("cycle 2 SelectRepositories() error = %v", err)
	}
	if len(batch.Repositories) != 1 {
		t.Fatalf("cycle 2 selected %d repositories, want the 1 triggered repository", len(batch.Repositories))
	}
	if len(store.handedOff) != 1 || store.handedOff[0] != "trigger-mid-sweep" {
		t.Fatalf("handed off = %v, want [trigger-mid-sweep]", store.handedOff)
	}
	if got := len(resolver.scopeIDs); got != 2 {
		t.Fatalf("at claim time the sweep had visited %d of 5 repositories, want 2 (claim before sweep end)", got)
	}

	// Cycle 3: the sweep resumes and covers the tail.
	if _, err := selector.SelectRepositories(context.Background()); err != nil {
		t.Fatalf("cycle 3 SelectRepositories() error = %v", err)
	}
	if got := len(resolver.scopeIDs); got != 5 {
		t.Fatalf("after resume the sweep visited %d repositories, want exactly 5", got)
	}
	if got := distinctStrings(resolver.scopeIDs); got != 5 {
		t.Fatalf("after resume the sweep visited %d distinct repositories, want all 5", got)
	}
}

// claimWaitHistogram returns the (count, sum) of the claim-wait
// histogram's single datapoint, failing on any other shape.
func claimWaitHistogram(t *testing.T, reader *sdkmetric.ManualReader) (uint64, float64) {
	t.Helper()
	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	for _, scopeMetrics := range metrics.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			if m.Name != "eshu_dp_webhook_trigger_claim_wait_seconds" {
				continue
			}
			histogram, ok := m.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("claim wait data = %T, want Histogram[float64]", m.Data)
			}
			if len(histogram.DataPoints) != 1 {
				t.Fatalf("claim wait datapoints = %d, want 1 (no labels)", len(histogram.DataPoints))
			}
			point := histogram.DataPoints[0]
			if point.Attributes.Len() != 0 {
				t.Fatalf("claim wait attributes = %v, want none", point.Attributes.ToSlice())
			}
			return point.Count, point.Sum
		}
	}
	t.Fatal("claim wait histogram not recorded")
	return 0, 0
}

// TestWebhookSelectorRecordsClaimWaitForAcceptedTriggersOnly pins the
// #8123 wait metric: the accepted row records claimed_at - received_at
// (157.2s, the traced wait from the issue), and the non-accepted row
// that owes no sync records nothing.
func TestWebhookSelectorRecordsClaimWaitForAcceptedTriggersOnly(t *testing.T) {
	now := time.Date(2026, 10, 14, 6, 40, 30, 0, time.UTC)
	instruments, reader := newCollisionTestInstruments(t)
	store := &stubWebhookTriggerStore{
		claimed: []webhook.StoredTrigger{
			{
				TriggerID:  "trigger-accepted",
				ReceivedAt: now.Add(-157200 * time.Millisecond),
				Trigger: webhook.Trigger{
					Provider:             webhook.ProviderGitHub,
					Decision:             webhook.DecisionAccepted,
					RepositoryExternalID: "42",
					RepositoryFullName:   "org/hot",
					DefaultBranch:        "main",
					TargetSHA:            "2222222222222222222222222222222222222222",
				},
			},
			{
				TriggerID:  "trigger-ignored",
				ReceivedAt: now.Add(-2 * time.Hour),
				Trigger: webhook.Trigger{
					Provider:             webhook.ProviderGitHub,
					Decision:             webhook.DecisionIgnored,
					RepositoryExternalID: "42",
					RepositoryFullName:   "org/quiet",
					DefaultBranch:        "main",
					TargetSHA:            "3333333333333333333333333333333333333333",
				},
			},
		},
	}
	selector := WebhookTriggerRepositorySelector{
		Config:      RepoSyncConfig{ReposDir: t.TempDir(), SourceMode: "explicit", CloneDepth: 1},
		Store:       store,
		Owner:       "ingester",
		Now:         func() time.Time { return now },
		Instruments: instruments,
		Logger:      discardLogger(),
		SyncGit: func(context.Context, RepoSyncConfig, []string) (GitSyncSelection, error) {
			return GitSyncSelection{}, nil
		},
	}

	if _, err := selector.SelectRepositories(context.Background()); err != nil {
		t.Fatalf("SelectRepositories() error = %v", err)
	}
	count, sum := claimWaitHistogram(t, reader)
	if count != 1 {
		t.Fatalf("claim wait count = %d, want 1 (accepted only)", count)
	}
	if math.Abs(sum-157.2) > 0.001 {
		t.Fatalf("claim wait sum = %v, want 157.2", sum)
	}
}

// TestLoadWebhookTriggerHandoffConfigSweepYieldSettings pins the named
// settings that bound sweep yields (#8123): valid values parse, invalid
// values keep the zero that takes the selector defaults.
func TestLoadWebhookTriggerHandoffConfigSweepYieldSettings(t *testing.T) {
	t.Parallel()

	config := LoadWebhookTriggerHandoffConfig("ingester", func(key string) string {
		return map[string]string{
			"ESHU_WEBHOOK_TRIGGER_HANDOFF_ENABLED":         "true",
			"ESHU_WEBHOOK_TRIGGER_SWEEP_MAX_YIELDS":        "4",
			"ESHU_WEBHOOK_TRIGGER_SWEEP_YIELD_CHECK_REPOS": "32",
		}[key]
	})
	if config.SweepMaxYields != 4 || config.SweepYieldCheckEvery != 32 {
		t.Fatalf("sweep yield settings = (max %d, check %d), want (4, 32)",
			config.SweepMaxYields, config.SweepYieldCheckEvery)
	}

	fallback := LoadWebhookTriggerHandoffConfig("ingester", func(key string) string {
		return map[string]string{
			"ESHU_WEBHOOK_TRIGGER_SWEEP_MAX_YIELDS":        "-1",
			"ESHU_WEBHOOK_TRIGGER_SWEEP_YIELD_CHECK_REPOS": "many",
		}[key]
	})
	if fallback.SweepMaxYields != 0 || fallback.SweepYieldCheckEvery != 0 {
		t.Fatalf("invalid sweep yield settings = (max %d, check %d), want (0, 0) selector defaults",
			fallback.SweepMaxYields, fallback.SweepYieldCheckEvery)
	}
}
