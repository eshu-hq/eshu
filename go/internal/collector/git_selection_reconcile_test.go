// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func reconcileTestConfig(reposDir string) RepoSyncConfig {
	return RepoSyncConfig{
		SourceMode:           "explicit",
		ReposDir:             reposDir,
		GitAuthMethod:        "none",
		CloneDepth:           1,
		ReconcileInterval:    24 * time.Hour,
		ReconcileMaxPerCycle: 10,
	}
}

func TestReconcileDueWhenNoFullProjectionExists(t *testing.T) {
	t.Parallel()

	reposDir := t.TempDir()
	repoPath := filepath.Join(reposDir, "github", "org", "repo")
	resolver := &stubBaselineResolver{lastFullOK: false}
	baseline := gitDeltaBaseline{
		Resolver:  resolver,
		Reconcile: reconcilePolicy{Interval: 24 * time.Hour},
		Now:       func() time.Time { return time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC) },
	}

	if !baseline.reconcileDue(context.Background(), reconcileTestConfig(reposDir), repoPath) {
		t.Fatal("reconcileDue = false, want true when no full projection exists")
	}
}

func TestReconcileDueRespectsInterval(t *testing.T) {
	t.Parallel()

	reposDir := t.TempDir()
	repoPath := filepath.Join(reposDir, "github", "org", "repo")
	now := time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC)

	cases := []struct {
		name     string
		lastFull time.Time
		want     bool
	}{
		{"recent full not due", now.Add(-1 * time.Hour), false},
		{"old full due", now.Add(-48 * time.Hour), true},
		{"exactly at interval due", now.Add(-24 * time.Hour), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resolver := &stubBaselineResolver{lastFull: tc.lastFull, lastFullOK: true}
			baseline := gitDeltaBaseline{
				Resolver:  resolver,
				Reconcile: reconcilePolicy{Interval: 24 * time.Hour},
				Now:       func() time.Time { return now },
			}
			if got := baseline.reconcileDue(context.Background(), reconcileTestConfig(reposDir), repoPath); got != tc.want {
				t.Fatalf("reconcileDue = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestReconcileDisabledWhenIntervalZero(t *testing.T) {
	t.Parallel()

	reposDir := t.TempDir()
	repoPath := filepath.Join(reposDir, "github", "org", "repo")
	resolver := &stubBaselineResolver{lastFullOK: false}
	baseline := gitDeltaBaseline{Resolver: resolver, Reconcile: reconcilePolicy{Interval: 0}}

	if baseline.reconcileDue(context.Background(), reconcileTestConfig(reposDir), repoPath) {
		t.Fatal("reconcileDue = true, want false when reconciliation disabled")
	}
	if len(resolver.fullScopeIDs) != 0 {
		t.Fatalf("disabled reconciliation must not query, got %d lookups", len(resolver.fullScopeIDs))
	}
}

// TestSyncForcesBoundedReconciliationFullSnapshot proves the sweep forces a full
// snapshot for an overdue scope, marks it for the freshness-hint bypass, and
// respects the per-cycle cap: with two overdue repos and MaxPerCycle=1, exactly
// one is reconciled (full) and the other still takes its normal delta.
func TestSyncForcesBoundedReconciliationFullSnapshot(t *testing.T) {
	reposDir := t.TempDir()
	repoA := filepath.Join(reposDir, "github", "org", "a")
	repoB := filepath.Join(reposDir, "github", "org", "b")
	for _, p := range []string{repoA, repoB} {
		if err := os.MkdirAll(filepath.Join(p, ".git"), 0o755); err != nil {
			t.Fatalf("create .git marker: %v", err)
		}
	}
	// Delta path for the non-reconciled repo: baseline "basesha" reachable,
	// remote "newsha", one changed file. Reconciled repo takes the empty-baseline
	// full path (rev-parse remote + checkout, no diff).
	writeFakeGitForBaseline(t, `	*"rev-parse refs/remotes/origin/main"*)
		printf "newsha\n"
		;;
	*"cat-file -e basesha"*)
		exit 0
		;;
	*"diff --name-status -z --find-renames basesha refs/remotes/origin/main"*)
		printf "M\0changed.go\0"
		;;
	*"checkout -B main refs/remotes/origin/main"*)
		;;`)

	config := reconcileTestConfig(reposDir)
	config.ReconcileMaxPerCycle = 1
	resolver := &stubBaselineResolver{sha: "basesha", lastFullOK: false}
	synced, err := syncGitRepositoriesWithLogger(
		context.Background(),
		config,
		[]string{"github/org/a", "github/org/b"},
		discardLogger(),
		gitDeltaBaseline{
			Resolver:  resolver,
			Reconcile: reconcilePolicy{Interval: 24 * time.Hour, MaxPerCycle: 1},
			Now:       func() time.Time { return time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC) },
		},
	)
	if err != nil {
		t.Fatalf("syncGitRepositoriesWithLogger() error = %v", err)
	}

	if got := len(synced.ReconcileByRepoPath); got != 1 {
		t.Fatalf("reconciled repos = %d, want 1 (per-cycle cap)", got)
	}
	if !synced.ReconcileByRepoPath[repoA] {
		t.Fatalf("repoA must be the reconciled repo; ReconcileByRepoPath = %#v", synced.ReconcileByRepoPath)
	}
	// repoA reconciled => full snapshot => no delta. repoB => normal delta.
	if _, ok := synced.DeltaByRepoPath[repoA]; ok {
		t.Fatalf("reconciled repoA must have no delta, got %#v", synced.DeltaByRepoPath[repoA])
	}
	if _, ok := synced.DeltaByRepoPath[repoB]; !ok {
		t.Fatalf("budget-exhausted repoB must take a normal delta; deltas = %#v", synced.DeltaByRepoPath)
	}
}
