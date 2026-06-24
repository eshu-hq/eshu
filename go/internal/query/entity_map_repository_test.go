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

func TestEntityMapRepositoryAnchorUsesDirectRelationshipFamilyTraversal(t *testing.T) {
	t.Parallel()

	graph := &recordingEntityMapGraph{runRows: [][]map[string]any{
		{
			{
				"id":              "repo-checkout",
				"name":            "checkout-service",
				"labels":          []any{"Repository"},
				"anchor_label":    "Repository",
				"anchor_property": "id",
				"anchor_value":    "repo-checkout",
			},
		},
		{},
		{},
	}}
	handler := &ImpactHandler{Neo4j: graph, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/impact/entity-map",
		bytes.NewBufferString(`{"from":"repo-checkout","from_type":"repository","depth":1,"limit":5}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if got, want := len(graph.runCalls), 2; got != want {
		t.Fatalf("graph Run calls = %d, want resolver plus one direct repository traversal spec", got)
	}
	for _, call := range graph.runCalls[1:] {
		if !strings.Contains(call.cypher, "(start:Repository {id: $from_id})") {
			t.Fatalf("traversal cypher = %s, want typed Repository id anchor", call.cypher)
		}
		if got := strings.Count(call.cypher, "MATCH "); got != 1 {
			t.Fatalf("traversal cypher has %d MATCH clauses, want one connected anchor+expand: %s", got, call.cypher)
		}
		if strings.Contains(call.cypher, "*1..1") {
			t.Fatalf("traversal cypher = %s, want direct one-hop traversal without variable path expansion", call.cypher)
		}
		if !strings.Contains(call.cypher, " AS relationship_type") {
			t.Fatalf("traversal cypher = %s, want direct relationship type projection", call.cypher)
		}
		if strings.Contains(call.cypher, "CONTAINS") || strings.Contains(call.cypher, "REPO_CONTAINS") {
			t.Fatalf("traversal cypher = %s, want default map to avoid structural repository fanout", call.cypher)
		}
		if strings.Contains(call.cypher, "CALLS") || strings.Contains(call.cypher, "IMPORTS") {
			t.Fatalf("traversal cypher = %s, want repository map to avoid code-edge fanout", call.cypher)
		}
	}
	var sawIncomingDeploys bool
	for _, call := range graph.runCalls[1:] {
		if strings.Contains(call.cypher, "(start:Repository {id: $from_id})<-[rel:DEPLOYS_FROM|") {
			sawIncomingDeploys = true
		}
	}
	if !sawIncomingDeploys {
		t.Fatalf("traversal calls = %#v, want incoming DEPLOYS_FROM family", graph.runCalls)
	}

	data := decodeEntityMapData(t, w)
	coverage := data["coverage"].(map[string]any)
	if got, want := coverage["query_shape"], "typed_entity_map_relationship_family"; got != want {
		t.Fatalf("coverage.query_shape = %#v, want %#v", got, want)
	}
}

func TestEntityMapRepositoryAnchorUsesNarrowRelationshipFamilyForBoundedDepth(t *testing.T) {
	t.Parallel()

	graph := &recordingEntityMapGraph{runRows: [][]map[string]any{
		{
			{
				"id":              "repo-checkout",
				"name":            "checkout-service",
				"labels":          []any{"Repository"},
				"anchor_label":    "Repository",
				"anchor_property": "id",
				"anchor_value":    "repo-checkout",
			},
		},
		{},
		{},
		{},
		{},
	}}
	handler := &ImpactHandler{Neo4j: graph, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/impact/entity-map",
		bytes.NewBufferString(`{"from":"repo-checkout","from_type":"repository","depth":2,"limit":5}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if got, want := len(graph.runCalls), 3; got != want {
		t.Fatalf("graph Run calls = %d, want resolver plus direct and deeper repository traversal specs", got)
	}
	for _, call := range graph.runCalls[1:] {
		if !strings.Contains(call.cypher, "(start:Repository {id: $from_id})") {
			t.Fatalf("traversal cypher = %s, want typed Repository id anchor", call.cypher)
		}
		if got := strings.Count(call.cypher, "MATCH "); got != 1 {
			t.Fatalf("traversal cypher has %d MATCH clauses, want one connected anchor+expand: %s", got, call.cypher)
		}
		if strings.Contains(call.cypher, "(start:Repository {id: $from_id})-[rels:") {
			t.Fatalf("traversal cypher = %s, want no outgoing repository default traversal", call.cypher)
		}
		for _, avoid := range []string{"CONTAINS", "REPO_CONTAINS", "CALLS", "IMPORTS"} {
			if strings.Contains(call.cypher, avoid) {
				t.Fatalf("traversal cypher = %s, want repository depth>1 default to avoid %s", call.cypher, avoid)
			}
		}
	}
	for _, want := range entityMapRepositoryIncomingRelationships {
		var sawRelationship bool
		for _, call := range graph.runCalls[1:] {
			if strings.Contains(call.cypher, want) {
				sawRelationship = true
			}
		}
		if !sawRelationship {
			t.Fatalf("traversal calls = %#v, want incoming %s family", graph.runCalls, want)
		}
	}

	data := decodeEntityMapData(t, w)
	coverage := data["coverage"].(map[string]any)
	if got, want := coverage["query_shape"], "typed_entity_map_bounded_relationship_family"; got != want {
		t.Fatalf("coverage.query_shape = %#v, want %#v", got, want)
	}
}

func TestEntityMapDirectTraversalNormalizesScalarRelationshipType(t *testing.T) {
	t.Parallel()

	graph := &recordingEntityMapGraph{runRows: [][]map[string]any{
		{
			{
				"id":              "repo-checkout",
				"name":            "checkout-service",
				"labels":          []any{"Repository"},
				"anchor_label":    "Repository",
				"anchor_property": "id",
				"anchor_value":    "repo-checkout",
			},
		},
		{
			{
				"entity_id":         "directory:.github",
				"entity_name":       ".github",
				"entity_labels":     []any{"Directory"},
				"direction":         "outgoing",
				"depth":             int64(1),
				"relationship_type": "CONTAINS",
				"repo_id":           "repo-checkout",
			},
		},
		{},
	}}
	handler := &ImpactHandler{Neo4j: graph, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/impact/entity-map",
		bytes.NewBufferString(`{"from":"repo-checkout","from_type":"repository","relationship":"CONTAINS","depth":1,"limit":5}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	data := decodeEntityMapData(t, w)
	evidence := data["evidence"].(map[string]any)
	relationships := evidence["relationships"].([]any)
	if got, want := len(relationships), 1; got != want {
		t.Fatalf("relationship count = %d, want %d", got, want)
	}
	relationship := relationships[0].(map[string]any)
	if got, want := relationship["relationship_type"], "CONTAINS"; got != want {
		t.Fatalf("relationship_type = %#v, want %#v", got, want)
	}
	types := relationship["relationship_types"].([]any)
	if got, want := types[0], "CONTAINS"; got != want {
		t.Fatalf("relationship_types[0] = %#v, want %#v", got, want)
	}
}

func TestEntityMapExplicitRelationshipBackfillsMissingBackendType(t *testing.T) {
	t.Parallel()

	graph := &recordingEntityMapGraph{runRows: [][]map[string]any{
		{
			{
				"id":              "repo-checkout",
				"name":            "checkout-service",
				"labels":          []any{"Repository"},
				"anchor_label":    "Repository",
				"anchor_property": "id",
				"anchor_value":    "repo-checkout",
			},
		},
		{
			{
				"entity_id":     "directory:.github",
				"entity_name":   ".github",
				"entity_labels": []any{"Directory"},
				"direction":     "outgoing",
				"depth":         int64(1),
				"repo_id":       "repo-checkout",
			},
		},
		{},
	}}
	handler := &ImpactHandler{Neo4j: graph, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/impact/entity-map",
		bytes.NewBufferString(`{"from":"repo-checkout","from_type":"repository","relationship":"CONTAINS","depth":1,"limit":5}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	data := decodeEntityMapData(t, w)
	evidence := data["evidence"].(map[string]any)
	relationships := evidence["relationships"].([]any)
	relationship := relationships[0].(map[string]any)
	if got, want := relationship["relationship_type"], "CONTAINS"; got != want {
		t.Fatalf("relationship_type = %#v, want %#v", got, want)
	}
	types := relationship["relationship_types"].([]any)
	if got, want := types[0], "CONTAINS"; got != want {
		t.Fatalf("relationship_types[0] = %#v, want %#v", got, want)
	}
}

func TestEntityMapExplicitRelationshipUsesRequestedDirectTraversal(t *testing.T) {
	t.Parallel()

	graph := &recordingEntityMapGraph{runRows: [][]map[string]any{
		{
			{
				"id":              "repo-checkout",
				"name":            "checkout-service",
				"labels":          []any{"Repository"},
				"anchor_label":    "Repository",
				"anchor_property": "id",
				"anchor_value":    "repo-checkout",
			},
		},
		{},
		{},
	}}
	handler := &ImpactHandler{Neo4j: graph, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/impact/entity-map",
		bytes.NewBufferString(`{"from":"repo-checkout","from_type":"repository","relationship":"CONTAINS","depth":1,"limit":5}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	for _, call := range graph.runCalls[1:] {
		if strings.Contains(call.cypher, "*1..1") {
			t.Fatalf("traversal cypher = %s, want direct one-hop traversal for explicit relationship", call.cypher)
		}
		if !strings.Contains(call.cypher, "[rel:CONTAINS]") {
			t.Fatalf("traversal cypher = %s, want requested CONTAINS relationship pattern", call.cypher)
		}
	}
}
