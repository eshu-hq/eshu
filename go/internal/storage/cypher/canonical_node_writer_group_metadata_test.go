package cypher

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

func TestCanonicalNodeWriterAnnotatesAtomicGroupStatementsWithPhaseMetadata(t *testing.T) {
	t.Parallel()

	exec := &mockGroupExecutor{}
	writer := NewCanonicalNodeWriter(exec, 2, nil)

	err := writer.Write(context.Background(), projector.CanonicalMaterialization{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		RepoID:       "repo-1",
		RepoPath:     "/repo",
		Repository: &projector.RepositoryRow{
			RepoID: "repo-1",
			Name:   "repo",
			Path:   "/repo",
		},
		Directories: []projector.DirectoryRow{
			{Path: "/repo/src", Name: "src", ParentPath: "/repo", RepoID: "repo-1", Depth: 0},
		},
		Files: []projector.FileRow{
			{Path: "/repo/src/a.go", RelativePath: "src/a.go", Name: "a.go", Language: "go", RepoID: "repo-1", DirPath: "/repo/src"},
		},
		Entities: []projector.EntityRow{
			{
				EntityID:     "function-1",
				Label:        "Function",
				EntityName:   "run",
				FilePath:     "/repo/src/a.go",
				RelativePath: "src/a.go",
				StartLine:    1,
				EndLine:      2,
				Language:     "go",
				RepoID:       "repo-1",
			},
		},
	})
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	assertGroupedStatementPhase(t, exec.groupStmts, "MATCH (r:Repository {id: $repo_id})", "repository_cleanup")
	assertGroupedStatementPhase(t, exec.groupStmts, "MATCH (r:Repository {path: $path})", "repository_cleanup")
	assertGroupedStatementPhase(t, exec.groupStmts, "MERGE (r:Repository", "repository")
	assertGroupedStatementPhase(t, exec.groupStmts, "MERGE (d:Directory", "directories")
	assertGroupedStatementPhase(t, exec.groupStmts, "MATCH (f:File {path: row.path})", CanonicalPhaseFiles)
	assertGroupedStatementPhase(t, exec.groupStmts, "MERGE (f:File", CanonicalPhaseFiles)
	assertGroupedStatementPhase(t, exec.groupStmts, "MERGE (n:Function", CanonicalPhaseEntities)
}

func assertGroupedStatementPhase(t *testing.T, stmts []Statement, cypherFragment string, wantPhase string) {
	t.Helper()

	for _, stmt := range stmts {
		if !strings.Contains(stmt.Cypher, cypherFragment) {
			continue
		}
		if got := stmt.Parameters[StatementMetadataPhaseKey]; got != wantPhase {
			t.Fatalf("statement %q phase = %#v, want %#v", cypherFragment, got, wantPhase)
		}
		return
	}
	t.Fatalf("grouped statement containing %q not found", cypherFragment)
}
