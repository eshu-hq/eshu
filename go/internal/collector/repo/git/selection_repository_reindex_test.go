// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/webhook"
)

// fakeRepositoryReindex answers RepositoryReindexWatermarks like the store:
// it returns the rows newer than after, and records each read.
type fakeRepositoryReindex struct {
	rows   map[string]time.Time
	err    error
	afters []time.Time
}

func (f *fakeRepositoryReindex) RepositoryReindexWatermarks(_ context.Context, after time.Time) (map[string]time.Time, error) {
	f.afters = append(f.afters, after)
	if f.err != nil {
		return nil, f.err
	}
	newer := make(map[string]time.Time)
	for scopeID, requestedAt := range f.rows {
		if requestedAt.After(after) {
			newer[scopeID] = requestedAt
		}
	}
	return newer, nil
}

// managedScopeID is the git scope ID the collector derives for repo under
// reposDir.
func managedScopeID(t *testing.T, reposDir, repo string) string {
	t.Helper()
	scopeID := gitScopeIDForManagedRepo(reconcileTestConfig(reposDir), filepath.Join(reposDir, filepath.FromSlash(repo)))
	if scopeID == "" {
		t.Fatalf("no scope id for %s", repo)
	}
	return scopeID
}

// TestResolveRepositoryReindexWatermarks pins the per-cycle read: it asks for
// rows newer than the fleet watermark, keeps rows at or before observedAt in
// UTC, defers later rows one by one, and ignores a read failure for the cycle.
func TestResolveRepositoryReindexWatermarks(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	fleet := observedAt.Add(-time.Hour)
	config := RepoSyncConfig{RepoShardCount: 2, RepoShardIndex: 1}
	edt := time.FixedZone("EDT", -4*3600)

	var logs bytes.Buffer
	reader := &fakeRepositoryReindex{rows: map[string]time.Time{
		"scope:active":   observedAt.Add(-time.Minute).In(edt),
		"scope:equal":    observedAt,
		"scope:deferred": observedAt.Add(time.Second),
		"scope:old":      fleet.Add(-time.Minute),
	}}
	got := resolveRepositoryReindexWatermarks(context.Background(), reader, fleet, observedAt, config,
		slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))

	want := map[string]time.Time{"scope:active": observedAt.Add(-time.Minute), "scope:equal": observedAt}
	if len(got) != len(want) {
		t.Fatalf("active watermarks = %v, want %v", got, want)
	}
	for scopeID, at := range want {
		if !got[scopeID].Equal(at) || got[scopeID].Location() != time.UTC {
			t.Fatalf("watermark %s = %v, want %v in UTC", scopeID, got[scopeID], at)
		}
	}
	if len(reader.afters) != 1 || !reader.afters[0].Equal(fleet) {
		t.Fatalf("reads = %v, want one read after the fleet watermark %v", reader.afters, fleet)
	}
	for _, want := range []string{
		`"msg":"git_repository_reindex_deferred"`, `"deferred_count":1`,
		`"msg":"git_repository_reindex_active"`, `"active_count":2`, `"repo_shard_index":1`,
	} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("logs lack %s:\n%s", want, logs.String())
		}
	}

	logs.Reset()
	failed := resolveRepositoryReindexWatermarks(context.Background(), &fakeRepositoryReindex{err: errors.New("postgres down")},
		time.Time{}, observedAt, config, slog.New(slog.NewJSONHandler(&logs, nil)))
	if len(failed) != 0 {
		t.Fatalf("watermarks after a read failure = %v, want none", failed)
	}
	for _, want := range []string{`"level":"WARN"`, `"msg":"git_repository_reindex_read_failed"`, `postgres down`} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("logs lack %s:\n%s", want, logs.String())
		}
	}
	if none := resolveRepositoryReindexWatermarks(context.Background(), nil, fleet, observedAt, config, discardLogger()); none != nil {
		t.Fatalf("nil reader watermarks = %v, want nil", none)
	}
}

// TestRepositoryReindexDecisionTable pins how a per-repository watermark
// combines with the fleet watermark in decideForScope: the later one applies,
// the repository reason is used only when the repository's own watermark is
// the later one, and the fresh rule and throttle are unchanged.
func TestRepositoryReindexDecisionTable(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	fleet := now.Add(-3 * time.Hour)
	daily := reconcilePolicy{Interval: 24 * time.Hour, MaxPerCycle: 10}
	off := reconcilePolicy{MaxPerCycle: 10}
	cases := []struct {
		name          string
		policy        reconcilePolicy
		fleet         time.Time
		repository    time.Time
		state         scope.FullReconcileState
		wantDue       bool
		want          string
		wantWatermark time.Time
	}{
		{
			"repository watermark later than the full forces", daily, fleet, now.Add(-time.Hour),
			reindexState(now.Add(-2*time.Hour), "", time.Time{}), true, reconcileReasonRepositoryReindexRequested, now.Add(-time.Hour),
		},
		{
			"repository watermark without a fleet watermark forces", daily,
			time.Time{},
			now.Add(-time.Hour),
			reindexState(now.Add(-2*time.Hour), "", time.Time{}), true, reconcileReasonRepositoryReindexRequested, now.Add(-time.Hour),
		},
		{
			"full newer than the repository watermark stays fresh", daily, fleet, now.Add(-time.Hour),
			reindexState(now.Add(-30*time.Minute), "", time.Time{}), false, reconcileReasonFresh, now.Add(-time.Hour),
		},
		{
			"repository watermark at the fleet watermark keeps the fleet reason", daily, fleet, fleet,
			reindexState(now.Add(-4*time.Hour), "", time.Time{}), true, reconcileReasonReindexRequested, fleet,
		},
		{
			"in-flight full holds a repository reindex", daily, fleet, now.Add(-time.Hour),
			reindexState(now.Add(-4*time.Hour), scope.GenerationStatusPending, now.Add(-2*time.Hour)), false, reconcileReasonInFlight, now.Add(-time.Hour),
		},
		{
			"interval 0 honors a repository watermark", off,
			time.Time{},
			now.Add(-time.Hour),
			reindexState(now.Add(-2*time.Hour), "", time.Time{}), true, reconcileReasonRepositoryReindexRequested, now.Add(-time.Hour),
		},
		{
			"no watermark for the scope stays fresh", daily,
			time.Time{},
			time.Time{},
			reindexState(now.Add(-2*time.Hour), "", time.Time{}), false, reconcileReasonFresh,
			time.Time{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			repositories := map[string]time.Time{"other-scope": now.Add(-time.Minute)}
			if !tc.repository.IsZero() {
				repositories["scope-1"] = tc.repository
			}
			baseline := gitDeltaBaseline{
				Resolver:                     &stubBaselineResolver{state: tc.state},
				Reconcile:                    tc.policy,
				ReindexRequestedAt:           tc.fleet,
				RepositoryReindexRequestedAt: repositories,
				Now:                          func() time.Time { return now },
			}
			decision := baseline.decideForScope(context.Background(), "scope-1", discardLogger())
			if decision.Due != tc.wantDue || decision.Reason != tc.want {
				t.Fatalf("decision = (%t, %q), want (%t, %q)", decision.Due, decision.Reason, tc.wantDue, tc.want)
			}
			if !decision.ReindexRequestedAt.Equal(tc.wantWatermark) {
				t.Fatalf("decision watermark = %v, want %v", decision.ReindexRequestedAt, tc.wantWatermark)
			}
		})
	}
}

// TestRepositoryReindexSweepOffReadsOnlyRequestedScopes: with the sweep off
// and no fleet watermark, a scope without a repository watermark is skipped
// without reading its state, and a requested scope is read and forced.
func TestRepositoryReindexSweepOffReadsOnlyRequestedScopes(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	reposDir := t.TempDir()
	config := reconcileTestConfig(reposDir)
	requested := managedScopeID(t, reposDir, "github/org/requested")
	resolver := &stubBaselineResolver{state: reindexState(now.Add(-2*time.Hour), "", time.Time{})}
	baseline := gitDeltaBaseline{
		Resolver:                     resolver,
		Reconcile:                    reconcilePolicy{},
		RepositoryReindexRequestedAt: map[string]time.Time{requested: now.Add(-time.Hour)},
		Now:                          func() time.Time { return now },
	}

	skipped := baseline.reconcileDue(context.Background(), config, filepath.Join(reposDir, "github", "org", "other"), discardLogger())
	if skipped.Due || len(resolver.fullScopeIDs) != 0 {
		t.Fatalf("unrequested scope: due = %t, state reads = %v; want not due and no reads", skipped.Due, resolver.fullScopeIDs)
	}
	forced := baseline.reconcileDue(context.Background(), config, filepath.Join(reposDir, "github", "org", "requested"), discardLogger())
	if !forced.Due || forced.Reason != reconcileReasonRepositoryReindexRequested {
		t.Fatalf("requested scope decision = (%t, %q), want (true, %s)", forced.Due, forced.Reason, reconcileReasonRepositoryReindexRequested)
	}
	if len(resolver.fullScopeIDs) != 1 || resolver.fullScopeIDs[0] != requested {
		t.Fatalf("state reads = %v, want only %s", resolver.fullScopeIDs, requested)
	}
}

// TestRepositoryReindexTargetedScopeIsNotStarved: during a fleet reindex the
// per-cycle budget fills in iteration order, so a repository an operator asked
// for by name would wait its turn behind the whole fleet. Requested
// repositories are synced first; the cycle still forces exactly the budget.
func TestRepositoryReindexTargetedScopeIsNotStarved(t *testing.T) {
	reposDir := t.TempDir()
	repos := make([]string, 0, 30)
	for i := range 30 {
		repos = append(repos, fmt.Sprintf("github/org/repo-%02d", i))
	}
	writeFreshScopesFakeGit(t, reposDir, repos)
	config := reconcileTestConfig(reposDir)
	config.ReconcileMaxPerCycle = 10
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	last := repos[len(repos)-1]
	instruments, reader := newCollisionTestInstruments(t)

	synced, err := syncGitRepositoriesWithLogger(context.Background(), config, repos, discardLogger(),
		gitDeltaBaseline{
			Resolver:                     &dirtyScopes{now: now, dirty: map[string]bool{}},
			Instruments:                  instruments,
			Reconcile:                    reconcilePolicyFromConfig(config),
			ReindexRequestedAt:           now.Add(-50 * time.Minute),
			RepositoryReindexRequestedAt: map[string]time.Time{managedScopeID(t, reposDir, last): now.Add(-10 * time.Minute)},
			Now:                          func() time.Time { return now },
		})
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if got := len(synced.ReconcileByRepoPath); got != 10 {
		t.Fatalf("forced scopes = %d, want the budget of 10", got)
	}
	if !synced.ReconcileByRepoPath[filepath.Join(reposDir, filepath.FromSlash(last))] {
		t.Fatalf("requested repository %s was not forced in the first cycle: %v", last, synced.ReconcileByRepoPath)
	}
	got := reconcileCounterByReason(t, reader, "eshu_dp_collector_reconciliation_full_snapshots_total")
	if got[reconcileReasonRepositoryReindexRequested] != 1 || got[reconcileReasonReindexRequested] != 9 {
		t.Fatalf("reconciliation counter = %v, want repository_reindex_requested=1 and reindex_requested=9", got)
	}
	if want := sortUniqueStrings(synced.SelectedRepoPaths); strings.Join(want, ",") != strings.Join(synced.SelectedRepoPaths, ",") {
		t.Fatalf("selected paths %v are not sorted", synced.SelectedRepoPaths)
	}
}

// TestRepositoryReindexReachesBothSelectors: the native and webhook selectors
// each read the repository watermarks once per cycle, after the fleet
// watermark, and force only the requested repository.
func TestRepositoryReindexReachesBothSelectors(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	fleet := now.Add(-5 * time.Hour)

	t.Run("native", func(t *testing.T) {
		reposDir := t.TempDir()
		repos := []string{"github/org/a", "github/org/b", "github/org/c"}
		writeFreshScopesFakeGit(t, reposDir, repos)
		reader := &fakeRepositoryReindex{rows: map[string]time.Time{managedScopeID(t, reposDir, "github/org/b"): now.Add(-10 * time.Minute)}}
		selector := NativeRepositorySelector{
			Config: reconcileTestConfig(reposDir),
			Now:    func() time.Time { return now },
			DiscoverSelection: func(context.Context, RepoSyncConfig, string) (RepositorySelection, error) {
				return RepositorySelection{RepositoryIDs: repos}, nil
			},
			Logger:                     discardLogger(),
			BaselineResolver:           &dirtyScopes{now: now, dirty: map[string]bool{}},
			ReindexWatermark:           &fakeReindexWatermark{at: fleet},
			RepositoryReindexWatermark: reader,
		}
		batch, err := selector.SelectRepositories(context.Background())
		if err != nil {
			t.Fatalf("SelectRepositories() error = %v", err)
		}
		assertOnlyRequestedForced(t, batch, filepath.Join(reposDir, "github", "org", "b"))
		if len(reader.afters) != 1 || !reader.afters[0].Equal(fleet) {
			t.Fatalf("repository watermark reads = %v, want one read after %v", reader.afters, fleet)
		}
	})

	t.Run("webhook", func(t *testing.T) {
		reposDir := t.TempDir()
		writeFreshScopesFakeGit(t, reposDir, []string{"eshu-hq/eshu"})
		store := &stubWebhookTriggerStore{claimed: []webhook.StoredTrigger{{
			TriggerID: "trigger-1",
			Trigger: webhook.Trigger{
				Provider: webhook.ProviderGitHub, Decision: webhook.DecisionAccepted,
				RepositoryExternalID: "42", RepositoryFullName: "eshu-hq/eshu", DefaultBranch: "main",
			},
		}}}
		reader := &fakeRepositoryReindex{rows: map[string]time.Time{managedScopeID(t, reposDir, "eshu-hq/eshu"): now.Add(-10 * time.Minute)}}
		selector := WebhookTriggerRepositorySelector{
			Config:                     reconcileTestConfig(reposDir),
			Store:                      store,
			Owner:                      "collector-git",
			Now:                        func() time.Time { return now },
			Logger:                     discardLogger(),
			BaselineResolver:           &dirtyScopes{now: now, dirty: map[string]bool{}},
			RepositoryReindexWatermark: reader,
		}
		batch, err := selector.SelectRepositories(context.Background())
		if err != nil {
			t.Fatalf("SelectRepositories() error = %v", err)
		}
		assertOnlyRequestedForced(t, batch, filepath.Join(reposDir, "eshu-hq", "eshu"))
		if len(reader.afters) != 1 || !reader.afters[0].IsZero() {
			t.Fatalf("repository watermark reads = %v, want one read of every row", reader.afters)
		}
	})
}

func assertOnlyRequestedForced(t *testing.T, batch SelectionBatch, requestedPath string) {
	t.Helper()
	forced := 0
	for _, repository := range batch.Repositories {
		if repository.Reconcile {
			forced++
			if repository.RepoPath != requestedPath {
				t.Fatalf("forced %s, want only %s", repository.RepoPath, requestedPath)
			}
		}
	}
	if forced != 1 {
		t.Fatalf("forced %d repositories (%+v), want only %s", forced, batch.Repositories, requestedPath)
	}
}

// TestReconcileSweepDecisionHonorsRepositoryReindex: the exported decision the
// end-to-end proofs use runs the same per-repository rule.
func TestReconcileSweepDecisionHonorsRepositoryReindex(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	resolver := &stubBaselineResolver{state: reindexState(now.Add(-3*time.Hour), "", time.Time{})}
	due, reason := ReconcileSweepDecision(context.Background(), resolver, 0, time.Time{}, now.Add(-time.Hour), now, "scope-1", discardLogger())
	if !due || reason != reconcileReasonRepositoryReindexRequested {
		t.Fatalf("ReconcileSweepDecision() = (%t, %q), want (true, %s)", due, reason, reconcileReasonRepositoryReindexRequested)
	}
}
