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
