// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"bytes"
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// runWatermarkCycle runs one selection cycle at now with the given policy and
// active reindex watermark, records a pending generation when the sync
// produced one, and reports whether the cycle forced a reconciliation.
func runWatermarkCycle(
	t *testing.T,
	world *reconcileWorld,
	reposDir string,
	now time.Time,
	policy reconcilePolicy,
	watermark time.Time,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) (bool, GitSyncSelection) {
	t.Helper()
	config := reconcileTestConfig(reposDir)
	synced, err := syncGitRepositoriesWithLogger(
		context.Background(), config, []string{"github/org/big"}, logger,
		gitDeltaBaseline{
			Resolver:           world,
			Instruments:        instruments,
			Reconcile:          policy,
			ReindexRequestedAt: watermark,
			Now:                func() time.Time { return now },
		},
	)
	if err != nil {
		t.Fatalf("syncGitRepositoriesWithLogger() error = %v", err)
	}
	repoPath := filepath.Join(reposDir, "github", "org", "big")
	forced := synced.ReconcileByRepoPath[repoPath]
	if len(synced.SelectedRepoPaths) > 0 {
		_, isDelta := synced.DeltaByRepoPath[repoPath]
		world.generations = append(world.generations, reconcileWorldGeneration{
			sha: "samesha", isDelta: isDelta && !forced, status: "pending", ingestedAt: now,
		})
	}
	return forced, synced
}

// TestReindexWatermarkForcesUnchangedRepoWithFreshFull is the #7620
// regression: a reindex was requested after the scope's only activated full,
// the full is still fresh for the sweep, and the remote head equals the active
// commit. Today the unchanged repository is skipped and the reindex request is
// never read. The watermark must force a full re-parse with reason
// reindex_requested: the selection carries Reconcile and no delta, so the
// snapshot blanks the freshness hint and is not skipped as unchanged.
func TestReindexWatermarkForcesUnchangedRepoWithFreshFull(t *testing.T) {
	start := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	world, reposDir := newReconcileWorld(t, start)
	world.generations[0].ingestedAt = start.Add(-time.Hour)
	watermark := start.Add(-30 * time.Minute)
	instruments, reader := newCollisionTestInstruments(t)
	var logs bytes.Buffer

	forced, synced := runWatermarkCycle(t, world, reposDir, start,
		reconcilePolicy{Interval: 24 * time.Hour, MaxPerCycle: 10}, watermark, instruments,
		slog.New(slog.NewJSONHandler(&logs, nil)))

	repoPath := filepath.Join(reposDir, "github", "org", "big")
	if !forced || len(synced.SelectedRepoPaths) != 1 {
		t.Fatalf("forced = %t, selected = %v; want a forced full re-parse of %s", forced, synced.SelectedRepoPaths, repoPath)
	}
	if _, ok := synced.DeltaByRepoPath[repoPath]; ok {
		t.Fatalf("delta = %#v, want none: a reindex must re-parse the full snapshot", synced.DeltaByRepoPath[repoPath])
	}
	if got := reconcileCounterByReason(t, reader, "eshu_dp_collector_reconciliation_full_snapshots_total"); got[reconcileReasonReindexRequested] != 1 || len(got) != 1 {
		t.Fatalf("reconciliation counter = %v, want only reindex_requested=1", got)
	}
	if got := fallbackCountsByReason(t, reader); len(got) != 0 {
		t.Fatalf("delta-baseline fallback counter = %v, want nothing", got)
	}
	for _, want := range []string{
		`"msg":"git_reconcile_forced"`,
		`"reason":"reindex_requested"`,
		`"reindex_requested_at":"2026-10-05T11:30:00Z"`,
	} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("logs lack %s:\n%s", want, logs.String())
		}
	}
}

// TestReindexWatermarkClearsAfterActivation: once the forced full activates,
// its ingest time is at or after the watermark, so later cycles leave the
// scope alone even though the request row stays pending.
func TestReindexWatermarkClearsAfterActivation(t *testing.T) {
	start := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	world, reposDir := newReconcileWorld(t, start)
	world.generations[0].ingestedAt = start.Add(-time.Hour)
	watermark := start.Add(-30 * time.Minute)
	policy := reconcilePolicy{Interval: 24 * time.Hour, MaxPerCycle: 10}

	if forced, _ := runWatermarkCycle(t, world, reposDir, start, policy, watermark, nil, discardLogger()); !forced {
		t.Fatal("cycle 0 did not force the reindex")
	}
	forcedFull := world.lastFull()
	world.generations[0].status = "superseded"
	world.generations[forcedFull].status = "active"
	world.generations[forcedFull].activated = true

	for cycle := 1; cycle <= 5; cycle++ {
		now := start.Add(time.Duration(cycle) * 6 * time.Minute)
		if forced, _ := runWatermarkCycle(t, world, reposDir, now, policy, watermark, nil, discardLogger()); forced {
			t.Fatalf("cycle %d re-forced a scope whose activated full is newer than the watermark", cycle)
		}
	}
}

// TestReindexWatermarkHoldsWhileForcedFullPending: the forced full is still
// pending, so later cycles must not stack another full on top of it.
func TestReindexWatermarkHoldsWhileForcedFullPending(t *testing.T) {
	start := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	world, reposDir := newReconcileWorld(t, start)
	world.generations[0].ingestedAt = start.Add(-time.Hour)
	watermark := start.Add(-30 * time.Minute)
	policy := reconcilePolicy{Interval: 24 * time.Hour, MaxPerCycle: 10}

	forcedCount := 0
	for cycle := 0; cycle < 5; cycle++ {
		now := start.Add(time.Duration(cycle) * 6 * time.Minute)
		if forced, _ := runWatermarkCycle(t, world, reposDir, now, policy, watermark, nil, discardLogger()); forced {
			forcedCount++
		}
	}
	if forcedCount != 1 {
		t.Fatalf("forced reindexes across 5 cycles = %d, want exactly 1 while the first stays pending", forcedCount)
	}
}

// reindexState builds a full reconcile state with an activated full ingested
// at projectedAt and, when latestStatus is not empty, a newer unactivated full
// attempt ingested at latestAt.
func reindexState(projectedAt time.Time, latestStatus scope.GenerationStatus, latestAt time.Time) scope.FullReconcileState {
	state := scope.FullReconcileState{
		HasProjectedFull: true, LastProjectedFullAt: projectedAt,
		HasLatestFull: true, LatestFullAt: projectedAt,
		LatestFullStatus: scope.GenerationStatusActive, LatestFullProjected: true,
	}
	if latestStatus != "" {
		state.LatestFullAt = latestAt
		state.LatestFullStatus = latestStatus
		state.LatestFullProjected = false
	}
	return state
}

// TestReindexWatermarkDecisionTable pins the #7620 decision order in
// decideForScope: the sweep's non-fresh reasons win, graph_dirty keeps
// priority over a fresh scope, and only a scope the sweep calls fresh is then
// checked against the watermark, behind the same throttle. With Interval 0 the
// sweep and graph_dirty stay off, and the watermark alone is honored with the
// default 24h throttle bounds.
func TestReindexWatermarkDecisionTable(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	watermark := now.Add(-2 * time.Hour)
	daily := reconcilePolicy{Interval: 24 * time.Hour, MaxPerCycle: 10}
	off := reconcilePolicy{MaxPerCycle: 10}
	cases := []struct {
		name      string
		policy    reconcilePolicy
		watermark time.Time
		state     scope.FullReconcileState
		writers   []scope.UncoveredProjectionWriter
		wantDue   bool
		want      string
	}{
		{
			"full older than watermark is forced", daily, watermark,
			reindexState(now.Add(-3*time.Hour), "", time.Time{}), nil, true, reconcileReasonReindexRequested,
		},
		{
			"full newer than watermark stays fresh", daily, watermark,
			reindexState(now.Add(-time.Hour), "", time.Time{}), nil, false, reconcileReasonFresh,
		},
		{
			"full exactly at watermark stays fresh", daily, watermark,
			reindexState(watermark, "", time.Time{}), nil, false, reconcileReasonFresh,
		},
		{
			"in-flight full holds the reindex", daily, watermark,
			reindexState(now.Add(-3*time.Hour), scope.GenerationStatusPending, now.Add(-time.Hour)), nil, false, reconcileReasonInFlight,
		},
		{
			"recently failed full holds the reindex", daily, watermark,
			reindexState(now.Add(-3*time.Hour), scope.GenerationStatusFailed, now.Add(-time.Hour)), nil, false, reconcileReasonRetryBackoff,
		},
		{
			"overdue sweep reason wins", daily, watermark,
			reindexState(now.Add(-25*time.Hour), "", time.Time{}), nil, true, reconcileReasonIntervalElapsed,
		},
		{
			"graph_dirty keeps priority", daily, watermark,
			reindexState(now.Add(-3*time.Hour), "", time.Time{}),
			[]scope.UncoveredProjectionWriter{dirtyWriter()},
			true, reconcileReasonGraphDirty,
		},
		{
			"interval 0 honors the watermark", off, watermark,
			reindexState(now.Add(-3*time.Hour), "", time.Time{}), nil, true, reconcileReasonReindexRequested,
		},
		{
			"interval 0 never-reconciled scope is forced", off, watermark,
			scope.FullReconcileState{},
			nil, true, reconcileReasonReindexRequested,
		},
		{
			"interval 0 uses the 24h retry backoff", off, watermark,
			reindexState(now.Add(-8*time.Hour), scope.GenerationStatusFailed, now.Add(-5*time.Hour)), nil, false, reconcileReasonRetryBackoff,
		},
		{
			"interval 0 retries after the 24h backoff", off, watermark,
			reindexState(now.Add(-8*time.Hour), scope.GenerationStatusFailed, now.Add(-7*time.Hour)), nil, true, reconcileReasonReindexRequested,
		},
		{
			"interval 0 ignores graph_dirty and the interval", off, watermark,
			reindexState(now.Add(-time.Hour), "", time.Time{}),
			[]scope.UncoveredProjectionWriter{dirtyWriter()},
			false, reconcileReasonFresh,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			baseline := gitDeltaBaseline{
				Resolver:           &stubBaselineResolver{state: tc.state, writers: tc.writers},
				Reconcile:          tc.policy,
				ReindexRequestedAt: tc.watermark,
				Now:                func() time.Time { return now },
			}
			decision := baseline.decideForScope(context.Background(), "scope-1", discardLogger())
			if decision.Due != tc.wantDue || decision.Reason != tc.want {
				t.Fatalf("decision = (%t, %q), want (%t, %q)", decision.Due, decision.Reason, tc.wantDue, tc.want)
			}
		})
	}
}

// TestReindexWatermarkUnsetKeepsTodaysDecisions: with no active watermark,
// every decision equals the sweep's own decision, and Interval 0 still
// disables reconciliation without reading scope state.
func TestReindexWatermarkUnsetKeepsTodaysDecisions(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	daily := reconcilePolicy{Interval: 24 * time.Hour, MaxPerCycle: 10}
	for _, state := range []scope.FullReconcileState{
		{},
		reindexState(now.Add(-time.Hour), "", time.Time{}),
		reindexState(now.Add(-25*time.Hour), "", time.Time{}),
		reindexState(now.Add(-25*time.Hour), scope.GenerationStatusPending, now.Add(-time.Hour)),
		reindexState(now.Add(-25*time.Hour), scope.GenerationStatusFailed, now.Add(-time.Hour)),
		reindexState(now.Add(-25*time.Hour), scope.GenerationStatusFailed, now.Add(-7*time.Hour)),
	} {
		baseline := gitDeltaBaseline{
			Resolver:  &stubBaselineResolver{state: state},
			Reconcile: daily,
			Now:       func() time.Time { return now },
		}
		decision := baseline.decideForScope(context.Background(), "scope-1", discardLogger())
		wantDue, want := daily.decide(now, state)
		if decision.Due != wantDue || decision.Reason != want {
			t.Fatalf("state %+v: decision = (%t, %q), want sweep decision (%t, %q)", state, decision.Due, decision.Reason, wantDue, want)
		}
	}

	resolver := &stubBaselineResolver{state: reindexState(now.Add(-25*time.Hour), "", time.Time{})}
	baseline := gitDeltaBaseline{Resolver: resolver, Reconcile: reconcilePolicy{}, Now: func() time.Time { return now }}
	reposDir := t.TempDir()
	decision := baseline.reconcileDue(context.Background(), reconcileTestConfig(reposDir),
		filepath.Join(reposDir, "github", "org", "repo"), discardLogger())
	if decision.Due || len(resolver.fullScopeIDs) != 0 {
		t.Fatalf("interval 0 without a watermark: due = %t, state reads = %d; want not due and no reads", decision.Due, len(resolver.fullScopeIDs))
	}
}

// TestReindexWatermarkSharesTheReconcileBudget: reindex-forced scopes count
// against ESHU_REPO_RECONCILE_MAX_PER_CYCLE like every other reconcile reason.
func TestReindexWatermarkSharesTheReconcileBudget(t *testing.T) {
	reposDir := t.TempDir()
	repos := []string{"github/org/one", "github/org/two", "github/org/three"}
	writeFreshScopesFakeGit(t, reposDir, repos)
	config := reconcileTestConfig(reposDir)
	config.ReconcileMaxPerCycle = 2
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	resolver := &dirtyScopes{now: now, dirty: map[string]bool{}}

	synced, err := syncGitRepositoriesWithLogger(context.Background(), config, repos, discardLogger(),
		gitDeltaBaseline{
			Resolver:           resolver,
			Reconcile:          reconcilePolicyFromConfig(config),
			ReindexRequestedAt: now.Add(-30 * time.Minute),
			Now:                func() time.Time { return now },
		})
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if got := len(synced.ReconcileByRepoPath); got != 2 {
		t.Fatalf("reindex-forced scopes = %d (%v), want the budget of 2", got, synced.ReconcileByRepoPath)
	}
}
