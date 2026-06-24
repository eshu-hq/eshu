// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

// TestNativeRepositorySelectorFilesystemDirect_NestedCorpusCollisionFires
// verifies the basename-collision diagnostic in ESHU_FILESYSTEM_DIRECT mode at
// the unsharded default (RepoShardCount=1, the production default). This is the
// regression case for issue #3700: a recursively-nested filesystem corpus (top-
// level repos PLUS a repos/ subdir duplicating them) in direct mode.
//
// The diagnostic must FIRE on the first run (a non-empty changed batch) and stay
// SILENT on an unchanged re-poll (manifest match → anti-spam gate).
func TestNativeRepositorySelectorFilesystemDirect_NestedCorpusCollisionFires(t *testing.T) {
	t.Parallel()

	filesystemRoot := t.TempDir()
	reposDir := t.TempDir()

	// Recursively-nested corpus: top-level repos plus a repos/ subdir duplicating
	// them at multiple depths. Every repos/<name> shares a basename with <name>.
	for _, rel := range []string{
		"svc-alpha",
		"svc-beta",
		"repos/svc-alpha",       // basename svc-alpha collides with svc-alpha
		"repos/svc-beta",        // basename svc-beta collides with svc-beta
		"repos/repos/svc-alpha", // deeper nesting — another collision depth
	} {
		makeCollidingRepo(t, filesystemRoot, rel)
	}

	const warning = "repository basename collision detected (possible accidental corpus nesting)"

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	inst, reader := newCollisionTestInstruments(t)

	selector := NativeRepositorySelector{
		Config: RepoSyncConfig{
			ReposDir:         reposDir,
			SourceMode:       "filesystem",
			FilesystemRoot:   filesystemRoot,
			FilesystemDirect: true, // ESHU_FILESYSTEM_DIRECT=true — production mode
			RepoLimit:        4000,
			GitAuthMethod:    "none",
			// RepoShardCount left at zero → unsharded default (the production case).
		},
		Logger:      logger,
		Instruments: inst,
	}

	// First run: new corpus → non-empty batch → diagnostic must fire.
	batch, err := selector.SelectRepositories(context.Background())
	if err != nil {
		t.Fatalf("SelectRepositories: %v", err)
	}
	if len(batch.Repositories) == 0 {
		t.Fatalf("expected a non-empty batch for a fresh nested corpus, got 0")
	}
	if got := collectBasenameCollisionTotal(t, reader); got == 0 {
		t.Errorf("eshu_dp_repository_basename_collision_total = 0, want > 0 in direct mode on a nested corpus")
	}
	if got := strings.Count(logBuf.String(), warning); got < 1 {
		t.Errorf("collision warning count = %d, want >= 1\nlog:\n%s", got, logBuf.String())
	}

	// Unchanged re-poll: manifest matches → anti-spam gate keeps it silent.
	counterAfterFirst := collectBasenameCollisionTotal(t, reader)
	logAfterFirst := logBuf.String()
	if _, err := selector.SelectRepositories(context.Background()); err != nil {
		t.Fatalf("SelectRepositories re-poll: %v", err)
	}
	if got := collectBasenameCollisionTotal(t, reader); got != counterAfterFirst {
		t.Errorf("re-poll counter advanced %d→%d, want no change (anti-spam gate)", counterAfterFirst, got)
	}
	if got := strings.Count(logBuf.String(), warning); got != strings.Count(logAfterFirst, warning) {
		t.Errorf("re-poll warning count changed, want no re-fire\nlog:\n%s", logBuf.String())
	}
}

// TestNativeRepositorySelectorFilesystemDirect_ShardedCollisionSingleEmit
// verifies the two sharding properties of the basename-collision diagnostic
// (issue #3700, P2 over-report):
//
//  1. Completeness: when sharding splits a colliding pair across buckets (e.g.
//     "svc-beta" → shard 2, "repos/svc-beta" → shard 0 with count=4), the
//     diagnostic still fires, because it inspects the full pre-shard discovered
//     set rather than each shard's post-shard subset. Without the pre-shard fix,
//     no individual shard would detect the collision and the signal would be
//     permanently silent.
//
//  2. Single-emit: only the index-0 shard reports. Every shard inspects the same
//     global set, so letting all N shards fire would inflate one real collision
//     into N duplicate logs and an N× metric reading. The aggregate across all
//     shards must equal the true surplus count, fired exactly once.
func TestNativeRepositorySelectorFilesystemDirect_ShardedCollisionSingleEmit(t *testing.T) {
	t.Parallel()

	// Confirm the split is real so the test is self-documenting, not fragile.
	const shardCount = 4
	if repositoryShardForID("svc-beta", shardCount) == repositoryShardForID("repos/svc-beta", shardCount) {
		t.Fatalf("test corpus requires 'svc-beta' and 'repos/svc-beta' to hash to "+
			"different shards with count=%d; they collided — choose a different corpus", shardCount)
	}

	filesystemRoot := t.TempDir()
	for _, rel := range []string{
		"svc-alpha",             // shard 0 with count=4
		"svc-beta",              // shard 2 with count=4
		"repos/svc-alpha",       // shard 2 — basename svc-alpha → collision
		"repos/svc-beta",        // shard 0 — basename svc-beta → collision
		"repos/repos/svc-alpha", // shard 0 — basename svc-alpha → collision
	} {
		makeCollidingRepo(t, filesystemRoot, rel)
	}

	const warning = "repository basename collision detected (possible accidental corpus nesting)"

	// True surplus across the full discovered set:
	//   svc-alpha appears 3× (svc-alpha, repos/svc-alpha, repos/repos/svc-alpha) → surplus 2
	//   svc-beta  appears 2× (svc-beta, repos/svc-beta)                          → surplus 1
	// => total surplus = 3, fired once (from shard 0 only).
	const wantTotalSurplus int64 = 3

	var aggregateCounter int64
	aggregateWarnings := 0
	firedShards := make([]int, 0, shardCount)

	for shardIdx := 0; shardIdx < shardCount; shardIdx++ {
		reposDir := t.TempDir()
		var logBuf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn}))
		inst, reader := newCollisionTestInstruments(t)

		selector := NativeRepositorySelector{
			Config: RepoSyncConfig{
				ReposDir:         reposDir,
				SourceMode:       "filesystem",
				FilesystemRoot:   filesystemRoot,
				FilesystemDirect: true,
				RepoLimit:        4000,
				GitAuthMethod:    "none",
				RepoShardCount:   shardCount,
				RepoShardIndex:   shardIdx,
			},
			Logger:      logger,
			Instruments: inst,
		}

		if _, err := selector.SelectRepositories(context.Background()); err != nil {
			t.Fatalf("shard %d: SelectRepositories: %v", shardIdx, err)
		}

		counter := collectBasenameCollisionTotal(t, reader)
		warnings := strings.Count(logBuf.String(), warning)
		aggregateCounter += counter
		aggregateWarnings += warnings
		if warnings > 0 || counter > 0 {
			firedShards = append(firedShards, shardIdx)
			if shardIdx != 0 {
				t.Errorf("shard %d fired the diagnostic (counter=%d warnings=%d); "+
					"only the index-0 shard may emit the global signal", shardIdx, counter, warnings)
			}
		}
	}

	// Exactly the index-0 shard fired.
	if len(firedShards) != 1 || firedShards[0] != 0 {
		t.Errorf("fired shards = %v, want exactly [0] (single-emit)", firedShards)
	}
	// Aggregate equals the true surplus once — not multiplied by shard count.
	if aggregateCounter != wantTotalSurplus {
		t.Errorf("aggregate counter across shards = %d, want %d (true surplus, single-emit)",
			aggregateCounter, wantTotalSurplus)
	}
	// One WARN per colliding basename (svc-alpha, svc-beta) from shard 0 only.
	if aggregateWarnings != 2 {
		t.Errorf("aggregate warning count across shards = %d, want 2 "+
			"(one per colliding basename, fired once from shard 0)", aggregateWarnings)
	}
}

// TestNativeRepositorySelectorFilesystemDirect_CollisionFiresWhenShardZeroEmpty
// is the regression test for issue #3700 P2 (Codex review on PR #3706).
//
// The single-emit gate pins the basename-collision report to the index-0 shard.
// If that emission also required the index-0 shard to OWN a changed repo (the old
// len(repoPaths) > 0 gate), the diagnostic would be silenced whenever every
// colliding repo hashes to a NON-zero shard: shard 0 has an empty subset and
// skips, while the shard that holds the collision is barred from emitting.
//
// Corpus: both "worker" and "repos/worker" hash to shard 1 with count=2 (verified
// via repositoryShardForID), so shard 0 owns nothing and shard 1 owns the whole
// collision. The diagnostic must still fire exactly once — from shard 0 — because
// the emit gate keys on the full-corpus changed signal (corpusChanged), not on
// the index-0 shard's own materialized paths. The re-poll must stay silent.
func TestNativeRepositorySelectorFilesystemDirect_CollisionFiresWhenShardZeroEmpty(t *testing.T) {
	t.Parallel()

	const shardCount = 2
	// Assert the whole colliding pair hashes to a single non-zero shard so the
	// test exercises the empty-shard-0 path; otherwise it proves nothing.
	flatShard := repositoryShardForID("worker", shardCount)
	nestedShard := repositoryShardForID("repos/worker", shardCount)
	if flatShard == 0 || nestedShard == 0 || flatShard != nestedShard {
		t.Fatalf("test corpus requires 'worker' and 'repos/worker' to both hash to the "+
			"same NON-zero shard with count=%d; got worker→%d repos/worker→%d — "+
			"choose a different corpus", shardCount, flatShard, nestedShard)
	}

	filesystemRoot := t.TempDir()
	makeCollidingRepo(t, filesystemRoot, "worker")
	makeCollidingRepo(t, filesystemRoot, "repos/worker") // basename worker → collision

	const warning = "repository basename collision detected (possible accidental corpus nesting)"

	// reposDir is per-shard and shared across that shard's polls so the manifest
	// persists between calls, exactly as under steady-state Service.Run.
	reposDirByShard := []string{t.TempDir(), t.TempDir()}

	runShard := func(idx int) (int64, int, int) {
		var logBuf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn}))
		inst, reader := newCollisionTestInstruments(t)
		selector := NativeRepositorySelector{
			Config: RepoSyncConfig{
				ReposDir:         reposDirByShard[idx],
				SourceMode:       "filesystem",
				FilesystemRoot:   filesystemRoot,
				FilesystemDirect: true,
				RepoLimit:        4000,
				GitAuthMethod:    "none",
				RepoShardCount:   shardCount,
				RepoShardIndex:   idx,
			},
			Logger:      logger,
			Instruments: inst,
		}
		batch, err := selector.SelectRepositories(context.Background())
		if err != nil {
			t.Fatalf("shard %d: SelectRepositories: %v", idx, err)
		}
		return collectBasenameCollisionTotal(t, reader), strings.Count(logBuf.String(), warning), len(batch.Repositories)
	}

	// First poll across both shards.
	c0, w0, indexed0 := runShard(0)
	c1, w1, indexed1 := runShard(1)

	if indexed0 != 0 {
		t.Fatalf("precondition: shard 0 indexed %d repos, want 0 (corpus hashes to shard 1)", indexed0)
	}
	if indexed1 != 2 {
		t.Fatalf("precondition: shard 1 indexed %d repos, want 2 (owns the collision)", indexed1)
	}
	// Shard 0 owns nothing but must still fire the global collision once.
	if c0 != 1 || w0 != 1 {
		t.Errorf("shard 0 (empty subset): counter=%d warnings=%d, want 1,1 "+
			"(must fire on full-corpus changed signal, not its own paths)", c0, w0)
	}
	// Shard 1 owns the collision but must stay silent (single-emit on shard 0).
	if c1 != 0 || w1 != 0 {
		t.Errorf("shard 1 (owns collision): counter=%d warnings=%d, want 0,0 (single-emit on shard 0)", c1, w1)
	}

	// Unchanged re-poll on shard 0: manifest matches → corpusChanged false → silent.
	// runShard uses a fresh reader/logger per call, so a silent poll reports 0,0.
	c0b, w0b, _ := runShard(0)
	if c0b != 0 || w0b != 0 {
		t.Errorf("shard 0 re-poll: counter=%d warnings=%d, want 0,0 (no re-fire on unchanged corpus)", c0b, w0b)
	}
}
