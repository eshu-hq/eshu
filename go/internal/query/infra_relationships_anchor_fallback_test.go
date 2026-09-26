// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestInfraRelationshipsFallsBackToUnlabeledAnchorAfterLabelMisses pins
// answer parity with the pre-#7006 read: `MATCH (n) WHERE n.id = $entity_id`
// resolved an id on ANY label, while impactRelationshipAnchorLabels covers
// only the impact/platform labels. A code entity such as a Function (the
// live answer-truth A7 case) must still resolve through a final unlabeled
// read instead of silently answering 404.
func TestInfraRelationshipsFallsBackToUnlabeledAnchorAfterLabelMisses(t *testing.T) {
	t.Parallel()

	var calls []string
	reader := fakeRepoGraphReader{
		runSingle: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
			calls = append(calls, cypher)
			if !strings.Contains(cypher, "MATCH (n) WHERE n.id = $entity_id") {
				return nil, nil // every fast-path label misses
			}
			return map[string]any{
				"id":       "fn-helper",
				"name":     "Helper",
				"labels":   []any{"Function"},
				"outgoing": []any{},
				"incoming": []any{map[string]any{
					"direction": "incoming", "type": "CALLS",
					"source_name": "Main", "source_id": "fn-main",
				}},
			}, nil
		},
	}
	handler := &InfraHandler{Profile: ProfileLocalAuthoritative, Neo4j: reader}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/infra/relationships", strings.NewReader(`{"entity_id":"fn-helper"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if got, want := len(calls), len(impactRelationshipAnchorLabels)+1; got != want {
		t.Fatalf("graph reads = %d, want %d (every fast-path label, then one unlabeled fallback)", got, want)
	}
	for _, cypher := range calls[:len(calls)-1] {
		if strings.Contains(cypher, "MATCH (n) WHERE") {
			t.Fatalf("unlabeled anchor ran before the fast-path labels were exhausted:\n%s", cypher)
		}
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	data := body
	if inner, ok := body["data"].(map[string]any); ok {
		data = inner
	}
	if data["id"] != "fn-helper" {
		t.Fatalf("id = %v, want fn-helper; body = %s", data["id"], rec.Body.String())
	}
}
