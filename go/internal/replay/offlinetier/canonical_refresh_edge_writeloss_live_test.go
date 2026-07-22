// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package offlinetier_test

// canonical_refresh_edge_writeloss_live_test.go is the P0 follow-up regression
// for the two write-loss defects documented in
// docs/internal/evidence/5652-nornic-bare-match-writeloss.md Part 3 (the
// UNWIND-batched canonicalNodeRefreshCurrent*Cypher DELETE statements in
// go/internal/storage/cypher/canonical_node_cypher.go). It drives the REAL
// production CanonicalNodeWriter through the REAL NornicDB PhaseGroupExecutor
// (the same call site delta_tier_live_test.go uses), not a synthetic
// standalone Cypher harness, because the dispatch mode -- managed transaction
// vs auto-commit -- turned out to be the actual variable that determined
// whether these DELETE statements applied on the pinned v1.1.11 image (see
// docs/public/reference/nornicdb-query-pitfalls.md, "multiple DELETE
// statements sharing a single managed Bolt transaction do not all apply...
// Treat every retract DELETE as auto-commit-only"). Production's
// PhaseGroupExecutor.executeSequentialRetractPhase already routes every
// OperationCanonicalRetract statement through auto-commit Execute, never
// ExecuteGroup, so these tests establish whether the shipped Cypher text is
// actually reachable-broken through that real dispatch path.
//
// Skills active: golang-engineering, cypher-query-rigor,
// concurrency-deadlock-rigor.

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

const (
	refreshRepoID   = "p0-5652-followup-refresh"
	refreshRepoPath = "/repos/p0-5652-followup-refresh"
)

// cleanupRefreshScope removes every node this test file's scope can create.
func cleanupRefreshScope(ctx context.Context, t *testing.T, exec liveExecutor) {
	t.Helper()
	stmts := []cypher.Statement{
		{Cypher: `MATCH (f:File) WHERE f.repo_id = $repo_id DETACH DELETE f`, Parameters: map[string]any{"repo_id": refreshRepoID}},
		{Cypher: `MATCH (d:Directory) WHERE d.repo_id = $repo_id DETACH DELETE d`, Parameters: map[string]any{"repo_id": refreshRepoID}},
		{Cypher: `MATCH (r:Repository {id: $repo_id}) DETACH DELETE r`, Parameters: map[string]any{"repo_id": refreshRepoID}},
		{Cypher: `MATCH (m:Module {name: $name}) DETACH DELETE m`, Parameters: map[string]any{"name": "p0-5652-followup-module"}},
	}
	for _, stmt := range stmts {
		if err := exec.Execute(ctx, stmt); err != nil {
			t.Fatalf("cleanup %q: %v", stmt.Cypher, err)
		}
	}
}

func refreshBaseMaterialization(genID string, first bool) projector.CanonicalMaterialization {
	return projector.CanonicalMaterialization{
		ScopeID:         "git:repository:" + refreshRepoID,
		GenerationID:    genID,
		RepoID:          refreshRepoID,
		RepoPath:        refreshRepoPath,
		FirstGeneration: first,
		Repository: &projector.RepositoryRow{
			RepoID: refreshRepoID,
			Name:   refreshRepoID,
			Path:   refreshRepoPath,
		},
		Directories: []projector.DirectoryRow{
			{Path: refreshRepoPath + "/dir-a", Name: "dir-a", ParentPath: refreshRepoPath, RepoID: refreshRepoID, Depth: 0},
			{Path: refreshRepoPath + "/dir-b", Name: "dir-b", ParentPath: refreshRepoPath, RepoID: refreshRepoID, Depth: 0},
		},
	}
}

// TestRefreshFileImportEdgesGraphTruth proves
// canonicalNodeRefreshCurrentFileImportEdgesCypher's stale-IMPORTS-edge
// cleanup actually deletes the edge, through the real production writer and
// PhaseGroupExecutor: gen1 seeds a File that imports moduleA; gen2 keeps the
// same File but drops the import. The retract phase must remove the stale
// File-[:IMPORTS]->Module edge before Phase G would re-create any surviving
// import (there is none in gen2).
func TestRefreshFileImportEdgesGraphTruth(t *testing.T) {
	if !liveTierEnabled() {
		t.Skipf("set %s=1 to run the refresh-edge write-loss regression against a real NornicDB", liveTierEnv)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	exec, writer := openDeltaLiveBackend(ctx, t)
	cleanupRefreshScope(ctx, t, exec)
	t.Cleanup(func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanCancel()
		cleanupRefreshScope(cleanCtx, t, exec)
	})

	filePath := refreshRepoPath + "/dir-a/importer.go"

	gen1 := refreshBaseMaterialization("gen1", true)
	gen1.Files = []projector.FileRow{
		{Path: filePath, RelativePath: "dir-a/importer.go", Name: "importer.go", Language: "go", RepoID: refreshRepoID, DirPath: refreshRepoPath + "/dir-a"},
	}
	gen1.Modules = []projector.ModuleRow{{Name: "p0-5652-followup-module", Language: "go"}}
	gen1.Imports = []projector.ImportRow{{FilePath: filePath, ModuleName: "p0-5652-followup-module", ImportedName: "followupmod", LineNumber: 1}}

	if err := writer.Write(ctx, gen1); err != nil {
		t.Fatalf("write gen1: %v", err)
	}

	beforeCount, err := exec.count(ctx,
		`MATCH (f:File {path: $path})-[r:IMPORTS]->(:Module) RETURN count(r)`,
		map[string]any{"path": filePath})
	if err != nil {
		t.Fatalf("count IMPORTS before: %v", err)
	}
	if beforeCount != 1 {
		t.Fatalf("gen1 setup wrong: IMPORTS edge count = %d, want 1", beforeCount)
	}

	// gen2: same file, no imports.
	gen2 := refreshBaseMaterialization("gen2", false)
	gen2.Files = gen1.Files

	if err := writer.Write(ctx, gen2); err != nil {
		t.Fatalf("write gen2: %v", err)
	}

	afterCount, err := exec.count(ctx,
		`MATCH (f:File {path: $path})-[r:IMPORTS]->(:Module) RETURN count(r)`,
		map[string]any{"path": filePath})
	if err != nil {
		t.Fatalf("count IMPORTS after: %v", err)
	}
	t.Logf("IMPORTS edge count after gen2 (stale import dropped): %d (want 0)", afterCount)
	if afterCount != 0 {
		t.Fatalf("stale IMPORTS edge survived gen2 retract: count = %d, want 0 -- canonicalNodeRefreshCurrentFileImportEdgesCypher did not delete it", afterCount)
	}

	fileStillPresent, err := exec.count(ctx, `MATCH (f:File {path: $path}) RETURN count(f)`, map[string]any{"path": filePath})
	if err != nil {
		t.Fatalf("count File after: %v", err)
	}
	if fileStillPresent != 1 {
		t.Fatalf("File node must survive (only its stale IMPORTS edge should be removed), count = %d", fileStillPresent)
	}
}

// TestRefreshDirectoryFileEdgesGraphTruth proves
// canonicalNodeRefreshCurrentDirectoryFileEdgesCypher's stale
// Directory-[:CONTAINS]->File cleanup actually deletes the OLD containment
// edge when a File moves to a different Directory between generations,
// through the real production writer and PhaseGroupExecutor.
func TestRefreshDirectoryFileEdgesGraphTruth(t *testing.T) {
	if !liveTierEnabled() {
		t.Skipf("set %s=1 to run the refresh-edge write-loss regression against a real NornicDB", liveTierEnv)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	exec, writer := openDeltaLiveBackend(ctx, t)
	cleanupRefreshScope(ctx, t, exec)
	t.Cleanup(func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanCancel()
		cleanupRefreshScope(cleanCtx, t, exec)
	})

	movedPathGen1 := refreshRepoPath + "/dir-a/mover.go"

	gen1 := refreshBaseMaterialization("gen1", true)
	gen1.Files = []projector.FileRow{
		{Path: movedPathGen1, RelativePath: "dir-a/mover.go", Name: "mover.go", Language: "go", RepoID: refreshRepoID, DirPath: refreshRepoPath + "/dir-a"},
	}
	if err := writer.Write(ctx, gen1); err != nil {
		t.Fatalf("write gen1: %v", err)
	}

	beforeCount, err := exec.count(ctx,
		`MATCH (d:Directory {path: $dir})-[r:CONTAINS]->(f:File {path: $path}) RETURN count(r)`,
		map[string]any{"dir": refreshRepoPath + "/dir-a", "path": movedPathGen1})
	if err != nil {
		t.Fatalf("count CONTAINS before: %v", err)
	}
	if beforeCount != 1 {
		t.Fatalf("gen1 setup wrong: dir-a CONTAINS edge count = %d, want 1", beforeCount)
	}

	// gen2: SAME file path (File identity is keyed by path), but its
	// dir_path now points at dir-b -- simulates a file that was moved.
	gen2 := refreshBaseMaterialization("gen2", false)
	gen2.Files = []projector.FileRow{
		{Path: movedPathGen1, RelativePath: "dir-b/mover.go", Name: "mover.go", Language: "go", RepoID: refreshRepoID, DirPath: refreshRepoPath + "/dir-b"},
	}
	if err := writer.Write(ctx, gen2); err != nil {
		t.Fatalf("write gen2: %v", err)
	}

	oldEdgeCount, err := exec.count(ctx,
		`MATCH (d:Directory {path: $dir})-[r:CONTAINS]->(f:File {path: $path}) RETURN count(r)`,
		map[string]any{"dir": refreshRepoPath + "/dir-a", "path": movedPathGen1})
	if err != nil {
		t.Fatalf("count old CONTAINS after: %v", err)
	}
	t.Logf("old dir-a CONTAINS edge count after gen2 (file moved to dir-b): %d (want 0)", oldEdgeCount)
	if oldEdgeCount != 0 {
		t.Fatalf("stale dir-a CONTAINS edge survived gen2 retract: count = %d, want 0 -- canonicalNodeRefreshCurrentDirectoryFileEdgesCypher did not delete it", oldEdgeCount)
	}

	newEdgeCount, err := exec.count(ctx,
		`MATCH (d:Directory {path: $dir})-[r:CONTAINS]->(f:File {path: $path}) RETURN count(r)`,
		map[string]any{"dir": refreshRepoPath + "/dir-b", "path": movedPathGen1})
	if err != nil {
		t.Fatalf("count new CONTAINS after: %v", err)
	}
	if newEdgeCount != 1 {
		t.Fatalf("new dir-b CONTAINS edge missing after gen2: count = %d, want 1", newEdgeCount)
	}
}

// TestRefreshDirectoryParentEdgesGraphTruth proves
// canonicalNodeRefreshCurrentDirectoryParentEdgesCypher's stale
// Directory-[:CONTAINS]->Directory reparent cleanup deletes the OLD parent
// edge when a directory moves to a new parent between generations, through
// the real production writer and PhaseGroupExecutor. This is a dedicated,
// self-contained companion to the shared delta cassette's
// edge-parent-a/edge-parent-b scenario in delta_tier_live_test.go, isolated
// to this file's own repo scope so it can run independently.
func TestRefreshDirectoryParentEdgesGraphTruth(t *testing.T) {
	if !liveTierEnabled() {
		t.Skipf("set %s=1 to run the refresh-edge write-loss regression against a real NornicDB", liveTierEnv)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	exec, writer := openDeltaLiveBackend(ctx, t)
	cleanupRefreshScope(ctx, t, exec)
	t.Cleanup(func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanCancel()
		cleanupRefreshScope(cleanCtx, t, exec)
	})

	childPath := refreshRepoPath + "/dir-a/reparented-child"

	gen1 := refreshBaseMaterialization("gen1", true)
	gen1.Directories = append(gen1.Directories, projector.DirectoryRow{
		Path: childPath, Name: "reparented-child", ParentPath: refreshRepoPath + "/dir-a", RepoID: refreshRepoID, Depth: 1,
	})
	if err := writer.Write(ctx, gen1); err != nil {
		t.Fatalf("write gen1: %v", err)
	}

	beforeCount, err := exec.count(ctx,
		`MATCH (p:Directory {path: $parent})-[r:CONTAINS]->(d:Directory {path: $child}) RETURN count(r)`,
		map[string]any{"parent": refreshRepoPath + "/dir-a", "child": childPath})
	if err != nil {
		t.Fatalf("count old parent CONTAINS before: %v", err)
	}
	if beforeCount != 1 {
		t.Fatalf("gen1 setup wrong: dir-a -> child CONTAINS edge count = %d, want 1", beforeCount)
	}

	// gen2: same child directory, reparented under dir-b.
	gen2 := refreshBaseMaterialization("gen2", false)
	gen2.Directories = append(gen2.Directories, projector.DirectoryRow{
		Path: childPath, Name: "reparented-child", ParentPath: refreshRepoPath + "/dir-b", RepoID: refreshRepoID, Depth: 1,
	})
	if err := writer.Write(ctx, gen2); err != nil {
		t.Fatalf("write gen2: %v", err)
	}

	oldEdgeCount, err := exec.count(ctx,
		`MATCH (p:Directory {path: $parent})-[r:CONTAINS]->(d:Directory {path: $child}) RETURN count(r)`,
		map[string]any{"parent": refreshRepoPath + "/dir-a", "child": childPath})
	if err != nil {
		t.Fatalf("count old parent CONTAINS after: %v", err)
	}
	t.Logf("old dir-a -> child CONTAINS edge count after gen2 reparent: %d (want 0)", oldEdgeCount)
	if oldEdgeCount != 0 {
		t.Fatalf("stale dir-a -> child CONTAINS edge survived gen2 reparent retract: count = %d, want 0 -- canonicalNodeRefreshCurrentDirectoryParentEdgesCypher did not delete it", oldEdgeCount)
	}

	newEdgeCount, err := exec.count(ctx,
		`MATCH (p:Directory {path: $parent})-[r:CONTAINS]->(d:Directory {path: $child}) RETURN count(r)`,
		map[string]any{"parent": refreshRepoPath + "/dir-b", "child": childPath})
	if err != nil {
		t.Fatalf("count new parent CONTAINS after: %v", err)
	}
	if newEdgeCount != 1 {
		t.Fatalf("new dir-b -> child CONTAINS edge missing after gen2 reparent: count = %d, want 1", newEdgeCount)
	}
}
