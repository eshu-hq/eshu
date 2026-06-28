// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package offlinetier_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
	"github.com/eshu-hq/eshu/go/internal/replay/offlinetier"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// liveTierEnv gates the real-backend offline replay tier. The companion
// scripts/verify-replay-tier.sh sets it to 1 after the NornicDB container is
// healthy and the Bolt environment is exported.
const liveTierEnv = "ESHU_REPLAY_TIER_LIVE"

// cassetteRelPath is the committed nested-directory cassette, relative to this
// package directory (go/internal/replay/offlinetier).
var cassetteRelPath = filepath.Join("..", "..", "..", "..", "testdata", "cassettes", "replayoffline", "nested-directory-tree.json")

func liveTierEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(liveTierEnv))) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

// TestCassetteMaterializationMapsNestedTree is the offline, no-backend half of
// the tier. It proves the cassette -> CanonicalMaterialization seam produces the
// repository row and the depth-ordered directory rows the production writer
// consumes, so the live half is asserting against a correctly-built input rather
// than an accidentally-empty one. It runs in every default `go test` pass.
func TestCassetteMaterializationMapsNestedTree(t *testing.T) {
	t.Parallel()

	mat := loadCassetteMaterialization(t)

	if mat.Repository == nil {
		t.Fatal("materialization has no repository row")
	}
	if got, want := mat.Repository.RepoID, "replay-offline-nested-tree"; got != want {
		t.Fatalf("repo id = %q, want %q", got, want)
	}
	if got, want := len(mat.Directories), 3; got != want {
		t.Fatalf("directory rows = %d, want %d", got, want)
	}
	// Depth-ordered root-first so parents precede children.
	for i, dir := range mat.Directories {
		if dir.Depth != i {
			t.Fatalf("directory[%d] depth = %d, want %d (rows must be depth-ordered)", i, dir.Depth, i)
		}
	}
	// The depth-2 row is the #4019 trigger: its parent is itself a directory
	// MERGE'd in the same projection, not the repository.
	deepest := mat.Directories[2]
	if got, want := deepest.Path, "/repos/replay-offline-nested-tree/src/pkg/sub"; got != want {
		t.Fatalf("deepest dir path = %q, want %q", got, want)
	}
	if got, want := deepest.ParentPath, "/repos/replay-offline-nested-tree/src/pkg"; got != want {
		t.Fatalf("deepest dir parent = %q, want %q", got, want)
	}
}

// TestOfflineReplayTierGraphTruth is the real-backend half: cassette ->
// in-process canonical projector -> REAL NornicDB -> read back -> assert graph
// truth. It SKIPs cleanly unless ESHU_REPLAY_TIER_LIVE=1 and the Bolt
// environment is configured, so the default `go test` run never needs Docker.
// With a backend it fails on any mismatch and NEVER substitutes a fake graph.
func TestOfflineReplayTierGraphTruth(t *testing.T) {
	if !liveTierEnabled() {
		t.Skipf("set %s=1 (and ESHU_GRAPH_BACKEND/NEO4J_URI/ESHU_NEO4J_DATABASE) to run the offline replay tier against a real NornicDB; run scripts/verify-replay-tier.sh", liveTierEnv)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// REAL driver over the configured Bolt backend. No fake, no in-memory graph.
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

	// Apply the REAL schema for the configured backend.
	backend, err := runtimecfg.LoadGraphBackend(os.Getenv)
	if err != nil {
		t.Fatalf("load graph backend: %v", err)
	}
	if err := graph.EnsureSchemaWithBackendStrict(ctx, exec, nil, schemaBackend(backend)); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	mat := loadCassetteMaterialization(t)

	// Deterministic re-runs: clear any prior scope state before and after.
	cleanupScope(ctx, t, exec, mat.RepoID)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		cleanupScope(cleanupCtx, t, exec, mat.RepoID)
	})

	// Drive the REAL production canonical projection writer over the cassette,
	// through the NornicDB phase-group write path (per-phase transactions) so the
	// directory CONTAINS edges resolve exactly as they do in production.
	writer := cypher.NewCanonicalNodeWriter(livePhaseGroupExecutor{inner: exec}, 500, nil)
	if err := writer.Write(ctx, mat); err != nil {
		t.Fatalf("canonical node writer Write: %v", err)
	}

	assertGraphTruth(ctx, t, exec, mat.RepoID)
}

// loadCassetteMaterialization loads the committed cassette through the real
// cassette.Source and builds the canonical materialization for its single scope.
func loadCassetteMaterialization(t *testing.T) projector.CanonicalMaterialization {
	t.Helper()

	src, err := cassette.NewSource(cassetteRelPath)
	if err != nil {
		t.Fatalf("load cassette %s: %v", cassetteRelPath, err)
	}
	gen, ok, err := src.Next(context.Background())
	if err != nil {
		t.Fatalf("read cassette generation: %v", err)
	}
	if !ok {
		t.Fatal("cassette yielded no generation")
	}
	mat, err := offlinetier.MaterializationFromGeneration(gen)
	if err != nil {
		t.Fatalf("build materialization from cassette: %v", err)
	}
	return mat
}

// assertGraphTruth reads the projected graph back over Cypher and asserts the
// repository node, all three directory nodes, and the full CONTAINS chain
// (repository -> depth-0, depth-0 -> depth-1, depth-1 -> depth-2) exist. The
// depth-2 assertions are the #4019 regression: if the nested directory or its
// parent edge were dropped, these counts come back 0 and the tier fails.
func assertGraphTruth(ctx context.Context, t *testing.T, exec liveExecutor, repoID string) {
	t.Helper()

	const (
		root  = "/repos/replay-offline-nested-tree"
		src   = root + "/src"
		pkg   = src + "/pkg"
		sub   = pkg + "/sub"
		relTo = "CONTAINS"
	)

	repoCount, err := exec.count(ctx, `MATCH (r:Repository {id: $repo_id}) RETURN count(r)`, map[string]any{"repo_id": repoID})
	if err != nil {
		t.Fatalf("count repository: %v", err)
	}
	if repoCount != 1 {
		t.Fatalf("Repository read-back count = %d, want 1", repoCount)
	}

	for _, path := range []string{src, pkg, sub} {
		dirCount, err := exec.count(ctx, `MATCH (d:Directory {path: $path}) RETURN count(d)`, map[string]any{"path": path})
		if err != nil {
			t.Fatalf("count directory %s: %v", path, err)
		}
		if dirCount != 1 {
			t.Fatalf("Directory %q read-back count = %d, want 1 (nested-directory drop?)", path, dirCount)
		}
		t.Logf("directory node present path=%s count=%d", path, dirCount)
	}

	edges := []struct {
		name      string
		fromMatch string
		fromKey   string
		fromVal   string
		toPath    string
	}{
		{"repo->depth0", "Repository", "id", repoID, src},
		{"depth0->depth1", "Directory", "path", src, pkg},
		{"depth1->depth2", "Directory", "path", pkg, sub},
	}
	for _, edge := range edges {
		query := "MATCH (a:" + edge.fromMatch + " {" + edge.fromKey + ": $from})-[r:" + relTo + "]->(d:Directory {path: $to}) RETURN count(r)"
		edgeCount, err := exec.count(ctx, query, map[string]any{"from": edge.fromVal, "to": edge.toPath})
		if err != nil {
			t.Fatalf("count edge %s: %v", edge.name, err)
		}
		if edgeCount != 1 {
			t.Fatalf("CONTAINS edge %s read-back count = %d, want 1 (nested-directory edge drop?)", edge.name, edgeCount)
		}
		t.Logf("CONTAINS edge present %s count=%d", edge.name, edgeCount)
	}
}

// cleanupScope removes the cassette's repository, directories, and their edges so
// re-runs are deterministic. It deletes by repo identity (Repository id and the
// directory repo_id property) so it touches only this tier's nodes.
func cleanupScope(ctx context.Context, t *testing.T, exec liveExecutor, repoID string) {
	t.Helper()

	if err := exec.Execute(ctx, cypher.Statement{
		Cypher:     `MATCH (d:Directory {repo_id: $repo_id}) DETACH DELETE d`,
		Parameters: map[string]any{"repo_id": repoID},
	}); err != nil {
		t.Fatalf("cleanup directories: %v", err)
	}
	if err := exec.Execute(ctx, cypher.Statement{
		Cypher:     `MATCH (r:Repository {id: $repo_id}) DETACH DELETE r`,
		Parameters: map[string]any{"repo_id": repoID},
	}); err != nil {
		t.Fatalf("cleanup repository: %v", err)
	}
}

// schemaBackend maps the runtime graph backend to the schema DDL dialect.
func schemaBackend(backend runtimecfg.GraphBackend) graph.SchemaBackend {
	if backend == runtimecfg.GraphBackendNeo4j {
		return graph.SchemaBackendNeo4j
	}
	return graph.SchemaBackendNornicDB
}
