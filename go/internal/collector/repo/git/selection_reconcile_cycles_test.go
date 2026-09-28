// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// reconcileWorldGeneration is one scope generation in the stateful fake the
// multi-cycle reconcile tests drive (#7288).
type reconcileWorldGeneration struct {
	sha        string
	isDelta    bool
	status     string
	ingestedAt time.Time
	activated  bool
}

// reconcileWorld is a stateful fake of one scope's scope_generations rows. It
// answers the resolver reads the way the Postgres queries do and records every
// generation the sync produces, so a test can move generations through the
// projector lifecycle between selection cycles.
type reconcileWorld struct {
	generations []reconcileWorldGeneration
}

func (w *reconcileWorld) LastProjectedCommitSHA(_ context.Context, _ string) (string, error) {
	for _, generation := range w.generations {
		if generation.status == "active" && generation.activated {
			return generation.sha, nil
		}
	}
	return "", nil
}

// FullReconcileState mirrors fullReconcileStateQuery: the newest activated
// full generation, and the newest full generation of any status.
func (w *reconcileWorld) FullReconcileState(_ context.Context, _ string) (scope.FullReconcileState, error) {
	var state scope.FullReconcileState
	for _, generation := range w.generations {
		if generation.isDelta {
			continue
		}
		if !state.HasLatestFull || !generation.ingestedAt.Before(state.LatestFullAt) {
			state.HasLatestFull = true
			state.LatestFullAt = generation.ingestedAt
			state.LatestFullStatus = scope.GenerationStatus(generation.status)
			state.LatestFullProjected = generation.activated
		}
		switch generation.status {
		case "active", "completed", "superseded":
		default:
			continue
		}
		if generation.activated && (!state.HasProjectedFull || !generation.ingestedAt.Before(state.LastProjectedFullAt)) {
			state.HasProjectedFull = true
			state.LastProjectedFullAt = generation.ingestedAt
		}
	}
	return state, nil
}

// lastFull returns the index of the newest full generation.
func (w *reconcileWorld) lastFull() int {
	for i := len(w.generations) - 1; i >= 0; i-- {
		if !w.generations[i].isDelta {
			return i
		}
	}
	return -1
}

// runReconcileCycle runs one selection cycle at now against the world and
// records a pending generation for the scope when the sync produced one.
// It reports whether the cycle forced a reconciliation snapshot.
func runReconcileCycle(t *testing.T, world *reconcileWorld, reposDir string, now time.Time, remoteSHA string) bool {
	forced, _ := runReconcileCycleSelection(t, world, reposDir, now, remoteSHA)
	return forced
}

// runReconcileCycleSelection is runReconcileCycle that also returns the
// selection, so a test can inspect the delta the cycle produced.
func runReconcileCycleSelection(t *testing.T, world *reconcileWorld, reposDir string, now time.Time, remoteSHA string) (bool, GitSyncSelection) {
	t.Helper()
	return runObservedReconcileCycle(t, world, reposDir, now, remoteSHA, nil, discardLogger())
}

// runObservedReconcileCycle is runReconcileCycleSelection with the telemetry
// instruments and logger the sync records into.
func runObservedReconcileCycle(
	t *testing.T,
	world *reconcileWorld,
	reposDir string,
	now time.Time,
	remoteSHA string,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) (bool, GitSyncSelection) {
	t.Helper()
	config := reconcileTestConfig(reposDir)
	synced, err := syncGitRepositoriesWithLogger(
		context.Background(), config, []string{"github/org/big"}, logger,
		gitDeltaBaseline{
			Resolver:    world,
			Instruments: instruments,
			Reconcile:   reconcilePolicy{Interval: 24 * time.Hour, MaxPerCycle: 10},
			Now:         func() time.Time { return now },
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
			sha: remoteSHA, isDelta: isDelta && !forced, status: "pending", ingestedAt: now,
		})
	}
	return forced, synced
}

// newReconcileWorld builds a scope whose only projected full generation is
// 25 hours old at start and whose active commit equals the remote head.
func newReconcileWorld(t *testing.T, start time.Time) (*reconcileWorld, string) {
	t.Helper()
	reposDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(reposDir, "github", "org", "big", ".git"), 0o755); err != nil {
		t.Fatalf("create .git marker: %v", err)
	}
	writeFakeGitForBaseline(t, `	*"rev-parse refs/remotes/origin/main"*)
		printf "samesha\n"
		;;
	*"checkout -B main refs/remotes/origin/main"*)
		;;`)
	world := &reconcileWorld{generations: []reconcileWorldGeneration{{
		sha: "samesha", status: "active", activated: true, ingestedAt: start.Add(-25 * time.Hour),
	}}}
	return world, reposDir
}

// TestReconcileSweepForcesOnceWhilePreviousFullPending pins #7288: while the
// forced full generation is still pending, later cycles with an unchanged
// remote head must not force another full snapshot.
func TestReconcileSweepForcesOnceWhilePreviousFullPending(t *testing.T) {
	start := time.Date(2026, 9, 19, 1, 0, 0, 0, time.UTC)
	world, reposDir := newReconcileWorld(t, start)

	forcedCount := 0
	for cycle := 0; cycle < 5; cycle++ {
		if runReconcileCycle(t, world, reposDir, start.Add(time.Duration(cycle)*6*time.Minute), "samesha") {
			forcedCount++
		}
	}
	if forcedCount != 1 {
		t.Fatalf("forced reconciles across 5 cycles = %d, want exactly 1 while the first stays pending", forcedCount)
	}
}

// TestReconcileSweepBacksOffAfterFailedFull pins the A' retry backoff: a forced
// full that fails is not re-forced until Interval/4 has passed since it was
// ingested, then exactly one new full is forced.
func TestReconcileSweepBacksOffAfterFailedFull(t *testing.T) {
	start := time.Date(2026, 9, 19, 1, 0, 0, 0, time.UTC)
	world, reposDir := newReconcileWorld(t, start)

	if !runReconcileCycle(t, world, reposDir, start, "samesha") {
		t.Fatal("cycle 0 did not force the overdue reconcile")
	}
	world.generations[world.lastFull()].status = "failed"

	forcedCount := 0
	now := start
	for now.Sub(start) < 6*time.Hour-6*time.Minute {
		now = now.Add(6 * time.Minute)
		if runReconcileCycle(t, world, reposDir, now, "samesha") {
			forcedCount++
		}
	}
	if forcedCount != 0 {
		t.Fatalf("forced reconciles inside the Interval/4 backoff = %d, want 0", forcedCount)
	}
	for cycle := 0; cycle < 3; cycle++ {
		now = now.Add(6 * time.Minute)
		if runReconcileCycle(t, world, reposDir, now, "samesha") {
			forcedCount++
		}
	}
	if forcedCount != 1 {
		t.Fatalf("forced reconciles after the backoff = %d, want exactly 1", forcedCount)
	}
}

// TestReconcileSweepControlProjectedFullNotForced is the control: a projected
// full one hour old keeps the scope fresh, so nothing is forced.
func TestReconcileSweepControlProjectedFullNotForced(t *testing.T) {
	start := time.Date(2026, 9, 19, 1, 0, 0, 0, time.UTC)
	world, reposDir := newReconcileWorld(t, start)
	world.generations[0].ingestedAt = start.Add(-time.Hour)

	for cycle := 0; cycle < 3; cycle++ {
		if runReconcileCycle(t, world, reposDir, start.Add(time.Duration(cycle)*6*time.Minute), "samesha") {
			t.Fatalf("cycle %d forced a reconcile with a projected full one hour old", cycle)
		}
	}
	if len(world.generations) != 1 {
		t.Fatalf("generations = %d, want 1 (unchanged head, fresh full)", len(world.generations))
	}
}

// writeFakeRemote points the fake git at remote head headSHA and answers a
// delta diff only when it is taken against diffBase, so a delta built on any
// other baseline carries no changed files.
func writeFakeRemote(t *testing.T, headSHA, diffBase string) {
	t.Helper()
	writeFakeGitForBaseline(t, `	*"rev-parse refs/remotes/origin/main"*)
		printf "`+headSHA+`\n"
		;;
	*"diff --name-status -z --find-renames `+diffBase+` refs/remotes/origin/main"*)
		printf "M\0changed.go\0"
		;;
	*"checkout -B main refs/remotes/origin/main"*)
		;;`)
}

// TestReconcileSweepDeltaSupersedesPendingFull pins the A'/B interaction: a
// delta taken while a forced full is pending diffs against the ACTIVE commit,
// not the pending full's commit; when the delta supersedes the full before it
// activates, the full is retried once after the Interval/4 backoff.
func TestReconcileSweepDeltaSupersedesPendingFull(t *testing.T) {
	start := time.Date(2026, 9, 19, 1, 0, 0, 0, time.UTC)
	world, reposDir := newReconcileWorld(t, start)
	world.generations[0].sha = "sha-active"

	writeFakeRemote(t, "sha-full", "sha-active")
	if !runReconcileCycle(t, world, reposDir, start, "sha-full") {
		t.Fatal("cycle 0 did not force the overdue reconcile")
	}
	pendingFull := world.lastFull()

	writeFakeRemote(t, "sha-delta", "sha-active")
	now := start.Add(6 * time.Minute)
	forced, synced := runReconcileCycleSelection(t, world, reposDir, now, "sha-delta")
	if forced {
		t.Fatal("cycle 1 forced a second full while the first is in flight")
	}
	repoPath := filepath.Join(reposDir, "github", "org", "big")
	delta, ok := synced.DeltaByRepoPath[repoPath]
	if !ok || len(delta.ChangedFileTargets) == 0 {
		t.Fatalf("cycle 1 delta = %#v (ok=%v), want a delta diffed against the active commit sha-active", delta, ok)
	}

	// Projector: the delta claims first, supersedes the pending full before it
	// activates, and becomes the active generation.
	world.generations[pendingFull].status = "superseded"
	world.generations[0].status = "superseded"
	deltaIndex := len(world.generations) - 1
	world.generations[deltaIndex].status = "active"
	world.generations[deltaIndex].activated = true

	forcedCount := 0
	for now.Sub(start) < 6*time.Hour-6*time.Minute {
		now = now.Add(6 * time.Minute)
		if runReconcileCycle(t, world, reposDir, now, "sha-delta") {
			forcedCount++
		}
	}
	if forcedCount != 0 {
		t.Fatalf("forced reconciles inside the Interval/4 backoff = %d, want 0", forcedCount)
	}
	for cycle := 0; cycle < 3; cycle++ {
		now = now.Add(6 * time.Minute)
		if runReconcileCycle(t, world, reposDir, now, "sha-delta") {
			forcedCount++
		}
	}
	if forcedCount != 1 {
		t.Fatalf("forced reconciles after the backoff = %d, want exactly 1", forcedCount)
	}
}
