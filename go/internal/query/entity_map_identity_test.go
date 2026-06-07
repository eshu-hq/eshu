package query

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEntityMapDepthTwoDefinesTraversalProjectsStableRelationshipFields(t *testing.T) {
	t.Parallel()

	runRows := [][]map[string]any{
		{
			{
				"id":              "workload:api-node-boats",
				"name":            "api-node-boats",
				"labels":          []any{"Workload"},
				"repo_id":         "repo-api-node-boats",
				"anchor_label":    "Workload",
				"anchor_property": "id",
				"anchor_value":    "workload:api-node-boats",
			},
		},
	}
	for range entityMapDefaultOutgoingRelationships {
		runRows = append(runRows, nil)
	}
	runRows = append(runRows, []map[string]any{
		{
			"entity_id":           "repo-api-node-boats",
			"entity_name":         "api-node-boats",
			"entity_labels":       []any{"Repository"},
			"direction":           "incoming",
			"depth":               int64(1),
			"relationship_type":   "DEFINES",
			"relationship_types":  []any{},
			"repo_id":             "repo-api-node-boats",
			"relationship_source": "graph",
		},
	})
	for i := 1; i < len(entityMapDefaultIncomingRelationships); i++ {
		runRows = append(runRows, nil)
	}

	graph := &recordingEntityMapGraph{runRows: runRows}
	handler := &ImpactHandler{Neo4j: graph, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/impact/entity-map",
		bytes.NewBufferString(`{"from":"api-node-boats","depth":2,"limit":10}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	definesCallIndex := 1 + len(entityMapDefaultOutgoingRelationships)
	if len(graph.runCalls) <= definesCallIndex {
		t.Fatalf("graph Run calls = %d, want incoming DEFINES traversal call", len(graph.runCalls))
	}
	if got, want := graph.runCalls[definesCallIndex].params["start_repo_id"], "repo-api-node-boats"; got != want {
		t.Fatalf("incoming DEFINES traversal start_repo_id param = %#v, want %#v", got, want)
	}
	definesCypher := graph.runCalls[definesCallIndex].cypher
	for _, want := range []string{
		`"DEFINES" AS relationship_type`,
		"WHEN entity:Repository AND $start_repo_id <> '' THEN $start_repo_id",
	} {
		if !strings.Contains(definesCypher, want) {
			t.Fatalf("incoming DEFINES traversal cypher missing %q: %s", want, definesCypher)
		}
	}
	if strings.Contains(definesCypher, "coalesce(entity.id, entity.uid") {
		t.Fatalf("incoming DEFINES traversal cypher uses fragile multi-property coalesce for entity_id: %s", definesCypher)
	}

	data := decodeEntityMapData(t, w)
	evidence := data["evidence"].(map[string]any)
	relationships := evidence["relationships"].([]any)
	if got, want := len(relationships), 1; got != want {
		t.Fatalf("relationship count = %d, want %d", got, want)
	}
	relationship := relationships[0].(map[string]any)
	if got, want := relationship["entity_id"], "repo-api-node-boats"; got != want {
		t.Fatalf("relationship.entity_id = %#v, want %#v", got, want)
	}
	if got, want := relationship["relationship_type"], "DEFINES"; got != want {
		t.Fatalf("relationship.relationship_type = %#v, want %#v", got, want)
	}
	types := relationship["relationship_types"].([]any)
	if got, want := types[0], "DEFINES"; got != want {
		t.Fatalf("relationship.relationship_types[0] = %#v, want %#v", got, want)
	}
}
