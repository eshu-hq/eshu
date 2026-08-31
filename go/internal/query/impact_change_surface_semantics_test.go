// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const (
	changeSurfaceConsumerRepoID   = "repository:orders"
	changeSurfaceDependencyRepoID = "repository:lib-common"
)

func TestChangeSurfaceImpactRowsUseConsumerDirectionForDependencies(t *testing.T) {
	t.Parallel()

	graph := semanticChangeSurfaceGraph()
	handler := &ImpactHandler{Neo4j: graph}
	target := changeSurfaceTargetCandidate{
		ID:     changeSurfaceDependencyRepoID,
		Name:   "lib-common",
		Labels: []string{"Repository"},
		RepoID: changeSurfaceDependencyRepoID,
	}

	rows, truncated, err := handler.changeSurfaceImpactRows(
		t.Context(),
		changeSurfaceInvestigationRequest{MaxDepth: 4, Limit: 10},
		target,
	)
	if err != nil {
		t.Fatalf("changeSurfaceImpactRows() error = %v", err)
	}
	if truncated {
		t.Fatal("changeSurfaceImpactRows() truncated = true, want false")
	}
	assertSemanticChangeSurfaceRows(t, rows)
	assertSemanticChangeSurfaceQueries(t, graph.runCalls)
}

func TestLegacyChangeSurfaceUsesConsumerDirectionAndKeepsProvenance(t *testing.T) {
	t.Parallel()

	graph := semanticChangeSurfaceGraph()
	handler := &ImpactHandler{Neo4j: graph}
	target := changeSurfaceTargetCandidate{
		ID:     changeSurfaceDependencyRepoID,
		Name:   "lib-common",
		Labels: []string{"Repository"},
		RepoID: changeSurfaceDependencyRepoID,
	}

	rows, truncated, err := handler.findChangeSurfaceImpactRows(
		t.Context(), target, "", 4, 10, repositoryAccessFilter{AllScopes: true},
	)
	if err != nil {
		t.Fatalf("findChangeSurfaceImpactRows() error = %v", err)
	}
	if truncated {
		t.Fatal("findChangeSurfaceImpactRows() truncated = true, want false")
	}
	assertSemanticChangeSurfaceRows(t, rows)
	if got := StringVal(rows[0], "rel_type"); got != "DEFINES" {
		t.Fatalf("first row rel_type = %q, want DEFINES", got)
	}
	if got := StringVal(rows[1], "rel_type"); got != "DEPENDS_ON" {
		t.Fatalf("second row rel_type = %q, want DEPENDS_ON", got)
	}
	assertSemanticChangeSurfaceQueries(t, graph.runCalls)
}

func TestPreChangeImpactDerivesRepositoryConsumerAnchorFromChangedFiles(t *testing.T) {
	t.Parallel()

	graph := semanticChangeSurfaceGraph()
	store := fakePortContentStore{entities: []EntityContent{{
		EntityID:     "content-entity:common",
		EntityName:   "Common",
		EntityType:   "Function",
		RepoID:       changeSurfaceDependencyRepoID,
		RelativePath: "common.go",
		Language:     "go",
		StartLine:    1,
		EndLine:      10,
	}}}
	handler := &ImpactHandler{
		Neo4j:   graph,
		Content: store,
		Profile: ProfileLocalAuthoritative,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/impact/pre-change",
		bytes.NewBufferString(`{"repo_id":"repository:lib-common","changed_paths":["common.go"],"limit":10}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
	data := decodePreChangeEnvelope(t, rec).Data.(map[string]any)
	direct := data["direct_impact"].([]any)
	var foundConsumer bool
	for _, item := range direct {
		if item.(map[string]any)["id"] == changeSurfaceConsumerRepoID {
			foundConsumer = true
			break
		}
	}
	if !foundConsumer {
		t.Fatalf("direct impact does not contain repository consumer %q: %#v", changeSurfaceConsumerRepoID, direct)
	}
	resolution := data["target_resolution"].(map[string]any)
	if got, want := resolution["input"], changeSurfaceDependencyRepoID; got != want {
		t.Fatalf("target resolution input = %#v, want derived repository %q", got, want)
	}
}

func semanticChangeSurfaceGraph() *recordingChangeSurfaceGraph {
	return &recordingChangeSurfaceGraph{runFunc: func(cypher string, _ map[string]any) ([]map[string]any, error) {
		switch {
		case strings.Contains(cypher, "MATCH (n:Repository {id: $target})"):
			return []map[string]any{{
				"id": changeSurfaceDependencyRepoID, "name": "lib-common",
				"labels": []any{"Repository"}, "repo_id": changeSurfaceDependencyRepoID,
			}}, nil
		case strings.Contains(cypher, "<-[:DEPENDS_ON"):
			return []map[string]any{semanticChangeSurfaceRow(
				changeSurfaceConsumerRepoID, "orders-api", 1, "DEPENDS_ON",
			)}, nil
		case strings.Contains(cypher, "MATCH path"):
			return []map[string]any{
				semanticChangeSurfaceRow("workload:lib-common", "lib-common-service", 1, "DEFINES"),
				semanticChangeSurfaceRow(changeSurfaceDependencyRepoID, "dependency", 1, "DEPENDS_ON"),
				semanticMixedChangeSurfaceRow("workload:mixed", "mixed", 2),
			}, nil
		default:
			return nil, nil
		}
	}}
}

func semanticChangeSurfaceRow(id, name string, depth int64, relType string) map[string]any {
	repoID := changeSurfaceDependencyRepoID
	if strings.HasPrefix(id, "repository:") {
		repoID = id
	}
	return map[string]any{
		"id": id, "name": name, "labels": []any{"Repository"}, "repo_id": repoID, "depth": depth,
		"rels": []any{map[string]any{"type": relType, "properties": map[string]any{"reason": "fixture"}}},
	}
}

func semanticMixedChangeSurfaceRow(id, name string, depth int64) map[string]any {
	row := semanticChangeSurfaceRow(id, name, depth, "DEFINES")
	row["rels"] = append(row["rels"].([]any), map[string]any{
		"type": "DEPENDS_ON", "properties": map[string]any{"reason": "fixture"},
	})
	return row
}

func assertSemanticChangeSurfaceRows(t *testing.T, rows []map[string]any) {
	t.Helper()
	if got, want := len(rows), 2; got != want {
		t.Fatalf("row count = %d, want %d: %#v", got, want, rows)
	}
	wantIDs := []string{"workload:lib-common", changeSurfaceConsumerRepoID}
	for index, want := range wantIDs {
		if got := StringVal(rows[index], "id"); got != want {
			t.Fatalf("row %d id = %q, want %q; rows=%#v", index, got, want, rows)
		}
	}
}

func assertSemanticChangeSurfaceQueries(t *testing.T, calls []changeSurfaceRunCall) {
	t.Helper()
	if got, want := len(calls), 2; got != want {
		t.Fatalf("graph run calls = %d, want outgoing and dependency-consumer queries", got)
	}
	if !strings.Contains(calls[0].cypher, "relationships(path) as rels") {
		t.Fatalf("outgoing query must project raw relationships: %s", calls[0].cypher)
	}
	if !strings.Contains(calls[1].cypher, "(start:Repository") ||
		!strings.Contains(calls[1].cypher, "<-[:DEPENDS_ON") {
		t.Fatalf("consumer query must use an incoming typed repository dependency traversal: %s", calls[1].cypher)
	}
}
