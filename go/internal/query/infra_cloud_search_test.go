// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSearchInfraResourcesMatchesCloudARNWithDoubleColon(t *testing.T) {
	t.Parallel()

	arn := "arn:aws:iam::111122223333:role/sample-service"
	reader := &recordingInfraGraphReader{
		runRows: []map[string]any{
			{
				"id":            "cloud-resource:iam-role",
				"name":          "sample-service",
				"labels":        []any{"CloudResource"},
				"provider":      "aws",
				"resource_type": "aws_iam_role",
				"resource_id":   arn,
				"arn":           arn,
			},
		},
	}
	handler := &InfraHandler{Neo4j: reader}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/infra/resources/search",
		bytes.NewBufferString(`{"query":"`+arn+`","category":"cloud","limit":5}`),
	)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	for _, fragment := range []string{
		"coalesce(n.resource_type, n.data_type, '') = $resource_type_query",
		"coalesce(n.arn, '') = $query",
		"coalesce(n.resource_id, '') = $query",
	} {
		if !strings.Contains(reader.lastCypher, fragment) {
			t.Fatalf("cypher = %q, want fragment %q", reader.lastCypher, fragment)
		}
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := int(resp["count"].(float64)), 1; got != want {
		t.Fatalf("count = %d, want %d", got, want)
	}
}

func TestSearchInfraResourcesRejectsUnknownCategory(t *testing.T) {
	t.Parallel()

	handler := &InfraHandler{Neo4j: &recordingInfraGraphReader{}}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/infra/resources/search",
		bytes.NewBufferString(`{"query":"sample-service-api","category":"unknown"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := resp["detail"], "unsupported category"; got != want {
		t.Fatalf("detail = %#v, want %#v", got, want)
	}
}
