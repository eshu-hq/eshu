// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package offlinetier_test

// delta_tier_live_test.go contains the real-backend (ESHU_REPLAY_TIER_LIVE=1)
// tests for R-17 delta/multi-generation/tombstone correctness. These tests drive
// the production canonical writer against a real NornicDB, proving retraction,
// supersession, and idempotency on the actual graph backend. They skip cleanly
// when the environment gate is unset so the default `go test` pass needs no Docker.

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/replay/offlinetier"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// TestDeltaTombstoneGraphTruth is the real-backend half of R-17. It:
//  1. Writes gen1 (alpha, beta, gamma) via the production canonical writer.
//  2. Writes gen2 (alpha, beta, delta; gamma tombstoned) with FirstGeneration=false.
//  3. Reads back and asserts:
//     - gamma is GONE (count=0): retraction worked.
//     - alpha, beta, delta are PRESENT (count=1 each).
//     - edge-child survives while its old edge-parent-a CONTAINS edge is gone
//     and its new edge-parent-b CONTAINS edge is present.
//     - repo name == "replay-delta-tombstone-v2" (supersession).
//  4. Replays gen2 a second time (idempotency): graph is unchanged.
//
// SKIPs cleanly unless ESHU_REPLAY_TIER_LIVE=1 and Bolt env is configured.
func TestDeltaTombstoneGraphTruth(t *testing.T) {
	if !liveTierEnabled() {
		t.Skipf("set %s=1 (and ESHU_GRAPH_BACKEND/NEO4J_URI/ESHU_NEO4J_DATABASE) to run the delta/tombstone tier against a real NornicDB", liveTierEnv)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	exec, writer := openDeltaLiveBackend(ctx, t)

	cleanupDeltaScope(ctx, t, exec)
	t.Cleanup(func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanCancel()
		cleanupDeltaScope(cleanCtx, t, exec)
	})

	// Write gen1.
	src := loadDeltaCassette(t)
	gen1, ok, err := src.Next(context.Background())
	if err != nil || !ok {
		t.Fatalf("read gen1: err=%v ok=%v", err, ok)
	}
	gen2, ok, err := src.Next(context.Background())
	if err != nil || !ok {
		t.Fatalf("read gen2: err=%v ok=%v", err, ok)
	}

	// One call drains gen1 and gen2 exactly once; dm.Gen1 is the baseline
	// materialization to write first (re-deriving it would drain gen1's already
	// closed fact channel a second time and yield an empty generation).
	dm, err := offlinetier.DeltaMaterializationFromGenerations(gen1, gen2)
	if err != nil {
		t.Fatalf("DeltaMaterializationFromGenerations: %v", err)
	}

	if err := writer.Write(ctx, dm.Gen1); err != nil {
		t.Fatalf("write gen1: %v", err)
	}

	// Verify gen1 state: gamma and its incoming CONTAINS edge must be present
	// before the delta write.
	assertDeltaDirCount(ctx, t, exec, deltaRepoPath+"/gamma", 1, "gen1 pre-delta: gamma present")
	assertDeltaIncomingContainsCount(
		ctx, t, exec, deltaRepoPath+"/gamma", 1,
		"gen1 pre-delta: gamma CONTAINS edge present",
	)
	assertDeltaDirectoryContainsEdgeCount(
		ctx, t, exec,
		deltaRepoPath+"/edge-parent-a",
		deltaRepoPath+"/edge-parent-a/edge-child",
		1,
		"gen1 pre-delta: old edge-parent-a CONTAINS edge present",
	)
	assertDeltaDirectoryContainsEdgeCount(
		ctx, t, exec,
		deltaRepoPath+"/edge-parent-b",
		deltaRepoPath+"/edge-parent-a/edge-child",
		0,
		"gen1 pre-delta: new edge-parent-b CONTAINS edge absent",
	)
	assertDeltaGitlabNeedsEdgeCount(
		ctx, t, exec,
		deltaGitlabTestJobUID,
		deltaGitlabBuildJobUID,
		1,
		"gen1 pre-delta: test job NEEDS build",
	)
	assertDeltaGitlabNeedsEdgeCount(
		ctx, t, exec,
		deltaGitlabTestJobUID,
		deltaGitlabDeployJobUID,
		0,
		"gen1 pre-delta: test job does not NEED deploy yet",
	)

	// Write gen2 (retraction enabled: FirstGeneration=false).
	if err := writer.Write(ctx, dm.Gen2); err != nil {
		t.Fatalf("write gen2: %v", err)
	}

	assertDeltaGraphTruth(ctx, t, exec, dm)

	// --- Idempotency: replay gen2 a second time ---
	src2 := loadDeltaCassette(t)
	gen1b, _, _ := src2.Next(context.Background())
	gen2b, _, _ := src2.Next(context.Background())
	dm2, err := offlinetier.DeltaMaterializationFromGenerations(gen1b, gen2b)
	if err != nil {
		t.Fatalf("DeltaMaterializationFromGenerations (idempotency): %v", err)
	}
	if err := writer.Write(ctx, dm2.Gen2); err != nil {
		t.Fatalf("write gen2 second time (idempotency): %v", err)
	}
	assertDeltaGraphTruth(ctx, t, exec, dm2)
	t.Log("idempotency: second gen2 write produced identical graph truth")
}

// TestDeltaTombstoneNegativeControlBrokenRetraction is the negative control that
// proves the gate has TEETH. It writes gen2 with FirstGeneration=true (the
// broken-retraction class: retract phase is suppressed) and asserts gamma is
// STILL present — the held-pending-retract bug class (#3859). It then proves a
// correct gen2 (FirstGeneration=false) DOES remove gamma.
func TestDeltaTombstoneNegativeControlBrokenRetraction(t *testing.T) {
	if !liveTierEnabled() {
		t.Skipf("set %s=1 to run the broken-retraction negative control against a real NornicDB", liveTierEnv)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	exec, writer := openDeltaLiveBackend(ctx, t)

	cleanupDeltaScope(ctx, t, exec)
	t.Cleanup(func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanCancel()
		cleanupDeltaScope(cleanCtx, t, exec)
	})

	// Write gen1 (FirstGeneration=true, all three dirs present).
	src := loadDeltaCassette(t)
	gen1, ok, err := src.Next(context.Background())
	if err != nil || !ok {
		t.Fatalf("read gen1: err=%v ok=%v", err, ok)
	}
	gen2, ok, err := src.Next(context.Background())
	if err != nil || !ok {
		t.Fatalf("read gen2: err=%v ok=%v", err, ok)
	}

	// Single drain of gen1/gen2; write the baseline via dm.Gen1.
	dm, err := offlinetier.DeltaMaterializationFromGenerations(gen1, gen2)
	if err != nil {
		t.Fatalf("DeltaMaterializationFromGenerations: %v", err)
	}
	if err := writer.Write(ctx, dm.Gen1); err != nil {
		t.Fatalf("write gen1: %v", err)
	}

	// BROKEN retraction: force FirstGeneration=true on gen2. This suppresses the
	// retract phase — exactly the #3859 held-pending-retract class.
	brokenGen2 := dm.Gen2
	brokenGen2.FirstGeneration = true // DELIBERATELY BROKEN

	if err := writer.Write(ctx, brokenGen2); err != nil {
		t.Fatalf("write broken gen2: %v", err)
	}

	// Negative control: with broken retraction gamma must still be present.
	gammaAfterBroken, err := exec.count(
		ctx,
		`MATCH (d:Directory {path: $path}) RETURN count(d)`,
		map[string]any{"path": deltaRepoPath + "/gamma"},
	)
	if err != nil {
		t.Fatalf("count gamma after broken retraction: %v", err)
	}
	if gammaAfterBroken == 0 {
		t.Fatal("negative control broken: gamma removed with FirstGeneration=true — retraction should be suppressed but is not")
	}
	t.Logf("negative control confirmed: gamma count=%d after broken-retraction (node not removed)", gammaAfterBroken)

	// Prove correct gen2 (FirstGeneration=false) DOES remove gamma.
	cleanupDeltaScope(ctx, t, exec)

	src2 := loadDeltaCassette(t)
	gen1c, _, _ := src2.Next(context.Background())
	gen2c, _, _ := src2.Next(context.Background())

	dmC, err := offlinetier.DeltaMaterializationFromGenerations(gen1c, gen2c)
	if err != nil {
		t.Fatalf("DeltaMaterializationFromGenerations (re-run): %v", err)
	}
	if err := writer.Write(ctx, dmC.Gen1); err != nil {
		t.Fatalf("write gen1 (re-run): %v", err)
	}
	if err := writer.Write(ctx, dmC.Gen2); err != nil {
		t.Fatalf("write correct gen2 (re-run): %v", err)
	}

	gammaAfterCorrect, err := exec.count(
		ctx,
		`MATCH (d:Directory {path: $path}) RETURN count(d)`,
		map[string]any{"path": deltaRepoPath + "/gamma"},
	)
	if err != nil {
		t.Fatalf("count gamma after correct retraction: %v", err)
	}
	if gammaAfterCorrect != 0 {
		t.Fatalf("correct gen2 left gamma in graph (count=%d) — retraction broken", gammaAfterCorrect)
	}
	t.Logf("negative control verified: correct gen2 removed gamma (count=%d)", gammaAfterCorrect)
}

// --- live-backend helpers for delta tier ---

// openDeltaLiveBackend opens the real NornicDB driver, applies the schema, and
// returns the liveExecutor and CanonicalNodeWriter wired to the phase-group path.
func openDeltaLiveBackend(ctx context.Context, t *testing.T) (liveExecutor, *cypher.CanonicalNodeWriter) {
	t.Helper()

	driver, cfg, err := runtimecfg.OpenNeo4jDriver(ctx, os.Getenv)
	if err != nil {
		t.Fatalf("open Bolt driver: %v", err)
	}
	t.Cleanup(func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer closeCancel()
		_ = driver.Close(closeCtx)
	})

	exec := liveExecutor{driver: driver, database: cfg.DatabaseName}

	backend, err := runtimecfg.LoadGraphBackend(os.Getenv)
	if err != nil {
		t.Fatalf("load graph backend: %v", err)
	}
	if err := graph.EnsureSchemaWithBackendStrict(ctx, exec, nil, schemaBackend(backend)); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	writer := cypher.NewCanonicalNodeWriter(livePhaseGroupExecutor{inner: exec}, 500, nil)
	return exec, writer
}

// assertDeltaGraphTruth reads back the graph after a gen2 delta write and asserts:
//   - tombstoned directories are GONE (count=0).
//   - tombstoned directories have no incoming CONTAINS edge (count=0).
//   - surviving directories are PRESENT (count=1 each).
//   - moved edge-child keeps its Directory node, loses the old parent edge, and
//     gains the new parent edge.
//   - repository name == "replay-delta-tombstone-v2" (supersession).
func assertDeltaGraphTruth(ctx context.Context, t *testing.T, exec liveExecutor, dm offlinetier.DeltaMaterialization) {
	t.Helper()

	for _, tombstonedPath := range dm.TombstonedDirectoryPaths {
		assertDeltaDirCount(ctx, t, exec, tombstonedPath, 0, "tombstoned directory must be absent after gen2 write")
		assertDeltaIncomingContainsCount(
			ctx, t, exec, tombstonedPath, 0,
			"tombstoned directory CONTAINS edge must be absent after gen2 write",
		)
	}
	assertNoAnonymousDeltaDirectoryShells(ctx, t, exec)
	for _, d := range dm.Gen2.Directories {
		assertDeltaDirCount(ctx, t, exec, d.Path, 1, "surviving directory must be present after gen2 write")
	}
	assertDeltaDirectoryContainsEdgeCount(
		ctx, t, exec,
		deltaRepoPath+"/edge-parent-a",
		deltaRepoPath+"/edge-parent-a/edge-child",
		0,
		"old edge-parent-a CONTAINS edge must be absent after gen2 write",
	)
	assertDeltaDirectoryContainsEdgeCount(
		ctx, t, exec,
		deltaRepoPath+"/edge-parent-b",
		deltaRepoPath+"/edge-parent-a/edge-child",
		1,
		"new edge-parent-b CONTAINS edge must be present after gen2 write",
	)
	assertDeltaGitlabNeedsEdgeCount(
		ctx, t, exec,
		deltaGitlabTestJobUID,
		deltaGitlabBuildJobUID,
		0,
		"old test->build NEEDS edge must be absent after gen2 write",
	)
	assertDeltaGitlabNeedsEdgeCount(
		ctx, t, exec,
		deltaGitlabTestJobUID,
		deltaGitlabDeployJobUID,
		1,
		"new test->deploy NEEDS edge must be present after gen2 write",
	)

	repoNameCount, err := exec.count(
		ctx,
		`MATCH (r:Repository {id: $repo_id, name: $name}) RETURN count(r)`,
		map[string]any{"repo_id": deltaRepoID, "name": "replay-delta-tombstone-v2"},
	)
	if err != nil {
		t.Fatalf("count repository with gen2 name: %v", err)
	}
	if repoNameCount != 1 {
		t.Fatalf("supersession: repo with gen2 name count = %d, want 1", repoNameCount)
	}
	t.Log("supersession: repository name updated to replay-delta-tombstone-v2")
}

// assertDeltaDirectoryContainsEdgeCount reads back a Directory -> Directory
// CONTAINS edge by stable endpoint paths and fails if it does not match want.
func assertDeltaDirectoryContainsEdgeCount(
	ctx context.Context,
	t *testing.T,
	exec liveExecutor,
	parentPath string,
	childPath string,
	want int64,
	msg string,
) {
	t.Helper()
	var count int64
	var err error
	for attempt := 0; attempt < 20; attempt++ {
		count, err = exec.count(
			ctx,
			`MATCH (p:Directory {path: $parent_path})-[r:CONTAINS]->(d:Directory {path: $child_path}) RETURN count(r)`,
			map[string]any{"parent_path": parentPath, "child_path": childPath},
		)
		if err != nil {
			t.Fatalf("count CONTAINS edge %q -> %q: %v", parentPath, childPath, err)
		}
		if count == want || want != 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if count != want {
		t.Fatalf("%s: CONTAINS edge %q -> %q count = %d, want %d", msg, parentPath, childPath, count, want)
	}
	t.Logf("CONTAINS edge %q -> %q count=%d (want %d) — %s", parentPath, childPath, count, want, msg)
}

// assertDeltaDirCount reads back the directory node count for path and fails if
// it does not match want.
func assertDeltaDirCount(ctx context.Context, t *testing.T, exec liveExecutor, path string, want int64, msg string) {
	t.Helper()
	var count int64
	var err error
	for attempt := 0; attempt < 20; attempt++ {
		count, err = exec.count(
			ctx,
			`MATCH (d:Directory {path: $path}) RETURN count(d)`,
			map[string]any{"path": path},
		)
		if err != nil {
			t.Fatalf("count directory %q: %v", path, err)
		}
		if count == want || want != 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if count != want {
		matchingCanonical, _ := exec.count(
			ctx,
			`MATCH (d:Directory {path: $path})
WHERE d.repo_id = $repo_id AND d.evidence_source = 'projector/canonical'
RETURN count(d)`,
			map[string]any{"path": path, "repo_id": deltaRepoID},
		)
		incoming, _ := exec.count(
			ctx,
			`MATCH ()-[:CONTAINS]->(d:Directory {path: $path}) RETURN count(d)`,
			map[string]any{"path": path},
		)
		outgoing, _ := exec.count(
			ctx,
			`MATCH (d:Directory {path: $path})-[:CONTAINS]->() RETURN count(d)`,
			map[string]any{"path": path},
		)
		t.Fatalf(
			"%s: directory %q count = %d, want %d (canonical_matches=%d incoming_contains=%d outgoing_contains=%d)",
			msg,
			path,
			count,
			want,
			matchingCanonical,
			incoming,
			outgoing,
		)
	}
	t.Logf("directory %q count=%d (want %d) — %s", path, count, want, msg)
}

// assertDeltaIncomingContainsCount reads back incoming CONTAINS edges for a
// directory path and fails if it does not match want.
func assertDeltaIncomingContainsCount(ctx context.Context, t *testing.T, exec liveExecutor, path string, want int64, msg string) {
	t.Helper()
	count, err := exec.count(
		ctx,
		`MATCH ()-[r:CONTAINS]->(d:Directory {path: $path}) RETURN count(r)`,
		map[string]any{"path": path},
	)
	if err != nil {
		t.Fatalf("count incoming CONTAINS edge for %q: %v", path, err)
	}
	if count != want {
		t.Fatalf("%s: incoming CONTAINS edge for %q count = %d, want %d", msg, path, count, want)
	}
	t.Logf("incoming CONTAINS edge for %q count=%d (want %d) — %s", path, count, want, msg)
}

// assertNoAnonymousDeltaDirectoryShells proves tombstoned directories are
// deleted, not hidden by clearing identity properties.
func assertNoAnonymousDeltaDirectoryShells(ctx context.Context, t *testing.T, exec liveExecutor) {
	t.Helper()
	count, err := exec.count(
		ctx,
		`MATCH (d:Directory)
WHERE d.repo_id IS NULL
RETURN count(d)`,
		nil,
	)
	if err != nil {
		t.Fatalf("count anonymous Directory shells: %v", err)
	}
	if count != 0 {
		t.Fatalf("anonymous Directory shell count = %d, want 0", count)
	}
	t.Log("anonymous Directory shell count=0")
}

// cleanupDeltaScope removes all nodes for the delta tombstone scenario so
// re-runs are deterministic.
func cleanupDeltaScope(ctx context.Context, t *testing.T, exec liveExecutor) {
	t.Helper()
	if err := exec.Execute(ctx, cypher.Statement{
		Cypher:     `MATCH (j:GitlabJob {repo_id: $repo_id}) DETACH DELETE j`,
		Parameters: map[string]any{"repo_id": deltaRepoID},
	}); err != nil {
		t.Fatalf("cleanup delta GitlabJob nodes: %v", err)
	}
	if err := exec.Execute(ctx, cypher.Statement{
		Cypher:     `MATCH (p:GitlabPipeline {repo_id: $repo_id}) DETACH DELETE p`,
		Parameters: map[string]any{"repo_id": deltaRepoID},
	}); err != nil {
		t.Fatalf("cleanup delta GitlabPipeline nodes: %v", err)
	}
	if err := exec.Execute(ctx, cypher.Statement{
		Cypher:     `MATCH (d:Directory {repo_id: $repo_id}) DETACH DELETE d`,
		Parameters: map[string]any{"repo_id": deltaRepoID},
	}); err != nil {
		t.Fatalf("cleanup delta directories: %v", err)
	}
	if err := exec.Execute(ctx, cypher.Statement{
		Cypher:     `MATCH (r:Repository {id: $repo_id}) DETACH DELETE r`,
		Parameters: map[string]any{"repo_id": deltaRepoID},
	}); err != nil {
		t.Fatalf("cleanup delta repository: %v", err)
	}
}
