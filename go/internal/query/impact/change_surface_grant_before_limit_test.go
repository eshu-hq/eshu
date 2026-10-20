// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

func TestChangeSurfaceScopedGrantsApplyBeforeBothLimits(t *testing.T) {
	t.Parallel()

	access := changeSurfaceTestAccess(
		[]string{"repository:owner", "repository:consumer"},
		[]string{"scope:owner"},
	)
	// Each branch reads depths 1..4 with the remaining probe need (8 calls),
	// and per-call params are asserted on every call. The fake serves its
	// granted row only at depth 1, so the grant-before-LIMIT shape is pinned
	// on the depth-1 statement of each branch; the grant-clause position is
	// fixed in the shared per-branch template, so depths 2-4 inherit it.
	var calls int
	handler := &Handler{Neo4j: graph.FakeGraphReader{RunFn: func(
		_ context.Context,
		cypher string,
		params map[string]any,
	) ([]map[string]any, error) {
		calls++
		assertChangeSurfaceGrantParams(t, params, access)
		depth, ok := changeSurfaceDepthRangeOfQuery(cypher)
		if !ok {
			t.Fatalf("call %d is not an exact-depth read: %.120q", calls, cypher)
		}
		if depth != 1 {
			return []map[string]any{}, nil
		}
		if strings.Contains(cypher, "CALL {") {
			if strings.Contains(cypher, "impacted.repo_id IN $allowed_repository_ids") &&
				strings.Contains(cypher, "impacted.repo_id IN $allowed_scope_ids") &&
				strings.Contains(cypher, "WITH path, impacted") &&
				strings.Index(cypher, "allowed_repository_ids") < strings.LastIndex(cypher, "LIMIT") {
				return []map[string]any{changeSurfaceTestRow(
					"workload:granted", "granted-workload", []any{"Workload"},
					"scope:owner", "DEFINES",
				)}, nil
			}
			return changeSurfaceDeniedRows("Workload", "DEFINES", 11), nil
		}
		if strings.Contains(cypher, "impacted.id IN $allowed_repository_ids") &&
			strings.Index(cypher, "allowed_repository_ids") < strings.LastIndex(cypher, "LIMIT") {
			return []map[string]any{changeSurfaceTestRow(
				"repository:consumer", "granted-consumer", []any{"Repository"},
				"", "DEPENDS_ON",
			)}, nil
		}
		return changeSurfaceDeniedRows("Repository", "DEPENDS_ON", 11), nil
	}}}

	rows, truncated, err := handler.changeSurfaceTraversalRows(
		context.Background(),
		ChangeSurfaceTargetCandidate{ID: "repository:changed", Labels: []string{"Repository"}},
		"",
		4,
		10,
		access,
	)
	if err != nil {
		t.Fatalf("changeSurfaceTraversalRows() error = %v", err)
	}
	if truncated {
		t.Fatal("changeSurfaceTraversalRows() truncated = true, want false after grant pushdown")
	}
	if got, want := testutil.RowIDs(rows), []string{"repository:consumer", "workload:granted"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("row ids = %#v, want %#v", got, want)
	}
	if got, want := calls, 8; got != want {
		t.Fatalf("graph calls = %d, want %d (depths 1..4 in each branch)", got, want)
	}
}

func TestChangeSurfaceDeniedRowsCannotSetScopedTruncation(t *testing.T) {
	t.Parallel()

	access := changeSurfaceTestAccess([]string{"repository:granted"}, nil)
	handler := &Handler{Neo4j: graph.FakeGraphReader{RunFn: func(
		_ context.Context,
		_ string,
		params map[string]any,
	) ([]map[string]any, error) {
		if _, ok := params["allowed_repository_ids"]; ok {
			return []map[string]any{}, nil
		}
		return changeSurfaceDeniedRows("Repository", "DEPENDS_ON", 11), nil
	}}}

	rows, truncated, err := handler.changeSurfaceTraversalRows(
		context.Background(),
		ChangeSurfaceTargetCandidate{ID: "repository:changed", Labels: []string{"Repository"}},
		"",
		4,
		10,
		access,
	)
	if err != nil {
		t.Fatalf("changeSurfaceTraversalRows() error = %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("rows = %#v, want empty", rows)
	}
	if truncated {
		t.Fatal("truncated = true, want false when every raw row is outside the grant")
	}
}

func TestChangeSurfaceUnscopedQueriesStayByteIdentical(t *testing.T) {
	t.Parallel()

	var queries []string
	var paramsSeen []map[string]any
	handler := &Handler{Neo4j: graph.FakeGraphReader{RunFn: func(
		_ context.Context,
		cypher string,
		params map[string]any,
	) ([]map[string]any, error) {
		queries = append(queries, cypher)
		paramsSeen = append(paramsSeen, params)
		return []map[string]any{}, nil
	}}}

	_, _, err := handler.changeSurfaceTraversalRows(
		context.Background(),
		ChangeSurfaceTargetCandidate{ID: "repository:changed", Labels: []string{"Repository"}},
		"",
		4,
		10,
		querycontract.RepositoryAccessFilter{AllScopes: true},
	)
	if err != nil {
		t.Fatalf("changeSurfaceTraversalRows() error = %v", err)
	}
	// The empty fake exhausts no depth, so every depth runs with the full
	// probe need: four exact-depth outgoing reads, then four exact-depth
	// consumer reads, each byte-pinned.
	wantQueries := []string{
		fmt.Sprintf(changeSurfaceInvestigateCypher, "(start:Repository {id: $target_id})", 1, 1, ""),
		fmt.Sprintf(changeSurfaceInvestigateCypher, "(start:Repository {id: $target_id})", 2, 2, ""),
		fmt.Sprintf(changeSurfaceInvestigateCypher, "(start:Repository {id: $target_id})", 3, 3, ""),
		fmt.Sprintf(changeSurfaceInvestigateCypher, "(start:Repository {id: $target_id})", 4, 4, ""),
		fmt.Sprintf(changeSurfaceRepositoryConsumersCypher, 1, 1, "", 11),
		fmt.Sprintf(changeSurfaceRepositoryConsumersCypher, 2, 2, "", 11),
		fmt.Sprintf(changeSurfaceRepositoryConsumersCypher, 3, 3, "", 11),
		fmt.Sprintf(changeSurfaceRepositoryConsumersCypher, 4, 4, "", 11),
	}
	if !reflect.DeepEqual(queries, wantQueries) {
		t.Fatalf("queries = %#v, want byte-identical %#v", queries, wantQueries)
	}
	for _, params := range paramsSeen {
		if _, ok := params["allowed_repository_ids"]; ok {
			t.Fatalf("unscoped params unexpectedly bind allowed_repository_ids: %#v", params)
		}
		if _, ok := params["allowed_scope_ids"]; ok {
			t.Fatalf("unscoped params unexpectedly bind allowed_scope_ids: %#v", params)
		}
	}
}

func TestChangeSurfaceGoFilterRemainsDefenseInDepth(t *testing.T) {
	t.Parallel()

	access := changeSurfaceTestAccess([]string{"repository:granted"}, nil)
	malicious := changeSurfaceTestRow(
		"workload:denied", "denied", []any{"Workload"}, "repository:denied", "DEFINES",
	)
	if got := changeSurfaceFilterTraversalRows([]map[string]any{malicious}, "", access, false); len(got) != 0 {
		t.Fatalf("denied backend row survived Go defense: %#v", got)
	}

	collision := changeSurfaceTestRow(
		"repository:denied", "collision", []any{"Repository"}, "repository:granted", "DEPLOYS_FROM",
	)
	if got := changeSurfaceFilterTraversalRows([]map[string]any{collision}, "", access, false); len(got) != 0 {
		t.Fatalf("Repository id collision survived Go defense: %#v", got)
	}
}

func changeSurfaceTestAccess(repositoryIDs, scopeIDs []string) querycontract.RepositoryAccessFilter {
	access := querycontract.RepositoryAccessFilter{
		AllowedRepositoryIDs: append([]string(nil), repositoryIDs...),
		AllowedScopeIDs:      append([]string(nil), scopeIDs...),
		Allowed:              make(map[string]struct{}, len(repositoryIDs)+len(scopeIDs)),
	}
	for _, id := range repositoryIDs {
		access.Allowed[id] = struct{}{}
	}
	for _, id := range scopeIDs {
		access.Allowed[id] = struct{}{}
	}
	return access
}

func assertChangeSurfaceGrantParams(t *testing.T, params map[string]any, access querycontract.RepositoryAccessFilter) {
	t.Helper()
	if got, want := params["allowed_repository_ids"], access.AllowedRepositoryIDs; !reflect.DeepEqual(got, want) {
		t.Fatalf("allowed_repository_ids = %#v, want %#v", got, want)
	}
	if got, want := params["allowed_scope_ids"], access.AllowedScopeIDs; !reflect.DeepEqual(got, want) {
		t.Fatalf("allowed_scope_ids = %#v, want %#v", got, want)
	}
}

func changeSurfaceDeniedRows(label, relType string, count int) []map[string]any {
	rows := make([]map[string]any, 0, count)
	for i := range count {
		repoID := "repository:denied"
		if label == "Repository" {
			repoID = ""
		}
		rows = append(rows, changeSurfaceTestRow(
			fmt.Sprintf("denied:%02d", i), fmt.Sprintf("denied-%02d", i), []any{label}, repoID, relType,
		))
	}
	return rows
}

func changeSurfaceTestRow(id, name string, labels []any, repoID, relType string) map[string]any {
	return map[string]any{
		"id":      id,
		"name":    name,
		"labels":  labels,
		"repo_id": repoID,
		"depth":   int64(1),
		"rels": []any{map[string]any{
			"type":       relType,
			"confidence": float64(1),
			"reason":     "test",
		}},
	}
}
