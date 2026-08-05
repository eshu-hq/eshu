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
		t.Fatal("truncated = true, want false for an entity count under the limit")
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

// limitCapturingContentStore records the exact limit argument
// queryRepoInfrastructureFromContent passes to ListRepoEntitiesByTypes, so a
// test can assert on the probe itself rather than only on its downstream
// effect.
type limitCapturingContentStore struct {
	fakePortContentStore
	capturedLimit int
	entities      []EntityContent
}

func (s *limitCapturingContentStore) ListRepoEntitiesByTypes(_ context.Context, _ string, _ []string, limit int) ([]EntityContent, error) {
	s.capturedLimit = limit
	return s.entities, nil
}

// TestQueryRepoInfrastructureFromContentProbesOneRowPastTheLimit is a PR #5933
// review follow-up (P1-1): the downstream infrastructureOverflowContentStore
// double (context_story_limits_test.go) deliberately ignores the limit
// argument ListRepoEntitiesByTypes receives, so the len(entities) >
// repositoryInfrastructureEntityLimit comparison it exercises stays honest
// about what the STORE returned. But that same design leaves the PROBE itself
// -- the +1 that makes overflow detectable in the first place -- with no
// coverage: reverting repositoryInfrastructureEntityLimit+1 back to
// repositoryInfrastructureEntityLimit in repository_infrastructure.go
// reintroduces the exact silent-cap bug this fix exists to close, and every
// other test in this package still passes, because none of them observe the
// requested limit, only the returned truncated bool. This test closes that
// gap directly: it captures the limit argument production code sends and
// pins it to repositoryInfrastructureEntityLimit+1.
func TestQueryRepoInfrastructureFromContentProbesOneRowPastTheLimit(t *testing.T) {
	t.Parallel()

	spy := &limitCapturingContentStore{
		entities: []EntityContent{
			{EntityType: "K8sResource", EntityName: "api", RelativePath: "deploy/api.yaml"},
		},
	}

	if _, truncated := queryRepoInfrastructureFromContent(t.Context(), spy, "repo-1"); truncated {
		t.Fatalf("truncated = true, want false for a single content row well under repositoryInfrastructureEntityLimit")
	}

	if got, want := spy.capturedLimit, repositoryInfrastructureEntityLimit+1; got != want {
		t.Fatalf(
			"ListRepoEntitiesByTypes limit = %d, want %d (the +1 probe is what lets an exactly-at-bound repository be told apart from an overflowing one; a plain repositoryInfrastructureEntityLimit silently drops the overflow with no signal)",
			got, want,
		)
	}
}

// TestQueryRepoInfrastructureFromContentSignalsTruncationAtLimit is the P2-3
// follow-up to #5764: on a normally-wired deployment the content read model is
// tried first (TestQueryRepoInfrastructureUsesContentRowsBeforeGraph above),
// so a truncation signal that fired only on the graph fallback would never
// disclose the common case where content itself clipped at
// repositoryInfrastructureEntityLimit. A HEALTHY content read that returns
// MORE than repositoryInfrastructureEntityLimit entities must report
// truncated=true and cap the result, mirroring
// TestQueryRepoInfrastructureFromGraphSignalsTruncationAtLimit's graph-path
// proof.
func TestQueryRepoInfrastructureFromContentSignalsTruncationAtLimit(t *testing.T) {
	t.Parallel()

	reader := fakeRepoGraphReader{
		run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
			t.Fatal("graph read called, want content rows to satisfy the read without a graph fallback")
			return nil, nil
		},
	}
	entities := make([]EntityContent, repositoryInfrastructureEntityLimit+1)
	for i := range entities {
		entities[i] = EntityContent{
			EntityType:   "K8sResource",
			EntityName:   fmt.Sprintf("res-%d", i),
			RelativePath: fmt.Sprintf("deploy/res-%d.yaml", i),
		}
	}
	content := fakePortContentStore{entities: entities}

	got, truncated, err := queryRepoInfrastructureRows(
		t.Context(),
		reader,
		content,
		map[string]any{"repo_id": "repo-1"},
	)
	if err != nil {
		t.Fatalf("queryRepoInfrastructureRows() error = %v, want nil", err)
	}
	if !truncated {
		t.Fatal("truncated = false, want true when the content entity count exceeds repositoryInfrastructureEntityLimit")
	}
	if len(got) != repositoryInfrastructureEntityLimit {
		t.Fatalf("len(got) = %d, want %d (capped)", len(got), repositoryInfrastructureEntityLimit)
	}
}

// TestQueryRepoInfrastructureFromContentNoTruncationAtLimit is the negative
// companion: a healthy content read that returns EXACTLY
// repositoryInfrastructureEntityLimit entities -- the content store had no
// more to give -- must not report truncation.
func TestQueryRepoInfrastructureFromContentNoTruncationAtLimit(t *testing.T) {
	t.Parallel()

	entities := make([]EntityContent, repositoryInfrastructureEntityLimit)
	for i := range entities {
		entities[i] = EntityContent{
			EntityType:   "K8sResource",
			EntityName:   fmt.Sprintf("res-%d", i),
			RelativePath: fmt.Sprintf("deploy/res-%d.yaml", i),
		}
	}
	content := fakePortContentStore{entities: entities}

	got, truncated := queryRepoInfrastructureFromContent(t.Context(), content, "repo-1")
	if truncated {
		t.Fatal("truncated = true, want false when the entity count exactly equals repositoryInfrastructureEntityLimit")
	}
	if len(got) != repositoryInfrastructureEntityLimit {
		t.Fatalf("len(got) = %d, want %d", len(got), repositoryInfrastructureEntityLimit)
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

// TestQueryRepoInfrastructureFromContentIgnoresNonInfrastructureEntityCount is
// the regression test for the #5764 P1 review finding that inverted the
// defect #5764 exists to remove: queryRepoInfrastructureFromContent used to
// call the UNTYPED ListRepoEntities, so "truncated" reported whether the
// repository had more than repositoryInfrastructureEntityLimit content
// entities of ANY type (functions, classes, structs -- every parsed entity;
// true for nearly every real repository), not whether the infrastructure
// panel itself was clipped. A repository with a handful of infrastructure
// entities buried in a much larger non-infrastructure population must NOT be
// reported as truncated, and every infrastructure row must still be present.
// Both existing content-path tests above
// (TestQueryRepoInfrastructureUsesContentRowsBeforeGraph and
// TestQueryRepoInfrastructureFromContentSignalsTruncationAtLimit) populate
// 100% K8sResource entities, the one population shape where this bug is
// invisible -- this test uses a MIXED population instead, exactly limit+1
// entities total, of which only 3 are infrastructure-typed.
func TestQueryRepoInfrastructureFromContentIgnoresNonInfrastructureEntityCount(t *testing.T) {
	t.Parallel()

	const nonInfraCount = repositoryInfrastructureEntityLimit - 2 // 4998
	entities := make([]EntityContent, 0, nonInfraCount+3)
	for i := 0; i < nonInfraCount; i++ {
		entities = append(entities, EntityContent{
			EntityType:   "Function",
			EntityName:   fmt.Sprintf("fn-%d", i),
			RelativePath: fmt.Sprintf("src/fn-%d.go", i),
		})
	}
	for i := 0; i < 3; i++ {
		entities = append(entities, EntityContent{
			EntityType:   "TerraformResource",
			EntityName:   fmt.Sprintf("tf-%d", i),
			RelativePath: fmt.Sprintf("infra/tf-%d.tf", i),
		})
	}
	if len(entities) != repositoryInfrastructureEntityLimit+1 {
		t.Fatalf("test setup: len(entities) = %d, want %d (limit+1)", len(entities), repositoryInfrastructureEntityLimit+1)
	}
	content := fakePortContentStore{entities: entities}

	got, truncated := queryRepoInfrastructureFromContent(t.Context(), content, "repo-1")
	if truncated {
		t.Fatal("truncated = true, want false: only 3 infrastructure-typed entities exist, far under the limit, even though the repo's total content-entity count (including non-infrastructure types) exceeds it")
	}
	if len(got) != 3 {
		t.Fatalf("len(got) = %d, want 3 infrastructure rows: %#v", len(got), got)
	}
	for _, entry := range got {
		if got, want := StringVal(entry, "type"), "TerraformResource"; got != want {
			t.Fatalf("type = %q, want %q", got, want)
		}
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
