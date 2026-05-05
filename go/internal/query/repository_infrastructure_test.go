package query

import (
	"context"
	"strings"
	"testing"
)

func TestQueryRepoInfrastructureFiltersNonInfrastructureGraphRows(t *testing.T) {
	t.Parallel()

	reader := fakeRepoGraphReader{
		run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			if !strings.Contains(cypher, "MATCH (r:Repository {id: $repo_id})") {
				t.Fatalf("cypher = %q, want repository anchored infrastructure query", cypher)
			}
			return []map[string]any{
				{"type": "Function", "name": "handler", "file_path": "src/app.js"},
				{"type": "Variable", "name": "config", "file_path": "src/config.js"},
				{"type": "K8sResource", "name": "api", "kind": "Deployment", "file_path": "deploy/api.yaml"},
			}, nil
		},
	}

	got := queryRepoInfrastructureFromGraph(t.Context(), reader, map[string]any{"repo_id": "repo-1"})
	if len(got) != 1 {
		t.Fatalf("len(queryRepoInfrastructureFromGraph) = %d, want 1 infrastructure row: %#v", len(got), got)
	}
	if got, want := StringVal(got[0], "type"), "K8sResource"; got != want {
		t.Fatalf("type = %q, want %q", got, want)
	}
	if got, want := StringVal(got[0], "name"), "api"; got != want {
		t.Fatalf("name = %q, want %q", got, want)
	}
}

func TestQueryRepoInfrastructureUsesContentRowsBeforeGraph(t *testing.T) {
	t.Parallel()

	reader := fakeRepoGraphReader{
		run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			if strings.Contains(cypher, "MATCH (r:Repository {id: $repo_id})") {
				t.Fatalf("cypher = %q, want content infrastructure rows before graph fallback", cypher)
			}
			return nil, nil
		},
	}
	content := fakePortContentStore{
		entities: []EntityContent{
			{
				EntityType:   "K8sResource",
				EntityName:   "api",
				RelativePath: "deploy/api.yaml",
				Metadata: map[string]any{
					"kind": "Deployment",
				},
			},
		},
	}

	got := queryRepoInfrastructureRows(
		t.Context(),
		reader,
		content,
		map[string]any{"repo_id": "repo-1"},
	)
	if len(got) != 1 {
		t.Fatalf("len(queryRepoInfrastructureRows) = %d, want 1 content row: %#v", len(got), got)
	}
	if got, want := StringVal(got[0], "type"), "K8sResource"; got != want {
		t.Fatalf("type = %q, want %q", got, want)
	}
	if got, want := StringVal(got[0], "kind"), "Deployment"; got != want {
		t.Fatalf("kind = %q, want %q", got, want)
	}
}
