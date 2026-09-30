// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

// dirtyWriter is one #7389 uncovered projection writer.
func dirtyWriter() scope.UncoveredProjectionWriter {
	return scope.UncoveredProjectionWriter{
		GenerationID: "gen-b", Status: scope.GenerationStatusSuperseded,
		FailureClass:             "projector_delta_baseline_mismatch_after_projection",
		ProjectionWriteStartedAt: time.Date(2026, 9, 19, 0, 30, 0, 0, time.UTC),
	}
}

// TestGraphDirtyForcesReconcileAtActiveHead is the #7389 F1 regression: a
// generation wrote the graph and never activated, the projected full is fresh,
// and the remote head equals the active commit. The sync must force a
// reconciliation (so the snapshot carries Reconcile and an empty freshness
// hint and is not skipped as unchanged), count it as reason graph_dirty on the
// reconciliation counter and not on the delta-baseline fallback counter, and
// log the writers.
func TestGraphDirtyForcesReconcileAtActiveHead(t *testing.T) {
	start := time.Date(2026, 9, 19, 1, 0, 0, 0, time.UTC)
	world, reposDir := newReconcileWorld(t, start)
	world.generations[0].ingestedAt = start.Add(-time.Hour) // fresh projected full
	world.writers = []scope.UncoveredProjectionWriter{dirtyWriter()}
	instruments, reader := newCollisionTestInstruments(t)
	var logs bytes.Buffer
	forced, synced := runObservedReconcileCycle(t, world, reposDir, start, "samesha", instruments,
		slog.New(slog.NewJSONHandler(&logs, nil)))
	repoPath := filepath.Join(reposDir, "github", "org", "big")
	if !forced || len(synced.SelectedRepoPaths) != 1 {
		t.Fatalf("forced = %t, selected = %v; want a forced reconcile of %s", forced, synced.SelectedRepoPaths, repoPath)
	}
	if got := reconcileCounterByReason(t, reader, "eshu_dp_collector_reconciliation_full_snapshots_total"); got["graph_dirty"] != 1 {
		t.Fatalf("reconciliation counter = %v, want graph_dirty=1", got)
	}
	if got := fallbackCountsByReason(t, reader); len(got) != 0 {
		t.Fatalf("delta-baseline fallback counter = %v, want nothing (the reconciliation reason is the signal)", got)
	}
	for _, want := range []string{
		`"msg":"git_delta_baseline_graph_dirty"`, `"generation_ids":["gen-b"]`,
		`"msg":"git_reconcile_forced"`, `"reason":"graph_dirty"`,
	} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("logs lack %s:\n%s", want, logs.String())
		}
	}
}

// TestGraphDirtyRespectsReconcileThrottle: a full still in flight, or one that
// failed inside Interval/4, suppresses the graph_dirty reconcile exactly like
// the sweep; the delta path keeps running meanwhile (a no-op at the active head).
func TestGraphDirtyRespectsReconcileThrottle(t *testing.T) {
	start := time.Date(2026, 9, 19, 1, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		status string
	}{{"full_in_flight", "pending"}, {"full_failed_recently", "failed"}} {
		t.Run(tc.name, func(t *testing.T) {
			world, reposDir := newReconcileWorld(t, start)
			world.generations[0].ingestedAt = start.Add(-time.Hour)
			world.generations = append(world.generations, reconcileWorldGeneration{
				sha: "samesha", status: tc.status, ingestedAt: start.Add(-30 * time.Minute),
			})
			world.writers = []scope.UncoveredProjectionWriter{dirtyWriter()}
			forced, synced := runReconcileCycleSelection(t, world, reposDir, start, "samesha")
			if forced || len(synced.SelectedRepoPaths) != 0 {
				t.Fatalf("forced = %t, selected = %v; want the graph_dirty reconcile suppressed by the throttle", forced, synced.SelectedRepoPaths)
			}
		})
	}
}

// dirtyScopes is a resolver for several scopes: each has a fresh projected
// full at the remote head, and dirty scopes report an uncovered writer.
type dirtyScopes struct {
	now   time.Time
	dirty map[string]bool
}

func (d *dirtyScopes) LastProjectedCommitSHA(context.Context, string) (string, error) {
	return "samesha", nil
}

func (d *dirtyScopes) FullReconcileState(context.Context, string) (scope.FullReconcileState, error) {
	at := d.now.Add(-time.Hour)
	return scope.FullReconcileState{
		HasProjectedFull: true, LastProjectedFullAt: at, HasLatestFull: true, LatestFullAt: at,
		LatestFullStatus: scope.GenerationStatusActive, LatestFullProjected: true,
	}, nil
}

func (d *dirtyScopes) UncoveredProjectionWriters(_ context.Context, scopeID string) ([]scope.UncoveredProjectionWriter, error) {
	if d.dirty[scopeID] {
		return []scope.UncoveredProjectionWriter{dirtyWriter()}, nil
	}
	return nil, nil
}

// TestGraphDirtyCountsAgainstTheReconcileBudget: a graph_dirty reconcile uses
// the per-cycle ESHU_REPO_RECONCILE_MAX_PER_CYCLE budget. With a budget of one
// and two dirty scopes, the second stays on its delta path (a no-op at the
// active head) and is forced on the next cycle.
func TestGraphDirtyCountsAgainstTheReconcileBudget(t *testing.T) {
	reposDir := t.TempDir()
	repos := []string{"github/org/one", "github/org/two"}
	for _, repo := range repos {
		if err := os.MkdirAll(filepath.Join(reposDir, filepath.FromSlash(repo), ".git"), 0o755); err != nil {
			t.Fatalf("create .git marker: %v", err)
		}
	}
	writeFakeGitForBaseline(t, `	*"rev-parse refs/remotes/origin/main"*)
		printf "samesha\n"
		;;
	*"checkout -B main refs/remotes/origin/main"*)
		;;`)
	config := reconcileTestConfig(reposDir)
	config.ReconcileMaxPerCycle = 1
	now := time.Date(2026, 9, 19, 1, 0, 0, 0, time.UTC)
	resolver := &dirtyScopes{now: now, dirty: map[string]bool{}}
	for _, repo := range repos {
		resolver.dirty[gitScopeIDForManagedRepo(config, filepath.Join(reposDir, filepath.FromSlash(repo)))] = true
	}
	run := func() GitSyncSelection {
		synced, err := syncGitRepositoriesWithLogger(context.Background(), config, repos, discardLogger(),
			gitDeltaBaseline{Resolver: resolver, Reconcile: reconcilePolicyFromConfig(config), Now: func() time.Time { return now }})
		if err != nil {
			t.Fatalf("sync: %v", err)
		}
		return synced
	}
	first := run()
	if len(first.ReconcileByRepoPath) != 1 || len(first.SelectedRepoPaths) != 1 {
		t.Fatalf("cycle 1 reconciled %v selected %v, want exactly one forced full within the budget", first.ReconcileByRepoPath, first.SelectedRepoPaths)
	}
	for path := range first.ReconcileByRepoPath {
		resolver.dirty[gitScopeIDForManagedRepo(config, path)] = false // its full covered the writer
	}
	second := run()
	if len(second.ReconcileByRepoPath) != 1 || len(second.SelectedRepoPaths) != 1 {
		t.Fatalf("cycle 2 reconciled %v selected %v, want the deferred scope forced now", second.ReconcileByRepoPath, second.SelectedRepoPaths)
	}
	for path := range second.ReconcileByRepoPath {
		if first.ReconcileByRepoPath[path] {
			t.Fatalf("cycle 2 re-forced %s instead of the deferred scope", path)
		}
	}
}
