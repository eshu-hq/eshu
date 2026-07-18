// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// boltOrphanSweepReader adapts the bolt test runner to OrphanSweepReader.
type boltOrphanSweepReader struct {
	runner *boltRetractTestRunner
}

func (r *boltOrphanSweepReader) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	return r.runner.runCypher(ctx, cypher, params)
}

// legacyNotDashDashOrphanMarkCypher and legacyNotDashDashOrphanSweepCypher are
// verbatim copies of the origin/main (pre-#5147) mark/sweep statements for
// the File label (see git history of orphan_sweep.go before this branch).
// They are reproduced here, not imported, specifically to demonstrate on the
// live backend that the `NOT (n)--()` relationship-existence predicate never
// matches -- the historical sweep is a silent no-op on both pinned NornicDB
// backends. This is the RED reproduction the #5147 anti-join redesign fixes.
const (
	legacyNotDashDashOrphanMarkCypher = `MATCH (n:File)
WHERE n.evidence_source IS NOT NULL
  AND n.eshu_orphan_observed_at_unix IS NULL
  AND NOT (n)--()
WITH n LIMIT $limit
SET n.eshu_orphan_observed_at_unix = $observed_at_unix`

	legacyNotDashDashOrphanSweepCypher = `MATCH (n:File)
WHERE n.evidence_source IS NOT NULL
  AND n.eshu_orphan_observed_at_unix <= $cutoff_unix
  AND NOT (n)--()
WITH n LIMIT $limit
DELETE n`
)

// TestLiveOrphanAntiJoinReplacesBrokenNotDashDashPredicate is the committed
// live discriminating regression for #5147. It first reproduces the
// historical no-op (the origin/main `NOT (n)--()` mark/sweep statements never
// match a true orphan on the pinned NornicDB backends), then proves the new
// anti-join OrphanSweepStore correctly marks, ages, sweeps a true orphan,
// clears a relinked node's marker, and preserves connected nodes across two
// injected-clock cycles.
//
// Gate: ESHU_CYPHER_BOLT_DSN must be set (e.g. bolt://127.0.0.1:17688 or
// bolt://127.0.0.1:17689). When unset the test skips.
func TestLiveOrphanAntiJoinReplacesBrokenNotDashDashPredicate(t *testing.T) {
	runner := openBoltTestRunner(t)
	t.Cleanup(func() { runner.close(context.Background()) })
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	orphanPath := "5147-orphan-" + suffix
	connectedPath := "5147-connected-" + suffix
	peerPath := "5147-peer-" + suffix
	relinkedPath := "5147-relinked-" + suffix
	relinkedPeerPath := "5147-relinked-peer-" + suffix
	paths := []string{orphanPath, connectedPath, peerPath, relinkedPath, relinkedPeerPath}

	t.Cleanup(func() {
		if err := boltWriteStatement(
			context.Background(), runner,
			`MATCH (n:File) WHERE n.path IN $paths DETACH DELETE n`,
			map[string]any{"paths": paths},
		); err != nil {
			t.Errorf("cleanup live orphan anti-join proof: %v", err)
		}
	})
	// Best-effort pre-clean in case a prior run crashed before cleanup.
	_ = boltWriteStatement(
		ctx, runner,
		`MATCH (n:File) WHERE n.path IN $paths DETACH DELETE n`,
		map[string]any{"paths": paths},
	)

	seedFile := func(path string) {
		if err := boltWriteStatement(
			ctx, runner,
			`MERGE (n:File {path: $path}) SET n.evidence_source = 'test-5147-live'`,
			map[string]any{"path": path},
		); err != nil {
			t.Fatalf("seed File %s: %v", path, err)
		}
	}
	for _, p := range paths {
		seedFile(p)
	}
	mergeContains := func(a, b string) {
		if err := boltWriteStatement(
			ctx, runner,
			`MATCH (a:File {path: $a}) MATCH (b:File {path: $b}) MERGE (a)-[:CONTAINS]->(b)`,
			map[string]any{"a": a, "b": b},
		); err != nil {
			t.Fatalf("merge CONTAINS %s -> %s: %v", a, b, err)
		}
	}
	mergeContains(connectedPath, peerPath)

	// --- RED: reproduce the origin/main NOT (n)--() no-op on the true orphan ---
	t.Run("historical_not_dash_dash_predicate_is_a_no_op", func(t *testing.T) {
		nowUnix := time.Now().Unix()
		if err := boltWriteStatement(ctx, runner, legacyNotDashDashOrphanMarkCypher, map[string]any{
			"observed_at_unix": nowUnix,
			"limit":            1000,
		}); err != nil {
			t.Fatalf("legacy mark statement errored (expected to run, just not match): %v", err)
		}
		markRows, err := runner.runCypher(ctx, `MATCH (n:File {path: $path}) RETURN n.eshu_orphan_observed_at_unix AS observed_at`,
			map[string]any{"path": orphanPath})
		if err != nil {
			t.Fatalf("read back orphan marker: %v", err)
		}
		if len(markRows) == 0 {
			t.Fatal("orphan node missing after legacy mark statement")
		}
		if markRows[0]["observed_at"] != nil {
			t.Fatalf("RED reproduction failed: legacy NOT (n)--() mark unexpectedly matched the true orphan (observed_at=%v) -- the historical predicate is supposed to be a no-op",
				markRows[0]["observed_at"])
		}
		t.Logf("confirmed: legacy NOT (n)--() mark did not match the true orphan (no-op), as expected on origin/main")

		if err := boltWriteStatement(ctx, runner, legacyNotDashDashOrphanSweepCypher, map[string]any{
			"cutoff_unix": nowUnix + 1,
			"limit":       1000,
		}); err != nil {
			t.Fatalf("legacy sweep statement errored: %v", err)
		}
		afterSweep, err := runner.runCypher(ctx, `MATCH (n:File {path: $path}) RETURN n.path AS key`,
			map[string]any{"path": orphanPath})
		if err != nil {
			t.Fatalf("read back orphan survival: %v", err)
		}
		if len(afterSweep) == 0 {
			t.Fatal("RED reproduction failed: legacy sweep unexpectedly deleted the true orphan")
		}
		t.Logf("confirmed: true orphan survives the legacy NOT (n)--() sweep -- origin/main leaks orphans forever")
	})

	// --- GREEN: the new anti-join store correctly detects and cleans up ---
	clock := time.Unix(1_800_000_000, 0).UTC()
	store := NewOrphanSweepStore(&boltTestExecutor{runner: runner}, &boltOrphanSweepReader{runner: runner})
	store.CountLimit = 1000
	store.Now = func() time.Time { return clock }

	policy := OrphanSweepPolicy{
		OrphanTTL:  1 * time.Second,
		BatchLimit: 100,
		CountLimit: 1000,
		Labels:     []string{"File"},
	}

	// Cycle 1: orphanPath and relinkedPath are unmarked orphans -> both get
	// marked. connectedPath/peerPath are connected -> untouched.
	result1, err := store.SweepOrphanNodes(ctx, policy)
	if err != nil {
		t.Fatalf("cycle 1 SweepOrphanNodes: %v", err)
	}
	t.Logf("cycle 1 result: counts=%v marked=%v deleted=%v skipped=%v",
		result1.Counts, result1.Marked, result1.Deleted, result1.Skipped)

	assertFileMarked(t, ctx, runner, orphanPath, true)
	assertFileMarked(t, ctx, runner, relinkedPath, true)
	assertFileMarked(t, ctx, runner, connectedPath, false)
	assertFileMarked(t, ctx, runner, peerPath, false)

	// Between cycles: relinkedPath gains a relationship (simulating a
	// concurrent projector reconnecting it) and the clock advances past TTL.
	mergeContains(relinkedPeerPath, relinkedPath)
	clock = clock.Add(2 * time.Second)

	// Cycle 2: relinkedPath is marked+connected -> clear. orphanPath is
	// marked+aged+still disconnected -> sweep (delete).
	result2, err := store.SweepOrphanNodes(ctx, policy)
	if err != nil {
		t.Fatalf("cycle 2 SweepOrphanNodes: %v", err)
	}
	t.Logf("cycle 2 result: counts=%v marked=%v deleted=%v skipped=%v",
		result2.Counts, result2.Marked, result2.Deleted, result2.Skipped)

	assertBoltCount(t, ctx, runner,
		`MATCH (n:File {path: $path}) RETURN count(n) AS count`,
		map[string]any{"path": orphanPath}, 0, "orphan deleted after aging")
	assertBoltCount(t, ctx, runner,
		`MATCH (n:File {path: $path}) RETURN count(n) AS count`,
		map[string]any{"path": connectedPath}, 1, "connected preserved")
	assertBoltCount(t, ctx, runner,
		`MATCH (n:File {path: $path}) RETURN count(n) AS count`,
		map[string]any{"path": peerPath}, 1, "peer preserved")
	assertBoltCount(t, ctx, runner,
		`MATCH (n:File {path: $path}) RETURN count(n) AS count`,
		map[string]any{"path": relinkedPath}, 1, "relinked node preserved")
	assertFileMarked(t, ctx, runner, relinkedPath, false)
}

func assertFileMarked(t *testing.T, ctx context.Context, runner *boltRetractTestRunner, path string, wantMarked bool) {
	t.Helper()
	rows, err := runner.runCypher(ctx, `MATCH (n:File {path: $path}) RETURN n.eshu_orphan_observed_at_unix AS observed_at`,
		map[string]any{"path": path})
	if err != nil {
		t.Fatalf("read marker for %s: %v", path, err)
	}
	if len(rows) == 0 {
		t.Fatalf("node %s missing", path)
	}
	marked := rows[0]["observed_at"] != nil
	if marked != wantMarked {
		t.Fatalf("%s marked = %v (observed_at=%v), want %v", path, marked, rows[0]["observed_at"], wantMarked)
	}
}
