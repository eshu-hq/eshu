// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestChangeSurfaceScopedGrantsApplyBeforeBothLimits(t *testing.T) {
	t.Parallel()

	access := changeSurfaceTestAccess(
		[]string{"repository:owner", "repository:consumer"},
		[]string{"scope:owner"},
	)
	var calls int
	handler := &ImpactHandler{Neo4j: fakeGraphReader{run: func(
		_ context.Context,
		cypher string,
		params map[string]any,
	) ([]map[string]any, error) {
		calls++
		assertChangeSurfaceGrantParams(t, params, access)
		switch calls {
		case 1:
			if strings.Contains(cypher, "CALL {") &&
				strings.Contains(cypher, "impacted.repo_id IN $allowed_repository_ids") &&
				strings.Contains(cypher, "impacted.repo_id IN $allowed_scope_ids") &&
				strings.Contains(cypher, "WITH path, impacted") &&
				strings.Index(cypher, "allowed_repository_ids") < strings.LastIndex(cypher, "LIMIT") {
				return []map[string]any{changeSurfaceTestRow(
					"workload:granted", "granted-workload", []any{"Workload"},
					"scope:owner", "DEFINES",
				)}, nil
			}
			return changeSurfaceDeniedRows("Workload", "DEFINES", 11), nil
		case 2:
			if strings.Contains(cypher, "impacted.id IN $allowed_repository_ids") &&
				strings.Index(cypher, "allowed_repository_ids") < strings.LastIndex(cypher, "LIMIT") {
				return []map[string]any{changeSurfaceTestRow(
					"repository:consumer", "granted-consumer", []any{"Repository"},
					"", "DEPENDS_ON",
				)}, nil
			}
			return changeSurfaceDeniedRows("Repository", "DEPENDS_ON", 11), nil
		default:
			t.Fatalf("unexpected graph call %d", calls)
			return nil, nil
		}
	}}}

	rows, truncated, err := handler.changeSurfaceTraversalRows(
		context.Background(),
		changeSurfaceTargetCandidate{ID: "repository:changed", Labels: []string{"Repository"}},
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
	if got, want := changeSurfaceTestIDs(rows), []string{"repository:consumer", "workload:granted"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("row ids = %#v, want %#v", got, want)
	}
	if got, want := calls, 2; got != want {
		t.Fatalf("graph calls = %d, want %d", got, want)
	}
}

func TestChangeSurfaceDeniedRowsCannotSetScopedTruncation(t *testing.T) {
	t.Parallel()

	access := changeSurfaceTestAccess([]string{"repository:granted"}, nil)
	handler := &ImpactHandler{Neo4j: fakeGraphReader{run: func(
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
		changeSurfaceTargetCandidate{ID: "repository:changed", Labels: []string{"Repository"}},
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
	handler := &ImpactHandler{Neo4j: fakeGraphReader{run: func(
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
		changeSurfaceTargetCandidate{ID: "repository:changed", Labels: []string{"Repository"}},
		"",
		4,
		10,
		repositoryAccessFilter{allScopes: true},
	)
	if err != nil {
		t.Fatalf("changeSurfaceTraversalRows() error = %v", err)
	}
	wantQueries := []string{
		fmt.Sprintf(changeSurfaceInvestigateCypher, "(start:Repository {id: $target_id})", 4, ""),
		fmt.Sprintf(changeSurfaceRepositoryConsumersCypher, 4, "", 11),
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

func changeSurfaceTestAccess(repositoryIDs, scopeIDs []string) repositoryAccessFilter {
	access := repositoryAccessFilter{
		allowedRepositoryIDs: append([]string(nil), repositoryIDs...),
		allowedScopeIDs:      append([]string(nil), scopeIDs...),
		allowed:              make(map[string]struct{}, len(repositoryIDs)+len(scopeIDs)),
	}
	for _, id := range repositoryIDs {
		access.allowed[id] = struct{}{}
	}
	for _, id := range scopeIDs {
		access.allowed[id] = struct{}{}
	}
	return access
}

func assertChangeSurfaceGrantParams(t *testing.T, params map[string]any, access repositoryAccessFilter) {
	t.Helper()
	if got, want := params["allowed_repository_ids"], access.allowedRepositoryIDs; !reflect.DeepEqual(got, want) {
		t.Fatalf("allowed_repository_ids = %#v, want %#v", got, want)
	}
	if got, want := params["allowed_scope_ids"], access.allowedScopeIDs; !reflect.DeepEqual(got, want) {
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

func changeSurfaceTestIDs(rows []map[string]any) []string {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, StringVal(row, "id"))
	}
	return ids
}
