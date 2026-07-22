// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package offlinetier_test

// canonical_file_edge_writeloss_live_test.go is the P0 follow-up regression
// for the File-edge write-loss defect documented in
// docs/internal/evidence/5652-nornic-bare-match-writeloss.md Part 2:
// canonicalNodeFileUpdateExistingCypher's WITH-chained REPO_CONTAINS/CONTAINS
// MERGE clauses (after a preceding node SET) silently no-op on the pinned
// v1.1.11 image. Unlike the Refresh/retract DELETE statements (Defect 2,
// which route through PhaseGroupExecutor.executeSequentialRetractPhase's
// auto-commit Execute and turned out to already work in production), File
// writes carry OperationCanonicalUpsert and route through
// executeGroupedChunksWithDrain -> ge.ExecuteGroup, a REAL managed Bolt
// transaction -- the dispatch mode the WITH-chained clause-drop bug actually
// needs. This test drives the real production CanonicalNodeWriter through
// that real dispatch path (openDeltaLiveBackend, the same call site
// delta_tier_live_test.go uses) to prove the defect reproduces there before
// any fix, and to re-prove the fix afterward.
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
	fileEdgeRepoID   = "p0-5652-followup-file-edges"
	fileEdgeRepoPath = "/repos/p0-5652-followup-file-edges"
)

func fileEdgeCleanup(ctx context.Context, t *testing.T, exec liveExecutor) {
	t.Helper()
	stmts := []string{
		`MATCH (f:File) WHERE f.repo_id = $repo_id DETACH DELETE f`,
		`MATCH (d:Directory) WHERE d.repo_id = $repo_id DETACH DELETE d`,
		`MATCH (r:Repository {id: $repo_id}) DETACH DELETE r`,
	}
	for _, c := range stmts {
		if err := exec.Execute(ctx, cypher.Statement{Cypher: c, Parameters: map[string]any{"repo_id": fileEdgeRepoID}}); err != nil {
			t.Fatalf("cleanup %q: %v", c, err)
		}
	}
}

// TestFileUpdateExistingEdgesGraphTruth_ExistingFile proves that updating an
// ALREADY-EXISTING File (gen1 creates it, gen2 updates its generation_id)
// still carries live REPO_CONTAINS and CONTAINS edges after gen2, through the
// real production writer.Write() and PhaseGroupExecutor dispatch. This is the
// "true update" case the evidence doc's live proof covered (a File seeded
// before the statement under test ever runs).
func TestFileUpdateExistingEdgesGraphTruth_ExistingFile(t *testing.T) {
	if !liveTierEnabled() {
		t.Skipf("set %s=1 to run the File-edge write-loss regression against a real NornicDB", liveTierEnv)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	exec, writer := openDeltaLiveBackend(ctx, t)
	fileEdgeCleanup(ctx, t, exec)
	t.Cleanup(func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanCancel()
		fileEdgeCleanup(cleanCtx, t, exec)
	})

	filePath := fileEdgeRepoPath + "/dir-a/existing.go"

	gen1 := projector.CanonicalMaterialization{
		ScopeID:         "git:repository:" + fileEdgeRepoID,
		GenerationID:    "gen1",
		RepoID:          fileEdgeRepoID,
		RepoPath:        fileEdgeRepoPath,
		FirstGeneration: true,
		Repository:      &projector.RepositoryRow{RepoID: fileEdgeRepoID, Name: fileEdgeRepoID, Path: fileEdgeRepoPath},
		Directories: []projector.DirectoryRow{
			{Path: fileEdgeRepoPath + "/dir-a", Name: "dir-a", ParentPath: fileEdgeRepoPath, RepoID: fileEdgeRepoID, Depth: 0},
		},
		Files: []projector.FileRow{
			{Path: filePath, RelativePath: "dir-a/existing.go", Name: "existing.go", Language: "go", RepoID: fileEdgeRepoID, DirPath: fileEdgeRepoPath + "/dir-a"},
		},
	}
	if err := writer.Write(ctx, gen1); err != nil {
		t.Fatalf("write gen1: %v", err)
	}

	assertFileEdgeCounts(ctx, t, exec, filePath, fileEdgeRepoPath+"/dir-a", "gen1 baseline")

	// gen2: same file, same path -- non-first-generation update.
	gen2 := gen1
	gen2.GenerationID = "gen2"
	gen2.FirstGeneration = false
	if err := writer.Write(ctx, gen2); err != nil {
		t.Fatalf("write gen2: %v", err)
	}

	assertFileEdgeCounts(ctx, t, exec, filePath, fileEdgeRepoPath+"/dir-a", "gen2 update-existing")

	genRows, err := exec.Run(ctx, `MATCH (f:File {path: $path}) RETURN f.generation_id AS generation_id`, map[string]any{"path": filePath})
	if err != nil {
		t.Fatalf("read generation_id: %v", err)
	}
	if len(genRows) == 0 || genRows[0]["generation_id"] != "gen2" {
		t.Fatalf("File.generation_id after gen2 = %v, want gen2", genRows)
	}
}

// TestFileUpdateExistingEdgesGraphTruth_BrandNewFile proves that a File which
// did NOT exist in gen1 (appears for the first time in a non-first-generation
// gen2 batch alongside an already-existing file) still gets its node AND
// edges created correctly in ONE managed-transaction phase-group write. This
// is the untested corner case the evidence doc's live proof did not cover
// (its fixture pre-seeded the File before running the statement): it decides
// whether folding canonicalNodeFileCreateMissingCypher's job into a
// MERGE-anchored canonicalNodeFileUpdateExistingCypher split is safe, or
// whether CreateMissing must be kept for files with no prior existence.
func TestFileUpdateExistingEdgesGraphTruth_BrandNewFile(t *testing.T) {
	if !liveTierEnabled() {
		t.Skipf("set %s=1 to run the File-edge write-loss regression against a real NornicDB", liveTierEnv)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	exec, writer := openDeltaLiveBackend(ctx, t)
	fileEdgeCleanup(ctx, t, exec)
	t.Cleanup(func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanCancel()
		fileEdgeCleanup(cleanCtx, t, exec)
	})

	existingPath := fileEdgeRepoPath + "/dir-a/existing.go"
	newPath := fileEdgeRepoPath + "/dir-a/brand-new.go"

	gen1 := projector.CanonicalMaterialization{
		ScopeID:         "git:repository:" + fileEdgeRepoID,
		GenerationID:    "gen1",
		RepoID:          fileEdgeRepoID,
		RepoPath:        fileEdgeRepoPath,
		FirstGeneration: true,
		Repository:      &projector.RepositoryRow{RepoID: fileEdgeRepoID, Name: fileEdgeRepoID, Path: fileEdgeRepoPath},
		Directories: []projector.DirectoryRow{
			{Path: fileEdgeRepoPath + "/dir-a", Name: "dir-a", ParentPath: fileEdgeRepoPath, RepoID: fileEdgeRepoID, Depth: 0},
		},
		Files: []projector.FileRow{
			{Path: existingPath, RelativePath: "dir-a/existing.go", Name: "existing.go", Language: "go", RepoID: fileEdgeRepoID, DirPath: fileEdgeRepoPath + "/dir-a"},
		},
	}
	if err := writer.Write(ctx, gen1); err != nil {
		t.Fatalf("write gen1: %v", err)
	}

	// gen2: NOT first-generation, batch contains the existing file (update)
	// PLUS a brand-new file that never existed before, in the SAME batch --
	// so both rows travel through the SAME UpdateExisting/CreateMissing
	// statement pair within the SAME phase-group ExecuteGroup transaction.
	gen2 := projector.CanonicalMaterialization{
		ScopeID:         gen1.ScopeID,
		GenerationID:    "gen2",
		RepoID:          fileEdgeRepoID,
		RepoPath:        fileEdgeRepoPath,
		FirstGeneration: false,
		Repository:      gen1.Repository,
		Directories:     gen1.Directories,
		Files: []projector.FileRow{
			{Path: existingPath, RelativePath: "dir-a/existing.go", Name: "existing.go", Language: "go", RepoID: fileEdgeRepoID, DirPath: fileEdgeRepoPath + "/dir-a"},
			{Path: newPath, RelativePath: "dir-a/brand-new.go", Name: "brand-new.go", Language: "go", RepoID: fileEdgeRepoID, DirPath: fileEdgeRepoPath + "/dir-a"},
		},
	}
	if err := writer.Write(ctx, gen2); err != nil {
		t.Fatalf("write gen2 (brand-new file in same batch as update): %v", err)
	}

	assertFileEdgeCounts(ctx, t, exec, existingPath, fileEdgeRepoPath+"/dir-a", "gen2 existing file")
	assertFileEdgeCounts(ctx, t, exec, newPath, fileEdgeRepoPath+"/dir-a", "gen2 brand-new file")
}

// assertFileEdgeCounts asserts a File node exists with exactly one
// REPO_CONTAINS edge from the repository and one CONTAINS edge from its
// directory.
func assertFileEdgeCounts(ctx context.Context, t *testing.T, exec liveExecutor, filePath, dirPath, label string) {
	t.Helper()

	nodeCount, err := exec.count(ctx, `MATCH (f:File {path: $path}) RETURN count(f)`, map[string]any{"path": filePath})
	if err != nil {
		t.Fatalf("%s: count File: %v", label, err)
	}
	if nodeCount != 1 {
		t.Fatalf("%s: File node count = %d, want 1", label, nodeCount)
	}

	repoEdgeCount, err := exec.count(ctx,
		`MATCH (r:Repository)-[rel:REPO_CONTAINS]->(f:File {path: $path}) RETURN count(rel)`,
		map[string]any{"path": filePath})
	if err != nil {
		t.Fatalf("%s: count REPO_CONTAINS: %v", label, err)
	}
	t.Logf("%s: REPO_CONTAINS edge count for %q = %d (want 1)", label, filePath, repoEdgeCount)
	if repoEdgeCount != 1 {
		t.Fatalf("%s: REPO_CONTAINS edge count = %d, want 1 -- File UpdateExisting/CreateMissing did not create/keep the repo edge", label, repoEdgeCount)
	}

	dirEdgeCount, err := exec.count(ctx,
		`MATCH (d:Directory {path: $dir})-[rel:CONTAINS]->(f:File {path: $path}) RETURN count(rel)`,
		map[string]any{"dir": dirPath, "path": filePath})
	if err != nil {
		t.Fatalf("%s: count directory CONTAINS: %v", label, err)
	}
	t.Logf("%s: directory CONTAINS edge count for %q = %d (want 1)", label, filePath, dirEdgeCount)
	if dirEdgeCount != 1 {
		t.Fatalf("%s: directory CONTAINS edge count = %d, want 1 -- File UpdateExisting/CreateMissing did not create/keep the directory edge", label, dirEdgeCount)
	}
}
