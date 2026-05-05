package reducer

import (
	"context"
	"strings"
	"testing"
)

func TestGraphInfrastructurePlatformLookupListsProvisionedPlatforms(t *testing.T) {
	t.Parallel()

	graph := &stubGraphQueryRunner{
		rows: []map[string]any{
			{
				"repo_id":           "repo-infra",
				"platform_id":       "platform:ecs:aws:cluster/runtime-main:none:none",
				"platform_name":     "runtime-main",
				"platform_kind":     "ecs",
				"platform_provider": "aws",
				"platform_locator":  "cluster/runtime-main",
			},
			{
				"repo_id":       "repo-skip",
				"platform_name": "missing-id",
				"platform_kind": "ecs",
			},
		},
	}

	lookup := GraphInfrastructurePlatformLookup{Graph: graph}
	result, err := lookup.ListProvisionedPlatforms(context.Background(), []string{"repo-infra", "repo-skip"})
	if err != nil {
		t.Fatalf("ListProvisionedPlatforms() error = %v", err)
	}

	if !strings.Contains(graph.cypher, "PROVISIONS_PLATFORM") {
		t.Fatalf("query missing PROVISIONS_PLATFORM: %s", graph.cypher)
	}
	if got, want := len(graph.calls), 2; got != want {
		t.Fatalf("graph query calls = %d, want %d", got, want)
	}
	if got, want := graph.calls[0].params["repo_id"], "repo-infra"; got != want {
		t.Fatalf("first repo_id param = %v, want %v", got, want)
	}
	if got, want := graph.calls[1].params["repo_id"], "repo-skip"; got != want {
		t.Fatalf("second repo_id param = %v, want %v", got, want)
	}
	rows := result["repo-infra"]
	if len(rows) != 1 {
		t.Fatalf("len(result[repo-infra]) = %d, want 1", len(rows))
	}
	if got, want := rows[0].PlatformKind, "ecs"; got != want {
		t.Fatalf("PlatformKind = %q, want %q", got, want)
	}
	if got := len(result["repo-skip"]); got != 0 {
		t.Fatalf("len(result[repo-skip]) = %d, want 0 for incomplete platform row", got)
	}
}

type stubGraphQueryRunner struct {
	cypher string
	params map[string]any
	calls  []stubGraphQueryCall
	rows   []map[string]any
	err    error
}

type stubGraphQueryCall struct {
	cypher string
	params map[string]any
}

func (f *stubGraphQueryRunner) Run(
	_ context.Context,
	cypher string,
	params map[string]any,
) ([]map[string]any, error) {
	f.cypher = cypher
	f.params = params
	f.calls = append(f.calls, stubGraphQueryCall{cypher: cypher, params: params})
	if f.err != nil {
		return nil, f.err
	}
	repoID := anyToString(params["repo_id"])
	rows := make([]map[string]any, 0, len(f.rows))
	for _, row := range f.rows {
		if anyToString(row["repo_id"]) == repoID {
			rows = append(rows, row)
		}
	}
	return rows, nil
}
