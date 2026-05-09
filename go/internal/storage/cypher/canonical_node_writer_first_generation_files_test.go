package cypher

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

func TestCanonicalNodeWriterFirstGenerationFilesUseSingleIdempotentMerge(t *testing.T) {
	t.Parallel()

	exec := &mockExecutor{}
	writer := NewCanonicalNodeWriter(exec, 500, nil).WithFileBatchSize(2)

	err := writer.Write(context.Background(), projector.CanonicalMaterialization{
		ScopeID:         "scope-1",
		GenerationID:    "gen-1",
		RepoID:          "repo-1",
		RepoPath:        "/repos/service",
		FirstGeneration: true,
		Repository: &projector.RepositoryRow{
			RepoID: "repo-1",
			Name:   "service",
			Path:   "/repos/service",
		},
		Files: []projector.FileRow{
			{
				Path:         "/repos/service/src/a.go",
				RelativePath: "src/a.go",
				Name:         "a.go",
				Language:     "go",
				RepoID:       "repo-1",
				DirPath:      "/repos/service/src",
			},
			{
				Path:         "/repos/service/src/b.go",
				RelativePath: "src/b.go",
				Name:         "b.go",
				Language:     "go",
				RepoID:       "repo-1",
				DirPath:      "/repos/service/src",
			},
		},
	})
	if err != nil {
		t.Fatalf("Write() error = %v, want nil", err)
	}

	fileStatements := canonicalTestStatementsByPhase(exec.calls, CanonicalPhaseFiles)
	if got, want := len(fileStatements), 1; got != want {
		t.Fatalf("file statement count = %d, want %d", got, want)
	}
	fileCypher := fileStatements[0].Cypher
	if !strings.Contains(fileCypher, "MERGE (f:File {path: row.path})") {
		t.Fatalf("first-generation file cypher must MERGE File by path, got:\n%s", fileCypher)
	}
	if strings.Contains(fileCypher, "WHERE NOT EXISTS") {
		t.Fatalf("first-generation file cypher must avoid existence subquery, got:\n%s", fileCypher)
	}
	if strings.Contains(fileCypher, "MATCH (f:File {path: row.path})") {
		t.Fatalf("first-generation file cypher must avoid separate update pass, got:\n%s", fileCypher)
	}
}

func TestCanonicalNodeWriterPriorGenerationFilesKeepGuardedCreate(t *testing.T) {
	t.Parallel()

	exec := &mockExecutor{}
	writer := NewCanonicalNodeWriter(exec, 500, nil).WithFileBatchSize(2)

	err := writer.Write(context.Background(), projector.CanonicalMaterialization{
		ScopeID:         "scope-1",
		GenerationID:    "gen-2",
		RepoID:          "repo-1",
		RepoPath:        "/repos/service",
		FirstGeneration: false,
		Repository: &projector.RepositoryRow{
			RepoID: "repo-1",
			Name:   "service",
			Path:   "/repos/service",
		},
		Files: []projector.FileRow{
			{
				Path:         "/repos/service/src/a.go",
				RelativePath: "src/a.go",
				Name:         "a.go",
				Language:     "go",
				RepoID:       "repo-1",
				DirPath:      "/repos/service/src",
			},
		},
	})
	if err != nil {
		t.Fatalf("Write() error = %v, want nil", err)
	}

	fileStatements := canonicalTestStatementsByPhase(exec.calls, CanonicalPhaseFiles)
	if got, want := len(fileStatements), 2; got != want {
		t.Fatalf("file statement count = %d, want %d", got, want)
	}
	if !strings.Contains(fileStatements[0].Cypher, "MATCH (f:File {path: row.path})") {
		t.Fatalf("prior-generation first file statement must update existing files, got:\n%s", fileStatements[0].Cypher)
	}
	if !strings.Contains(fileStatements[1].Cypher, "WHERE NOT EXISTS") {
		t.Fatalf("prior-generation second file statement must keep guarded create, got:\n%s", fileStatements[1].Cypher)
	}
}

func canonicalTestStatementsByPhase(stmts []Statement, phase string) []Statement {
	filtered := make([]Statement, 0)
	for _, stmt := range stmts {
		if stmt.Parameters[StatementMetadataPhaseKey] == phase {
			filtered = append(filtered, stmt)
		}
	}
	return filtered
}
