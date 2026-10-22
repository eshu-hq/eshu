// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func TestEntityMapSuppressesLegacyAppSyncKeysInResolverAndNeighborhood(t *testing.T) {
	t.Parallel()

	resolver := EntityMapNodeResolverQuery("CloudResource", "id", "api-key", "id", 0, 10)
	predicate := "coalesce(n.resource_type, '') <> 'aws_appsync_api_key' AND coalesce(n.data_type, '') <> 'aws_appsync_api_key'"
	if !strings.Contains(resolver.Cypher, predicate) || strings.Index(resolver.Cypher, predicate) > strings.Index(resolver.Cypher, "ORDER BY") {
		t.Fatalf("CloudResource resolver must exclude legacy keys before LIMIT: %s", resolver.Cypher)
	}
	selected := EntityMapCandidate{ID: "workload:service", AnchorLabel: "Workload", AnchorProperty: "id"}
	traversal := EntityMapDirectTraversalCypher(selected, EntityMapTraversalSpec{Direction: "outgoing", Relationships: []string{"USES"}, MinHops: 1, MaxHops: 1})
	if strings.Contains(traversal, "labels(entity) AND") || strings.Contains(traversal, "IN labels(entity)") {
		t.Fatalf("entity map neighborhood WHERE must stay bare (a labels() predicate empties the result on NornicDB): %s", traversal)
	}
	if !strings.Contains(traversal, "entity.resource_type AS resource_type") || !strings.Contains(traversal, "entity.data_type AS data_type") {
		t.Fatalf("entity map neighborhood must project the type fields the Go key filter reads: %s", traversal)
	}
	rows := NormalizeEntityMapRows([]map[string]any{
		{"id": "workload:healthy", "name": "healthy", "entity_labels": []any{"Workload"}, "relationship_type": "USES"},
		{"id": "key:1", "name": "key-1", "entity_labels": []any{"CloudResource"}, "resource_type": "aws_appsync_api_key", "relationship_type": "USES"},
		{"id": "key:2", "name": "key-2", "entity_labels": []any{"CloudResource"}, "data_type": "aws_appsync_api_key", "relationship_type": "USES"},
		{"id": "fn:typed", "name": "typed", "entity_labels": []any{"Function"}, "resource_type": "aws_appsync_api_key", "relationship_type": "CALLS"},
	}, EntityMapTraversalSpec{Direction: "outgoing", Relationships: []string{"USES"}, MinHops: 1, MaxHops: 1}, selected)
	if len(rows) != 2 {
		t.Fatalf("neighborhood must drop only CloudResource key rows, got %v", rows)
	}
	for _, row := range rows {
		if id := querycontract.StringVal(row, "entity_id"); id != "workload:healthy" && id != "fn:typed" {
			t.Fatalf("neighborhood kept unexpected row %v", row)
		}
	}
	candidates := entityMapCandidates([]map[string]any{
		{"id": "key:1", "name": "key-1", "labels": []any{"CloudResource"}, "resource_type": "aws_appsync_api_key", "repo_id": "r", "anchor_label": "CloudResource", "anchor_property": "id", "anchor_value": "key:1"},
		{"id": "workload:healthy", "name": "healthy", "labels": []any{"Workload"}, "repo_id": "r", "anchor_label": "Workload", "anchor_property": "id", "anchor_value": "workload:healthy"},
	})
	if len(candidates) != 1 || candidates[0].ID != "workload:healthy" {
		t.Fatalf("resolver must drop legacy key candidates, got %+v", candidates)
	}
}

func TestEntityMapPopulatesTypedVerbAndEntityIDForVarLengthEdge(t *testing.T) {
	t.Parallel()

	resolverRows := []map[string]any{
		{
			"id":              "workload:orders-api",
			"name":            "orders-api",
			"labels":          []any{"Workload"},
			"repo_id":         "repository:r_orders_api",
			"anchor_label":    "Workload",
			"anchor_property": "id",
			"anchor_value":    "workload:orders-api",
		},
	}
	definesRow := map[string]any{
		"entity_id":          "repository:r_orders_api",
		"entity_name":        "orders-api",
		"entity_labels":      []any{"Repository"},
		"direction":          "incoming",
		"depth":              int64(0),
		"relationship_type":  "DEFINES",
		"relationship_types": []any{},
		"repo_id":            "repository:r_orders_api",
	}
	runRows := [][]map[string]any{
		resolverRows,
		nil,
		nil,
		{definesRow},
		nil,
	}

	graph := &recordingEntityMapGraph{runRows: runRows}
	handler := &Handler{Neo4j: graph, Profile: querycontract.ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/impact/entity-map",
		bytes.NewBufferString(`{"from":"orders-api","from_type":"service","depth":2}`),
	)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}

	var definesCall entityMapRunCall
	foundDefinesCall := false
	for _, call := range graph.runCalls[1:] {
		if strings.Contains(call.cypher, "RETURN DISTINCT") {
			t.Fatalf("traversal cypher uses RETURN DISTINCT: %s", call.cypher)
		}
		if strings.Contains(call.cypher, "[rel:DEFINES|") {
			definesCall = call
			foundDefinesCall = true
		}
	}
	if !foundDefinesCall {
		t.Fatal("incoming DEFINES traversal call not recorded")
	}
	if strings.Contains(definesCall.cypher, "coalesce(entity.id, entity.uid, entity.resource_id, entity.path, entity.name) AS entity_id") {
		t.Fatalf("DEFINES traversal cypher still uses fragile multi-property entity_id coalesce: %s", definesCall.cypher)
	}

	data := decodeEntityMapData(t, w)
	evidence := data["evidence"].(map[string]any)
	relationships := evidence["relationships"].([]any)
	if len(relationships) != 1 {
		t.Fatalf("relationship count = %d, want 1; evidence=%#v", len(relationships), evidence)
	}
	row := relationships[0].(map[string]any)
	if got, want := row["entity_id"], "repository:r_orders_api"; got != want {
		t.Fatalf("entity_id = %#v, want %#v; row=%#v", got, want, row)
	}
	if got, want := row["relationship_type"], "DEFINES"; got != want {
		t.Fatalf("relationship_type = %#v, want %#v; row=%#v", got, want, row)
	}
	types, ok := row["relationship_types"].([]any)
	if !ok || len(types) != 1 || types[0] != "DEFINES" {
		t.Fatalf("relationship_types = %#v, want [DEFINES]; row=%#v", row["relationship_types"], row)
	}
	if got, want := row["depth"], float64(1); got != want {
		t.Fatalf("depth = %#v, want %#v; row=%#v", got, want, row)
	}

	sections := data["sections"].(map[string]any)
	if got := sections["defined_by"].([]any); len(got) != 1 {
		t.Fatalf("defined_by count = %d, want 1; sections=%#v", len(got), sections)
	}
}
