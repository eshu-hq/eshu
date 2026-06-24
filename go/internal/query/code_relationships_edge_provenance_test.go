// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRelationshipGraphRowCypherProjectsEdgeProvenance(t *testing.T) {
	t.Parallel()

	cypher := relationshipGraphRowCypher("e.id = $entity_id")

	for _, fragment := range []string{
		"outgoingRel.confidence",
		"outgoingRel.resolution_method",
		"incomingRel.confidence",
		"incomingRel.resolution_method",
	} {
		if !strings.Contains(cypher, fragment) {
			t.Fatalf("cypher = %q, want edge provenance fragment %q", cypher, fragment)
		}
	}
}

func TestNornicDBOneHopRelationshipsCypherProjectsEdgeProvenance(t *testing.T) {
	t.Parallel()

	for _, direction := range []string{"incoming", "outgoing"} {
		direction := direction
		t.Run(direction, func(t *testing.T) {
			t.Parallel()

			cypher, _ := nornicDBOneHopRelationshipsCypher(
				"content-entity:handleRelationships",
				direction,
				"CALLS",
				"Function",
				"uid",
			)
			for _, fragment := range []string{
				"rel.confidence as confidence",
				"rel.resolution_method as resolution_method",
			} {
				if !strings.Contains(cypher, fragment) {
					t.Fatalf("cypher = %q, want provenance fragment %q", cypher, fragment)
				}
			}
		})
	}
}

func TestHandleRelationshipsSurfacesGraphEdgeProvenance(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			runSingle: func(_ context.Context, _ string, _ map[string]any) (map[string]any, error) {
				return map[string]any{
					"id":         "function-1",
					"name":       "handlePayment",
					"labels":     []any{"Function"},
					"file_path":  "src/payments.ts",
					"repo_id":    "repo-1",
					"repo_name":  "payments",
					"language":   "typescript",
					"start_line": int64(10),
					"end_line":   int64(32),
					"outgoing": []any{
						map[string]any{
							"direction":         "outgoing",
							"type":              "CALLS",
							"target_name":       "chargeCard",
							"target_id":         "function-2",
							"confidence":        0.97,
							"resolution_method": "scip",
						},
						map[string]any{
							"direction":         "outgoing",
							"type":              "CALLS",
							"target_name":       "legacyCharge",
							"target_id":         "function-3",
							"resolution_method": "",
						},
					},
					"incoming": []any{},
				}, nil
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/relationships",
		bytes.NewBufferString(`{"entity_id":"function-1","direction":"outgoing","relationship_type":"CALLS"}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	outgoing, ok := resp["outgoing"].([]any)
	if !ok {
		t.Fatalf("resp[outgoing] type = %T, want []any", resp["outgoing"])
	}
	if got, want := len(outgoing), 2; got != want {
		t.Fatalf("len(resp[outgoing]) = %d, want %d", got, want)
	}
	provenance, ok := outgoing[0].(map[string]any)
	if !ok {
		t.Fatalf("outgoing[0] type = %T, want map[string]any", outgoing[0])
	}
	if got, want := provenance["confidence"], 0.97; got != want {
		t.Fatalf("outgoing[0].confidence = %#v, want %#v", got, want)
	}
	if got, want := provenance["resolution_method"], "scip"; got != want {
		t.Fatalf("outgoing[0].resolution_method = %#v, want %#v", got, want)
	}
	legacy, ok := outgoing[1].(map[string]any)
	if !ok {
		t.Fatalf("outgoing[1] type = %T, want map[string]any", outgoing[1])
	}
	if _, present := legacy["confidence"]; present {
		t.Fatalf("legacy edge should omit confidence, got %#v", legacy["confidence"])
	}
	if _, present := legacy["resolution_method"]; present {
		t.Fatalf("legacy edge should omit resolution_method, got %#v", legacy["resolution_method"])
	}
}

func TestNormalizeNornicDBRelationshipRowsDropsMissingEdgeProvenance(t *testing.T) {
	t.Parallel()

	rows := normalizeNornicDBRelationshipRows([]map[string]any{
		{
			"direction":         "outgoing",
			"type":              "CALLS",
			"confidence":        "rel.confidence",
			"resolution_method": "rel.resolution_method",
		},
		{
			"direction":         "outgoing",
			"type":              "CALLS",
			"confidence":        0.0,
			"resolution_method": "scip",
		},
	})

	if _, present := rows[0]["confidence"]; present {
		t.Fatalf("placeholder confidence should be omitted, got %#v", rows[0]["confidence"])
	}
	if _, present := rows[0]["resolution_method"]; present {
		t.Fatalf("placeholder resolution_method should be omitted, got %#v", rows[0]["resolution_method"])
	}
	if got, want := rows[1]["confidence"], 0.0; got != want {
		t.Fatalf("valid zero confidence = %#v, want %#v", got, want)
	}
	if got, want := rows[1]["resolution_method"], "scip"; got != want {
		t.Fatalf("valid resolution_method = %#v, want %#v", got, want)
	}
}
