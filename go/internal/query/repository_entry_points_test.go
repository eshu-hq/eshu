package query

import (
	"context"
	"database/sql/driver"
	"strings"
	"testing"
)

func TestContentReaderRepositoryEntryPointsQueriesKnownFunctionNames(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{"entity_name", "relative_path", "language"},
			rows: [][]driver.Value{
				{"main", "cmd/server/main.go", "go"},
				{"handler", "src/lambda.ts", "typescript"},
			},
		},
	})

	reader := NewContentReader(db)
	got, err := reader.repositoryEntryPoints(t.Context(), "repo-1")
	if err != nil {
		t.Fatalf("repositoryEntryPoints() error = %v, want nil", err)
	}
	if !got.Available {
		t.Fatal("repositoryEntryPoints().Available = false, want true")
	}
	if len(got.Rows) != 2 {
		t.Fatalf("len(Rows) = %d, want 2", len(got.Rows))
	}
	if got, want := StringVal(got.Rows[0], "name"), "main"; got != want {
		t.Fatalf("Rows[0].name = %q, want %q", got, want)
	}
}

func TestQueryRepoEntryPointsUsesContentRowsBeforeGraph(t *testing.T) {
	t.Parallel()

	reader := fakeRepoGraphReader{
		run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			if strings.Contains(cypher, "MATCH (r:Repository {id: $repo_id})") {
				t.Fatalf("cypher = %q, want content entry-point rows before graph fallback", cypher)
			}
			return nil, nil
		},
	}
	content := fakePortContentStore{
		entryPoints: repositoryEntryPointReadModel{
			Available: true,
			Rows: []map[string]any{
				{"name": "main", "relative_path": "cmd/server/main.go", "language": "go"},
			},
		},
	}

	got := queryRepoEntryPoints(
		t.Context(),
		reader,
		content,
		map[string]any{"repo_id": "repo-1"},
	)
	if len(got) != 1 {
		t.Fatalf("len(queryRepoEntryPoints) = %d, want 1 content function row: %#v", len(got), got)
	}
	if got, want := StringVal(got[0], "name"), "main"; got != want {
		t.Fatalf("name = %q, want %q", got, want)
	}
	if got, want := StringVal(got[0], "relative_path"), "cmd/server/main.go"; got != want {
		t.Fatalf("relative_path = %q, want %q", got, want)
	}
}

func TestQueryRepoEntryPointsFiltersNonEntrypointGraphRows(t *testing.T) {
	t.Parallel()

	reader := fakeRepoGraphReader{
		run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
			return []map[string]any{
				{"name": "_callRoute", "relative_path": "test/routes.lab.js", "language": "javascript"},
				{"name": "main", "relative_path": "cmd/server/main.go", "language": "go"},
			}, nil
		},
	}

	got := queryRepoEntryPoints(t.Context(), reader, nil, map[string]any{"repo_id": "repo-1"})
	if len(got) != 1 {
		t.Fatalf("len(queryRepoEntryPoints) = %d, want 1 graph entry-point row: %#v", len(got), got)
	}
	if got, want := StringVal(got[0], "name"), "main"; got != want {
		t.Fatalf("name = %q, want %q", got, want)
	}
}
