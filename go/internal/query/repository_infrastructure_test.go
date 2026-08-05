// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
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

	got, truncated, err := queryRepoInfrastructureFromGraph(t.Context(), reader, map[string]any{"repo_id": "repo-1"})
	if err != nil {
		t.Fatalf("queryRepoInfrastructureFromGraph() error = %v, want nil", err)
	}
	if truncated {
		t.Fatal("truncated = true, want false for a row count under the limit")
	}
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

// TestQueryRepoInfrastructureFromGraphBoundsRowsWithNamedLimit is the LIMIT
// contract guard for #5764's new bound on this call site: the Cypher must
// carry a literal "LIMIT $limit" suffix and params["limit"] must equal
// repositoryInfrastructureEntityLimit+1 (P3 review follow-up: requesting one
// extra row over the disclosed limit is what lets the caller detect
// truncation exactly, via len(rows) > repositoryInfrastructureEntityLimit,
// instead of the ambiguous len(rows) == limit check), not a copy-pasted or
// drifted literal. Deleting either the "LIMIT $limit" clause or the
// queryParams["limit"] assignment in production must fail this test -- no
// other existing test in this package asserts the Cypher text or the params
// value for this specific bound.
func TestQueryRepoInfrastructureFromGraphBoundsRowsWithNamedLimit(t *testing.T) {
	t.Parallel()

	var sawLimitClause bool
	var sawLimitParam any
	reader := fakeRepoGraphReader{
		run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
			sawLimitClause = strings.Contains(cypher, "LIMIT $limit")
			sawLimitParam = params["limit"]
			return nil, nil
		},
	}

	if _, _, err := queryRepoInfrastructureFromGraph(t.Context(), reader, map[string]any{"repo_id": "repo-1"}); err != nil {
		t.Fatalf("queryRepoInfrastructureFromGraph() error = %v, want nil", err)
	}
	if !sawLimitClause {
		t.Fatal("cypher missing \"LIMIT $limit\" clause")
	}
	if sawLimitParam != repositoryInfrastructureEntityLimit+1 {
		t.Fatalf("params[limit] = %#v, want %d (repositoryInfrastructureEntityLimit+1)", sawLimitParam, repositoryInfrastructureEntityLimit+1)
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

	got, truncated, err := queryRepoInfrastructureRows(
		t.Context(),
		reader,
		content,
		map[string]any{"repo_id": "repo-1"},
	)
	if err != nil {
		t.Fatalf("queryRepoInfrastructureRows() error = %v, want nil", err)
	}
	if truncated {
		t.Fatal("truncated = true, want false: the content path's own silent truncation is not signaled here")
	}
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

// TestQueryRepoInfrastructureFromGraphSignalsTruncationAtLimit is the P2-2/P3
// regression: a HEALTHY graph read that returns MORE than
// repositoryInfrastructureEntityLimit rows must report truncated=true, and
// cap the result to the limit, so a caller can disclose that more
// infrastructure may exist past the bound, rather than looking identical to
// "every entity is present" the way it did before this fix. The fake below
// returns exactly params["limit"] rows (repositoryInfrastructureEntityLimit+1,
// mirroring the production LIMIT clause honored by a real backend), proving
// the exact-detection property the limit+1 request buys: a mutant reverting
// to the ambiguous len(rows) == repositoryInfrastructureEntityLimit check
// would see 5001 rows and wrongly report truncated=false.
func TestQueryRepoInfrastructureFromGraphSignalsTruncationAtLimit(t *testing.T) {
	t.Parallel()

	reader := fakeRepoGraphReader{
		run: func(_ context.Context, _ string, params map[string]any) ([]map[string]any, error) {
			limit := IntVal(params, "limit")
			rows := make([]map[string]any, limit)
			for i := range rows {
				rows[i] = map[string]any{"type": "K8sResource", "name": fmt.Sprintf("res-%d", i)}
			}
			return rows, nil
		},
	}

	got, truncated, err := queryRepoInfrastructureFromGraph(t.Context(), reader, map[string]any{"repo_id": "repo-1"})
	if err != nil {
		t.Fatalf("queryRepoInfrastructureFromGraph() error = %v, want nil", err)
	}
	if !truncated {
		t.Fatal("truncated = false, want true when the row count exceeds repositoryInfrastructureEntityLimit")
	}
	if len(got) != repositoryInfrastructureEntityLimit {
		t.Fatalf("len(got) = %d, want %d (capped)", len(got), repositoryInfrastructureEntityLimit)
	}
}

// TestQueryRepoInfrastructureFromGraphNoTruncationAtLimit is the negative
// companion: a healthy read that returns EXACTLY
// repositoryInfrastructureEntityLimit rows -- the backend had no more to
// give -- must not report truncation. This is the boundary case the old
// len(rows) == limit check got backwards.
func TestQueryRepoInfrastructureFromGraphNoTruncationAtLimit(t *testing.T) {
	t.Parallel()

	reader := fakeRepoGraphReader{
		run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
			rows := make([]map[string]any, repositoryInfrastructureEntityLimit)
			for i := range rows {
				rows[i] = map[string]any{"type": "K8sResource", "name": fmt.Sprintf("res-%d", i)}
			}
			return rows, nil
		},
	}

	got, truncated, err := queryRepoInfrastructureFromGraph(t.Context(), reader, map[string]any{"repo_id": "repo-1"})
	if err != nil {
		t.Fatalf("queryRepoInfrastructureFromGraph() error = %v, want nil", err)
	}
	if truncated {
		t.Fatal("truncated = true, want false when the row count exactly equals repositoryInfrastructureEntityLimit")
	}
	if len(got) != repositoryInfrastructureEntityLimit {
		t.Fatalf("len(got) = %d, want %d", len(got), repositoryInfrastructureEntityLimit)
	}
}
