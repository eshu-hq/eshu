// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

// TestWedgedPendingFullHoldsWithoutParsing is the ruling a9-126 performance
// proof: a full generation pending longer than Interval, at the remote head,
// with live projector work, used to be forced (and parsed) every cycle and then
// skipped by the store. Now three cycles select nothing (no snapshot, no parse).
// When its work is gone, the same scope is forced once.
func TestWedgedPendingFullHoldsWithoutParsing(t *testing.T) {
	start := time.Date(2026, 10, 11, 1, 0, 0, 0, time.UTC)
	world, reposDir := newReconcileWorld(t, start)
	world.generations[0].ingestedAt = start.Add(-72 * time.Hour)
	world.generations = append(world.generations, reconcileWorldGeneration{
		sha: "samesha", status: "pending", ingestedAt: start.Add(-30 * time.Hour), workLive: true,
	})
	selections := 0
	for cycle := 0; cycle < 3; cycle++ {
		_, synced := runReconcileCycleSelection(t, world, reposDir, start.Add(time.Duration(cycle)*6*time.Minute), "samesha")
		selections += len(synced.SelectedRepoPaths)
		// The store skips a snapshot equal to the live pending full
		// (pending_same_identity), so no generation is added.
		world.generations = world.generations[:2]
	}
	if selections != 0 {
		t.Fatalf("selected (parsed) %d times over 3 cycles while the pending full is live at the head, want 0", selections)
	}
	world.generations[len(world.generations)-1].workLive = false
	if !runReconcileCycle(t, world, reposDir, start.Add(30*time.Minute), "samesha") {
		t.Fatal("work gone: the wedged scope was not forced")
	}
}

// TestWedgedPendingFullDoesNotSpendTheBudget (ruling a9-126, review r2 N2): a
// held scope early in the repository order no longer takes the only
// per-cycle reconcile slot, so a later overdue scope is forced the same cycle.
func TestWedgedPendingFullDoesNotSpendTheBudget(t *testing.T) {
	now := time.Date(2026, 10, 11, 1, 0, 0, 0, time.UTC)
	reposDir := t.TempDir()
	for _, name := range []string{"a-wedged", "b-overdue"} {
		if err := os.MkdirAll(filepath.Join(reposDir, "github", "org", name, ".git"), 0o755); err != nil {
			t.Fatalf("create .git marker: %v", err)
		}
	}
	writeFakeGitForBaseline(t, `	*"rev-parse refs/remotes/origin/main"*)
		printf "samesha\n"
		;;
	*"checkout -B main refs/remotes/origin/main"*)
		;;`)
	config := reconcileTestConfig(reposDir)
	wedgedScope := gitScopeIDForManagedRepo(config, filepath.Join(reposDir, "github", "org", "a-wedged"))
	states := map[string]scope.FullReconcileState{
		wedgedScope: {
			HasProjectedFull: true, LastProjectedFullAt: now.Add(-72 * time.Hour), LastStoredFullAt: now.Add(-72 * time.Hour),
			HasLatestFull: true, LatestFullAt: now.Add(-30 * time.Hour), LatestFullStatus: scope.GenerationStatusPending,
			LatestFullSourceCommitSHA: "samesha", LatestFullWorkLive: true,
		},
	}
	overdue := projectedFullState(now.Add(-30 * time.Hour))
	resolver := &perScopeResolver{states: states, fallback: overdue}
	synced, err := syncGitRepositoriesWithLogger(context.Background(), config, []string{"github/org/a-wedged", "github/org/b-overdue"}, discardLogger(),
		gitDeltaBaseline{
			Resolver:  resolver,
			Reconcile: reconcilePolicy{Interval: 24 * time.Hour, MaxPerCycle: 1},
			Now:       func() time.Time { return now },
		})
	if err != nil {
		t.Fatalf("syncGitRepositoriesWithLogger() error = %v", err)
	}
	wedgedPath := filepath.Join(reposDir, "github", "org", "a-wedged")
	overduePath := filepath.Join(reposDir, "github", "org", "b-overdue")
	if synced.ReconcileByRepoPath[wedgedPath] {
		t.Fatal("the held wedged scope was forced")
	}
	if !synced.ReconcileByRepoPath[overduePath] {
		t.Fatalf("the later overdue scope was not forced; reconciled = %v", synced.ReconcileByRepoPath)
	}
}

// perScopeResolver answers FullReconcileState per scope, with a fallback.
type perScopeResolver struct {
	states   map[string]scope.FullReconcileState
	fallback scope.FullReconcileState
}

func (r *perScopeResolver) DeltaSyncBaseline(context.Context, string) (scope.DeltaSyncBaseline, error) {
	return activeFullBaseline("samesha"), nil
}

func (r *perScopeResolver) FullReconcileState(_ context.Context, scopeID string) (scope.FullReconcileState, error) {
	if state, ok := r.states[scopeID]; ok {
		return withSyncBaseline(state, activeFullBaseline("samesha")), nil
	}
	return withSyncBaseline(r.fallback, activeFullBaseline("samesha")), nil
}

func (r *perScopeResolver) UncoveredProjectionWriters(context.Context, string) ([]scope.UncoveredProjectionWriter, error) {
	return nil, nil
}

// TestReindexHoldsWedgedPendingFull is the review r3 R3-1 regression (ruling
// a9-128): with an explicit reindex watermark pending and the newest full
// pending past the interval at the remote head with live work, the reindex
// forced (and parsed) every cycle while the store skipped each snapshot as
// pending_same_identity. Both reindex reasons now hold the scope
// (in_flight_expired_held, no parse, no budget slot), with the sweep on and
// off. The hold ends when the head moves or the work is gone; after the
// pending full activates before the watermark, the reindex forces once and
// the store keeps the re-parse (a9-125, TestDecideUnchangedGenerationCells).
func TestReindexHoldsWedgedPendingFull(t *testing.T) {
	now := time.Date(2026, 10, 11, 1, 0, 0, 0, time.UTC)
	reposDir := t.TempDir()
	repoPath := filepath.Join(reposDir, "github", "org", "big")
	if err := os.MkdirAll(filepath.Join(repoPath, ".git"), 0o755); err != nil {
		t.Fatalf("create .git marker: %v", err)
	}
	writeFakeGitForBaseline(t, `	*"rev-parse refs/remotes/origin/main"*)
		printf "samesha\n"
		;;
	*"checkout -B main refs/remotes/origin/main"*)
		;;`)
	config := reconcileTestConfig(reposDir)
	scopeID := gitScopeIDForManagedRepo(config, repoPath)
	wedged := func(sha string, live bool) scope.FullReconcileState {
		return scope.FullReconcileState{
			HasProjectedFull: true, LastProjectedFullAt: now.Add(-72 * time.Hour), LastStoredFullAt: now.Add(-72 * time.Hour),
			HasLatestFull: true, LatestFullAt: now.Add(-30 * time.Hour), LatestFullStatus: scope.GenerationStatusPending,
			LatestFullSourceCommitSHA: sha, LatestFullWorkLive: live,
		}
	}
	watermark := now.Add(-1 * time.Hour)
	repository := map[string]time.Time{scopeID: watermark}
	cases := []struct {
		name       string
		state      scope.FullReconcileState
		interval   time.Duration
		fleet      time.Time
		repository map[string]time.Time
		wantReason string // forced reason; empty means held
	}{
		{"fleet reindex, sweep on, wedged", wedged("samesha", true), 24 * time.Hour, watermark, nil, ""},
		{"fleet reindex, sweep off, wedged", wedged("samesha", true), 0, watermark, nil, ""},
		{"repository reindex, sweep on, wedged", wedged("samesha", true), 24 * time.Hour, time.Time{}, repository, ""},
		{"repository reindex, sweep off, wedged", wedged("samesha", true), 0, time.Time{}, repository, ""},
		{"fleet reindex, head moved", wedged("oldsha", true), 24 * time.Hour, watermark, nil, reconcileReasonReindexRequested},
		{"fleet reindex, work not live", wedged("samesha", false), 0, watermark, nil, reconcileReasonReindexRequested},
		{"fleet reindex, pending full activated before the watermark", projectedFullState(now.Add(-30 * time.Hour)), 0, watermark, nil, reconcileReasonReindexRequested},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
			resolver := &perScopeResolver{states: map[string]scope.FullReconcileState{scopeID: tc.state}}
			synced, err := syncGitRepositoriesWithLogger(context.Background(), config, []string{"github/org/big"}, logger,
				gitDeltaBaseline{
					Resolver:                     resolver,
					Reconcile:                    reconcilePolicy{Interval: tc.interval, MaxPerCycle: 1},
					ReindexRequestedAt:           tc.fleet,
					RepositoryReindexRequestedAt: tc.repository,
					Now:                          func() time.Time { return now },
				})
			if err != nil {
				t.Fatalf("syncGitRepositoriesWithLogger() error = %v", err)
			}
			if tc.wantReason == "" {
				if len(synced.SelectedRepoPaths) != 0 || synced.ReconcileByRepoPath[repoPath] {
					t.Fatalf("selected %d, forced %v; want held with no parse", len(synced.SelectedRepoPaths), synced.ReconcileByRepoPath[repoPath])
				}
				if !bytes.Contains(logs.Bytes(), []byte(`"msg":"git_reconcile_held"`)) || !bytes.Contains(logs.Bytes(), []byte(`"reindex_requested_at":"`+watermark.Format(time.RFC3339))) {
					t.Fatalf("hold log does not name the watermark: %s", logs.String())
				}
				return
			}
			if !synced.ReconcileByRepoPath[repoPath] || synced.ReconcileReasonByRepoPath[repoPath] != tc.wantReason {
				t.Fatalf("forced %v reason %q, want forced %q", synced.ReconcileByRepoPath[repoPath], synced.ReconcileReasonByRepoPath[repoPath], tc.wantReason)
			}
		})
	}
}

// TestReindexHoldDoesNotSpendTheBudget (ruling a9-128): with a fleet reindex
// watermark set, a wedged scope early in the order takes no reconcile slot,
// so the next due scope is forced in the same cycle.
func TestReindexHoldDoesNotSpendTheBudget(t *testing.T) {
	now := time.Date(2026, 10, 11, 1, 0, 0, 0, time.UTC)
	reposDir := t.TempDir()
	for _, name := range []string{"a-wedged", "b-due"} {
		if err := os.MkdirAll(filepath.Join(reposDir, "github", "org", name, ".git"), 0o755); err != nil {
			t.Fatalf("create .git marker: %v", err)
		}
	}
	writeFakeGitForBaseline(t, `	*"rev-parse refs/remotes/origin/main"*)
		printf "samesha\n"
		;;
	*"checkout -B main refs/remotes/origin/main"*)
		;;`)
	config := reconcileTestConfig(reposDir)
	wedgedPath := filepath.Join(reposDir, "github", "org", "a-wedged")
	duePath := filepath.Join(reposDir, "github", "org", "b-due")
	resolver := &perScopeResolver{
		states: map[string]scope.FullReconcileState{gitScopeIDForManagedRepo(config, wedgedPath): {
			HasProjectedFull: true, LastProjectedFullAt: now.Add(-72 * time.Hour), LastStoredFullAt: now.Add(-72 * time.Hour),
			HasLatestFull: true, LatestFullAt: now.Add(-30 * time.Hour), LatestFullStatus: scope.GenerationStatusPending,
			LatestFullSourceCommitSHA: "samesha", LatestFullWorkLive: true,
		}},
		fallback: projectedFullState(now.Add(-30 * time.Hour)),
	}
	synced, err := syncGitRepositoriesWithLogger(context.Background(), config, []string{"github/org/a-wedged", "github/org/b-due"}, discardLogger(),
		gitDeltaBaseline{
			Resolver:           resolver,
			Reconcile:          reconcilePolicy{Interval: 24 * time.Hour, MaxPerCycle: 1},
			ReindexRequestedAt: now.Add(-1 * time.Hour),
			Now:                func() time.Time { return now },
		})
	if err != nil {
		t.Fatalf("syncGitRepositoriesWithLogger() error = %v", err)
	}
	if synced.ReconcileByRepoPath[wedgedPath] {
		t.Fatal("the held wedged scope was forced")
	}
	if !synced.ReconcileByRepoPath[duePath] || synced.ReconcileReasonByRepoPath[duePath] != reconcileReasonReindexRequested {
		t.Fatalf("the next due scope was not forced for the reindex; reasons = %v", synced.ReconcileReasonByRepoPath)
	}
}

// TestReconcileSweepDecisionAtHead pins the exported e2e seam to the sync
// loop's post-fetch hold (rulings a9-126, a9-128).
func TestReconcileSweepDecisionAtHead(t *testing.T) {
	now := time.Date(2026, 10, 11, 1, 0, 0, 0, time.UTC)
	resolver := &perScopeResolver{fallback: scope.FullReconcileState{
		HasProjectedFull: true, LastProjectedFullAt: now.Add(-72 * time.Hour), LastStoredFullAt: now.Add(-72 * time.Hour),
		HasLatestFull: true, LatestFullAt: now.Add(-30 * time.Hour), LatestFullStatus: scope.GenerationStatusPending,
		LatestFullSourceCommitSHA: "samesha", LatestFullWorkLive: true,
	}}
	watermark := now.Add(-time.Hour)
	if due, reason := ReconcileSweepDecisionAtHead(context.Background(), resolver, 0, watermark, time.Time{}, now, "scope-1", "samesha", nil); due || reason != reconcileReasonInFlightExpiredHeld {
		t.Fatalf("same head = (%v, %q), want held", due, reason)
	}
	if due, reason := ReconcileSweepDecisionAtHead(context.Background(), resolver, 0, watermark, time.Time{}, now, "scope-1", "othersha", nil); !due || reason != reconcileReasonReindexRequested {
		t.Fatalf("head moved = (%v, %q), want forced reindex_requested", due, reason)
	}
	if due, reason := ReconcileSweepDecision(context.Background(), resolver, 0, watermark, time.Time{}, now, "scope-1", nil); !due || reason != reconcileReasonReindexRequested {
		t.Fatalf("ReconcileSweepDecision = (%v, %q), want forced (no hold without a head)", due, reason)
	}
}
