package cypher

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

func TestCanonicalNodeWriterReplacesRepositoryConflictsBeforeIDMerge(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&mockExecutor{}, 500, nil)
	mat := projector.CanonicalMaterialization{
		Repository: &projector.RepositoryRow{
			RepoID:    "repository:r_new",
			Name:      "service",
			Path:      "/repos/service",
			LocalPath: "/repos/service",
		},
	}
	cleanup := writer.buildRepositoryCleanupStatements(mat)
	upserts := writer.buildRepositoryStatements(mat)

	if len(cleanup) != 2 {
		t.Fatalf("repository cleanup statements = %d, want 2", len(cleanup))
	}
	if !strings.Contains(cleanup[0].Cypher, "MATCH (r:Repository {id: $repo_id})") {
		t.Fatalf("repository cleanup statement = %q, want id cleanup first", cleanup[0].Cypher)
	}
	if !strings.Contains(cleanup[0].Cypher, "DETACH DELETE r") {
		t.Fatalf("repository cleanup statement = %q, want stale id delete", cleanup[0].Cypher)
	}
	if !strings.Contains(cleanup[1].Cypher, "MATCH (r:Repository {path: $path})") {
		t.Fatalf("repository cleanup statement = %q, want path cleanup second", cleanup[1].Cypher)
	}
	if !strings.Contains(cleanup[1].Cypher, "DETACH DELETE r") {
		t.Fatalf("repository cleanup statement = %q, want stale path delete", cleanup[1].Cypher)
	}
	if len(upserts) != 1 {
		t.Fatalf("repository upsert statements = %d, want 1", len(upserts))
	}
	if !strings.Contains(upserts[0].Cypher, "MERGE (r:Repository {id: $repo_id})") {
		t.Fatalf("repository upsert statement = %q, want id MERGE", upserts[0].Cypher)
	}
}

func TestCanonicalNodeWriterSkipsRepositoryCleanupForFirstGeneration(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&mockExecutor{}, 500, nil)
	mat := projector.CanonicalMaterialization{
		FirstGeneration: true,
		Repository: &projector.RepositoryRow{
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

	err := writer.Write(context.Background(), projector.CanonicalMaterialization{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		RepoID:       "repository:r_new",
		RepoPath:     "/repos/service",
		Repository: &projector.RepositoryRow{
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

	err := writer.Write(context.Background(), projector.CanonicalMaterialization{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		RepoID:       "repository:r_new",
		RepoPath:     "/repos/service",
		Repository: &projector.RepositoryRow{
			RepoID:    "repository:r_new",
			Name:      "service",
			Path:      "/repos/service",
			LocalPath: "/repos/service",
		},
		Directories: []projector.DirectoryRow{
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
