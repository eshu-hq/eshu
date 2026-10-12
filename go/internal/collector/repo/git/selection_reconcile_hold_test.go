// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
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

func (r *perScopeResolver) LastProjectedCommitSHA(context.Context, string) (string, error) {
	return "samesha", nil
}

func (r *perScopeResolver) FullReconcileState(_ context.Context, scopeID string) (scope.FullReconcileState, error) {
	if state, ok := r.states[scopeID]; ok {
		return state, nil
	}
	return r.fallback, nil
}

func (r *perScopeResolver) UncoveredProjectionWriters(context.Context, string) ([]scope.UncoveredProjectionWriter, error) {
	return nil, nil
}
