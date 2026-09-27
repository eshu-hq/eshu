// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
)

// TestCanonicalNodeWriterRepositoryCleanupRetiresOnlyPathConflicts is the
// hermetic #7285 regression. A non-first, non-delta projection (every retry,
// because projector/service.go forces PreviousGenerationExists on attempt >= 2)
// must never DETACH DELETE the Repository node it is about to re-MERGE: that
// delete destroyed every reducer-owned edge on the node (DEFINES,
// EXPOSES_ENDPOINT, DEPLOYMENT_SOURCE, typed repository relationships, ...)
// and nothing re-arms the reducers that wrote them. Only the path_conflict
// retirement of a DIFFERENT-id Repository at the same path may remain.
func TestCanonicalNodeWriterRepositoryCleanupRetiresOnlyPathConflicts(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&mockExecutor{}, 500, nil)
	mat := canonical.CanonicalMaterialization{
		Repository: &canonical.RepositoryRow{
			RepoID:    "repository:r_new",
			Name:      "service",
			Path:      "/repos/service",
			LocalPath: "/repos/service",
		},
	}
	cleanup := writer.buildRepositoryCleanupStatements(mat)
	upserts := writer.buildRepositoryStatements(mat)

	if len(cleanup) != 1 {
		t.Fatalf("repository cleanup statements = %d, want 1 (path_conflict only)", len(cleanup))
	}
	if !strings.Contains(cleanup[0].Cypher, "MATCH (r:Repository {path: $path})") ||
		!strings.Contains(cleanup[0].Cypher, "WHERE r.id <> $repo_id") {
		t.Fatalf("repository cleanup statement = %q, want the path_conflict retirement", cleanup[0].Cypher)
	}
	if got := cleanup[0].Parameters["repo_id"]; got != "repository:r_new" {
		t.Fatalf("path_conflict repo_id = %#v, want repository:r_new", got)
	}
	if got := cleanup[0].Parameters["path"]; got != "/repos/service" {
		t.Fatalf("path_conflict path = %#v, want /repos/service", got)
	}
	if got := cleanup[0].Parameters[StatementMetadataSummaryKey]; got != "repository_cleanup lookup=path_conflict" {
		t.Fatalf("path_conflict summary = %#v", got)
	}
	for _, stmt := range cleanup {
		if repositoryIDDetachDelete(stmt.Cypher) {
			t.Fatalf("repository cleanup deletes the Repository by id: %q", stmt.Cypher)
		}
	}
	if len(upserts) != 1 {
		t.Fatalf("repository upsert statements = %d, want 1", len(upserts))
	}
	if !strings.Contains(upserts[0].Cypher, "MERGE (r:Repository {id: $repo_id})") {
		t.Fatalf("repository upsert statement = %q, want id MERGE", upserts[0].Cypher)
	}
}

// TestCanonicalNodeWriterRetryWriteNeverDeletesRepositoryByID drives the whole
// Write on the retry shape (FirstGeneration=false, DeltaProjection=false) and
// asserts that no statement in any phase deletes the Repository node by id,
// while the repository upsert and the path_conflict retirement still run.
func TestCanonicalNodeWriterRetryWriteNeverDeletesRepositoryByID(t *testing.T) {
	t.Parallel()

	exec := &mockExecutor{}
	writer := NewCanonicalNodeWriter(exec, 500, nil)
	err := writer.Write(context.Background(), canonical.CanonicalMaterialization{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		RepoID:       "repository:r_new",
		RepoPath:     "/repos/service",
		Repository: &canonical.RepositoryRow{
			RepoID: "repository:r_new", Name: "service", Path: "/repos/service", LocalPath: "/repos/service",
		},
		Files: []canonical.FileRow{{
			Path: "/repos/service/main.go", RelativePath: "main.go", Name: "main.go",
			Language: "go", RepoID: "repository:r_new",
		}},
	})
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	var sawUpsert, sawPathConflict bool
	for _, stmt := range exec.calls {
		if repositoryIDDetachDelete(stmt.Cypher) {
			t.Fatalf("retry Write deleted the Repository by id: %q", stmt.Cypher)
		}
		if strings.Contains(stmt.Cypher, "MERGE (r:Repository {id: $repo_id})") {
			sawUpsert = true
		}
		if strings.Contains(stmt.Cypher, "MATCH (r:Repository {path: $path})") {
			sawPathConflict = true
		}
	}
	if !sawUpsert || !sawPathConflict {
		t.Fatalf("retry Write upsert=%t path_conflict=%t, want both", sawUpsert, sawPathConflict)
	}
}

// repositoryIDDetachDelete reports whether cypherText deletes a Repository
// node that it anchored by id. The whitespace is normalized so a reflowed
// statement cannot hide the shape.
func repositoryIDDetachDelete(cypherText string) bool {
	normalized := strings.Join(strings.Fields(cypherText), " ")
	return strings.Contains(normalized, "MATCH (r:Repository {id: $repo_id}) DETACH DELETE r")
}

func TestCanonicalNodeWriterSkipsRepositoryCleanupForFirstGeneration(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&mockExecutor{}, 500, nil)
	mat := canonical.CanonicalMaterialization{
		FirstGeneration: true,
		Repository: &canonical.RepositoryRow{
			RepoID:    "repository:r_new",
			Name:      "service",
			Path:      "/repos/service",
			LocalPath: "/repos/service",
		},
	}

	if cleanup := writer.buildRepositoryCleanupStatements(mat); len(cleanup) != 0 {
		t.Fatalf("repository cleanup statements = %d, want 0 for first generation", len(cleanup))
	}
}

func TestCanonicalNodeWriterCommitsRepositoryPathCleanupBeforeRepositoryUpsert(t *testing.T) {
	t.Parallel()

	exec := &mockPhaseGroupExecutor{}
	writer := NewCanonicalNodeWriter(exec, 500, nil)

	err := writer.Write(context.Background(), canonical.CanonicalMaterialization{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		RepoID:       "repository:r_new",
		RepoPath:     "/repos/service",
		Repository: &canonical.RepositoryRow{
			RepoID:    "repository:r_new",
			Name:      "service",
			Path:      "/repos/service",
			LocalPath: "/repos/service",
		},
	})
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	cleanupGroup := -1
	repositoryGroup := -1
	for i, group := range exec.phaseGroups {
		for _, stmt := range group {
			switch {
			case strings.Contains(stmt.Cypher, "MATCH (r:Repository {path: $path})"):
				cleanupGroup = i
			case strings.Contains(stmt.Cypher, "MERGE (r:Repository {id: $repo_id})"):
				repositoryGroup = i
			}
		}
	}
	if cleanupGroup < 0 {
		t.Fatal("missing repository path cleanup phase group")
	}
	if repositoryGroup < 0 {
		t.Fatal("missing repository upsert phase group")
	}
	if cleanupGroup >= repositoryGroup {
		t.Fatalf("repository cleanup phase group = %d, repository upsert group = %d; cleanup must commit first",
			cleanupGroup, repositoryGroup)
	}
}

func TestCanonicalNodeWriterWritesDirectoriesAfterRepositoryUpsert(t *testing.T) {
	t.Parallel()

	exec := &mockPhaseGroupExecutor{}
	writer := NewCanonicalNodeWriter(exec, 500, nil)

	err := writer.Write(context.Background(), canonical.CanonicalMaterialization{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		RepoID:       "repository:r_new",
		RepoPath:     "/repos/service",
		Repository: &canonical.RepositoryRow{
			RepoID:    "repository:r_new",
			Name:      "service",
			Path:      "/repos/service",
			LocalPath: "/repos/service",
		},
		Directories: []canonical.DirectoryRow{
			{Path: "/repos/service/schema/data-plane", Name: "data-plane", ParentPath: "/repos/service", RepoID: "repository:r_new", Depth: 0},
		},
	})
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	repositoryGroup := -1
	directoryGroup := -1
	for i, group := range exec.phaseGroups {
		for _, stmt := range group {
			switch {
			case strings.Contains(stmt.Cypher, "MERGE (r:Repository {id: $repo_id})"):
				repositoryGroup = i
			case strings.Contains(stmt.Cypher, "MERGE (d:Directory {path: row.path})"):
				directoryGroup = i
			}
		}
	}
	if repositoryGroup < 0 {
		t.Fatal("missing repository upsert phase group")
	}
	if directoryGroup < 0 {
		t.Fatal("missing directory upsert phase group")
	}
	if repositoryGroup >= directoryGroup {
		t.Fatalf("repository upsert phase group = %d, directory group = %d; repository must commit first",
			repositoryGroup, directoryGroup)
	}
}
