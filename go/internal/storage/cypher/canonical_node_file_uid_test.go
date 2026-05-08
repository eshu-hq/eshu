package cypher

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

func TestCanonicalNodeWriterFileRowsUseRepoScopedUID(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(nil, 500, nil)
	statements := writer.buildFileStatements(projector.CanonicalMaterialization{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Files: []projector.FileRow{{
			Path:         "/repo/service-entry.ts",
			RelativePath: "service-entry.ts",
			Name:         "service-entry.ts",
			Language:     "typescript",
			RepoID:       "repo-js",
			DirPath:      "/repo",
		}},
	})
	if len(statements) == 0 {
		t.Fatal("buildFileStatements() returned no statements")
	}
	rows, ok := statements[0].Parameters["rows"].([]map[string]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("rows = %#v, want one file row", statements[0].Parameters["rows"])
	}
	if got, want := rows[0]["uid"], "repo-js:service-entry.ts"; got != want {
		t.Fatalf("file uid = %#v, want %#v", got, want)
	}
}

func TestCanonicalNodeWriterRootFileRowsDoNotRequireDirectoryMatch(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(nil, 500, nil)
	statements := writer.buildFileStatements(projector.CanonicalMaterialization{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Files: []projector.FileRow{{
			Path:         "/repo/service-entry.ts",
			RelativePath: "service-entry.ts",
			Name:         "service-entry.ts",
			Language:     "typescript",
			RepoID:       "repo-js",
			DirPath:      "/repo",
		}},
	})
	if got, want := len(statements), 2; got != want {
		t.Fatalf("root file statement count = %d, want %d", got, want)
	}
	for _, statement := range statements {
		if strings.Contains(statement.Cypher, "MATCH (d:Directory") {
			t.Fatalf("root file statement should not require Directory match: %s", statement.Cypher)
		}
		if !strings.Contains(statement.Cypher, "MERGE (r)-[repoRel:REPO_CONTAINS]->(f)") {
			t.Fatalf("root file statement missing Repository containment: %s", statement.Cypher)
		}
	}
}
